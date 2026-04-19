package tunnelwayagent

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rohitvpatil0810/tunnelway-agent/pkg/logger"
)

type Agent struct {
	ID           string
	internalPort int16
	Received     chan *TunnelRequest
	Send         chan *TunnelResponse

	PendingMu sync.Mutex
	Pending   map[string]bool

	LastHeartBeat time.Time

	stateMu sync.RWMutex
	state   *connectionState
}

type connectionState struct {
	conn      *websocket.Conn
	closed    chan struct{}
	closeOnce sync.Once
}

type TunnelResponse struct {
	ID      string
	Status  int
	Headers http.Header
	// Body    []byte `json:"Body"`
	// TODO: change to byte again - for testing changed to string
	Body string `json:"Body"`
}

type TunnelRequest struct {
	ID      string
	Method  string
	Path    string
	Headers http.Header
	Body    []byte
}

func Init(port int16) {
	logger.Init()

	if err := registerAgent(port); err != nil {
		logger.Log.Error("Failed to register agent", "error", err)
	}
}

func registerAgent(port int16) error {
	conn, message, err := dialAgent("")
	if err != nil {
		return err
	}

	logger.Log.Info("Serving public traffic on ", "url", message["subdomain"])

	agent := &Agent{
		ID:            extractAgentID(message["subdomain"]),
		internalPort:  port,
		Received:      make(chan *TunnelRequest, 128),
		Pending:       make(map[string]bool),
		Send:          make(chan *TunnelResponse, 128),
		LastHeartBeat: time.Now(),
	}

	state := agent.setConnectionState(conn)
	go agent.processReceivedMessages()
	agent.startConnectionLoops(state)

	for {
		<-state.closed
		logger.Log.Info("Connection closed, retrying...")
		state = agent.retryConnection()
	}
}

func dialAgent(agentID string) (*websocket.Conn, map[string]string, error) {
	u := url.URL{
		Scheme: "ws",
		Host:   "localhost:6000",
		Path:   "/_ws/agent",
	}
	if agentID != "" {
		query := u.Query()
		query.Set("agent_id", agentID)
		u.RawQuery = query.Encode()
	}

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		logger.Log.Error("Failed to dial websocket", "error", err)
		return nil, nil, err
	}

	var message map[string]string
	if err := conn.ReadJSON(&message); err != nil {
		logger.Log.Error("websocket read error", "error", err)
		conn.Close()
		return nil, nil, err
	}

	return conn, message, nil
}

func extractAgentID(subdomain string) string {
	parts := strings.SplitN(subdomain, ".", 2)
	if len(parts) == 0 {
		return subdomain
	}
	return parts[0]
}

func (a *Agent) setConnectionState(conn *websocket.Conn) *connectionState {
	state := &connectionState{
		conn:   conn,
		closed: make(chan struct{}),
	}

	a.stateMu.Lock()
	a.state = state
	a.stateMu.Unlock()

	return state
}

func (a *Agent) startConnectionLoops(state *connectionState) {
	go a.startReadLoop(state)
	go a.startWriteLoop(state)
	go a.startHeartBeat(state)
}

func (a *Agent) retryConnection() *connectionState {
	backoff := time.Second

	for {
		conn, message, err := dialAgent(a.ID)
		if err != nil {
			time.Sleep(backoff)
			backoff *= 2
			if backoff > time.Minute {
				backoff = time.Minute
			}
			continue
		}

		if subdomain := message["subdomain"]; subdomain != "" {
			a.ID = extractAgentID(subdomain)
			logger.Log.Info("Serving public traffic on ", "url", subdomain)
		}

		state := a.setConnectionState(conn)
		logger.Log.Info("Reconnected to server")
		a.startConnectionLoops(state)
		return state
	}
}

func (a *Agent) startHeartBeat(state *connectionState) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := state.conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				logger.Log.Error("Failed to send heartbeat", "error", err)
				a.signalClosed(state)
				return
			}
		case <-state.closed:
			return
		}
	}
}

func (a *Agent) signalClosed(state *connectionState) {
	state.closeOnce.Do(func() {
		close(state.closed)
		state.conn.Close()
	})
}

func (a *Agent) startReadLoop(state *connectionState) {
	for {
		_, msg, err := state.conn.ReadMessage()
		if err != nil {
			logger.Log.Error("websocket read error", "error", err)
			a.signalClosed(state)
			return
		}

		request := &TunnelRequest{}
		if err := json.Unmarshal(msg, request); err != nil {
			logger.Log.Error("Error unmarshalling json", "error", err)
			continue
		}

		a.Received <- request
		a.PendingMu.Lock()
		a.Pending[request.ID] = true
		a.PendingMu.Unlock()
	}
}

func (a *Agent) processReceivedMessages() {
	for request := range a.Received {
		logger.Log.Debug("Processing Request", "requestId", request.ID, "method", request.Method, "path", request.Path)
		response, err := ForwardRequest(a.internalPort, request)
		if err != nil {
			logger.Log.Error("Error forwarding request", "error", err)
			// Send an error response back to the server
			response = &TunnelResponse{
				ID:     request.ID,
				Status: http.StatusInternalServerError,
				Body:   "Internal Server Error: " + err.Error(),
			}
		}
		a.Send <- response
	}
}

func (a *Agent) startWriteLoop(state *connectionState) {
	for {
		select {
		case <-state.closed:
			return
		case response := <-a.Send:
			logger.Log.Debug("Sending Response", "responseId", response.ID, "status", response.Status)
			if err := state.conn.WriteJSON(response); err != nil {
				logger.Log.Error("websocket write error", "error", err)
				a.signalClosed(state)
				return
			}

			a.PendingMu.Lock()
			delete(a.Pending, response.ID)
			a.PendingMu.Unlock()
		}
	}
}
