package tunnelwayagent

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rohitvpatil0810/tunnelway-agent/pkg/logger"
)

type OutBounKind int

const (
	OutBoundResponse OutBounKind = iota
	OutBoundHeartBeat
)

type connectionState struct {
	conn      *websocket.Conn
	closed    chan struct{}
	closeOnce sync.Once
}

type OutBoundMessage struct {
	kind  OutBounKind
	frame *Frame
}

type Agent struct {
	ID           string
	internalPort int16

	ReceivedMu sync.Mutex
	Received   map[string]*RequestStream

	Send chan *OutBoundMessage

	LastHeartBeat time.Time

	stateMu sync.RWMutex
	state   *connectionState

	RequestQueue chan *RequestStream

	workerCount int
}

type FrameType int

const (
	FrameRequestStart FrameType = iota
	FrameRequestBodyChunk
	FrameRequestBodyEnd
	FrameResponseStart
	FrameResponseBodyChunk
	FrameResponseBodyEnd
)

type RequestStream struct {
	requestStart RequestStart

	pr *io.PipeReader
	pw *io.PipeWriter
}

type RequestStart struct {
	Method  string      `json:"method"`
	URL     string      `json:"url"`
	Headers http.Header `json:"headers"`
}

type Frame struct {
	Type      FrameType
	RequestID string
	Data      []byte
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
		Received:      make(map[string]*RequestStream),
		RequestQueue:  make(chan *RequestStream, 128),
		Send:          make(chan *OutBoundMessage, 128),
		LastHeartBeat: time.Now(),
		workerCount:   8, // Set the desired number of workers
	}

	state := agent.setConnectionState(conn)
	agent.startRequestWorkers()
	agent.startConnectionLoops(state)

	for {
		<-state.closed
		logger.Log.Info("Connection closed, retrying...")
		state = agent.retryConnection()
	}
}

func dialAgent(agentID string) (*websocket.Conn, map[string]string, error) {
	u := url.URL{
		Scheme: "wss",
		Host:   "tunnelway.online",
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
			a.Send <- &OutBoundMessage{
				kind: OutBoundHeartBeat,
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

func decodeFrame(data []byte) (*Frame, error) {
	reader := bytes.NewReader(data)

	// 1 byte for frame type
	frameTypeByte, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}

	idLen, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}

	idBytes := make([]byte, idLen)
	if _, err := io.ReadFull(reader, idBytes); err != nil {
		return nil, err
	}

	var payloadLen uint32
	if err := binary.Read(reader, binary.BigEndian, &payloadLen); err != nil {
		return nil, err
	}

	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}

	return &Frame{
		Type:      FrameType(frameTypeByte),
		RequestID: string(idBytes),
		Data:      payload,
	}, nil
}

func (a *Agent) handleRequestFrame(frame *Frame) {
	switch frame.Type {
	case FrameRequestStart:
		var requestStart RequestStart
		if err := json.Unmarshal(frame.Data, &requestStart); err != nil {
			logger.Log.Error("Failed to unmarshal request start frame", "error", err)
			return
		}

		pr, pw := io.Pipe()
		var requestStream = &RequestStream{
			requestStart: requestStart,
			pr:           pr,
			pw:           pw,
		}
		a.ReceivedMu.Lock()
		a.Received[frame.RequestID] = requestStream
		a.ReceivedMu.Unlock()

		a.RequestQueue <- requestStream

	case FrameRequestBodyChunk:
		a.ReceivedMu.Lock()
		stream, exists := a.Received[frame.RequestID]
		a.ReceivedMu.Unlock()
		if !exists {
			logger.Log.Error("Received body chunk for unknown request ID", "requestID", frame.RequestID)
			return
		}
		if _, err := stream.pw.Write(frame.Data); err != nil {
			logger.Log.Error("Failed to write to request body pipe", "error", err)
		}

	case FrameRequestBodyEnd:
		a.ReceivedMu.Lock()
		stream, exists := a.Received[frame.RequestID]
		a.ReceivedMu.Unlock()
		if !exists {
			logger.Log.Error("Received body end for unknown request ID", "requestID", frame.RequestID)
			return
		}
		stream.pw.Close()
	}
}

func (a *Agent) startReadLoop(state *connectionState) {
	for {
		_, msg, err := state.conn.ReadMessage()
		if err != nil {
			logger.Log.Error("websocket read error", "error", err)
			a.signalClosed(state)
			return
		}

		frame, err := decodeFrame(msg)
		if err != nil {
			logger.Log.Error("Failed to decode frame", "error", err)
			continue
		}

		a.handleRequestFrame(frame)
	}
}

func (a *Agent) startRequestWorkers() {
	for i := 0; i < a.workerCount; i++ {
		go requestWorker(i, a)
	}
}

func (a *Agent) startWriteLoop(state *connectionState) {
	for {
		select {
		case <-state.closed:
			return
		case outBoundMessage := <-a.Send:

			switch outBoundMessage.kind {
			case OutBoundHeartBeat:
				// Just send a ping message for heartbeat
				if err := state.conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
					logger.Log.Error("websocket write error", "error", err)
					a.signalClosed(state)
					return
				}
				continue
			case OutBoundResponse:
				// Send the actual response back to the server
				response := outBoundMessage.response
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
}
