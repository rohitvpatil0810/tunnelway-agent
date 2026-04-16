package tunnelwayagent

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rohitvpatil0810/tunnelway-agent/pkg/logger"
)

type Agent struct {
	conn         *websocket.Conn
	internalPort int16
	Received     chan *TunnelRequest
	Send         chan *TunnelResponse

	PendingMu sync.Mutex
	Pending   map[string]bool

	LastHeartBeat time.Time

	Closed chan struct{}
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
	// Initialize the logger
	logger.Init()

	// Register the agent with the remote server
	err := registerAgent(port)
	if err != nil {
		logger.Log.Error("Failed to register agent", "error", err)
		return
	}
}

func registerAgent(port int16) error {
	// construct server websocket URL
	u := url.URL{
		Scheme: "ws",
		Host:   "localhost:6000",
		Path:   "/_ws/agent",
	}

	// Dial the websocket
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		logger.Log.Error("Failed to dial websocket", "error", err)
		return err
	}

	// read first message from server (it returns the public)
	defer conn.Close()
	var message map[string]string
	err = conn.ReadJSON(&message)
	if err != nil {
		logger.Log.Error("websocket read error", "error", err)
	}

	logger.Log.Info("Serving public traffic on ", "url", message["subdomain"])

	agent := &Agent{
		conn:         conn,
		internalPort: port,
		// make buffer of 128, to allow up to 128 messages to be queued
		Received:      make(chan *TunnelRequest, 128),
		Pending:       make(map[string]bool),
		Send:          make(chan *TunnelResponse, 128),
		LastHeartBeat: time.Now(),
		Closed:        make(chan struct{}),
	}
	go agent.startReadLoop()
	go agent.processReceivedMessages()
	go agent.startWriteLoop()

	// keep the main goroutine alive forever, until the connection is closed
	select {
	case <-agent.Closed:
		logger.Log.Info("Connection closed, exiting")
		return nil
	}
}

func (a *Agent) startReadLoop() {
	for {
		_, msg, err := a.conn.ReadMessage()
		if err != nil {
			logger.Log.Error("websocket read error", "error", err)
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
		// Process the request and generate a response
		logger.Log.Debug("Processing Request", "requestId", request.ID, "method", request.Method, "path", request.Path)
		response, err := ForwardRequest(a.internalPort, request)
		if err != nil {
			logger.Log.Error("Error forwarding request", "error", err)
			continue
		}
		a.Send <- response
	}
}

func (a *Agent) startWriteLoop() {
	for response := range a.Send {
		logger.Log.Debug("Sending Response", "responseId", response.ID, "status", response.Status)
		err := a.conn.WriteJSON(response)
		if err != nil {
			logger.Log.Error("websocket write error", "error", err)
			return
		}
		a.PendingMu.Lock()
		// remove the request ID from pending
		delete(a.Pending, response.ID)
		a.PendingMu.Unlock()
	}
}
