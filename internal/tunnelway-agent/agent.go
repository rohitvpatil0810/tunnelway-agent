package tunnelwayagent

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
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
	frame []byte
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
	ID string
	RequestStart

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
			ID:           frame.RequestID,
			RequestStart: requestStart,
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

func encodeFrame(frame *Frame) ([]byte, error) {
	buf := bytes.NewBuffer(nil)

	// 1 byte for frame type
	if err := buf.WriteByte(byte(frame.Type)); err != nil {
		return nil, err
	}

	// request ID length + value
	idBytes := []byte(frame.RequestID)
	if len(idBytes) > 255 {
		return nil, errors.New("request ID too long")
	}
	if err := buf.WriteByte(byte(len(idBytes))); err != nil {
		return nil, err
	}
	if _, err := buf.Write(idBytes); err != nil {
		return nil, err
	}

	// payload length (4 bytes)
	if err := binary.Write(buf, binary.BigEndian, uint32(len(frame.Data))); err != nil {
		return nil, err
	}
	if _, err := buf.Write(frame.Data); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (a *Agent) currentState() *connectionState {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()
	return a.state
}

func (a *Agent) SendFrame(frame *Frame) (<-chan struct{}, error) {
	state := a.currentState()
	if state == nil {
		return nil, errors.New("agent has no active connection")
	}

	encoded, err := encodeFrame(frame)
	if err != nil {
		return nil, err
	}

	select {
	case <-state.closed:
		return nil, errors.New("agent connection is closed")
	case a.Send <- &OutBoundMessage{
		kind:  OutBoundResponse,
		frame: encoded,
	}:
		return state.closed, nil
	}
}

func (a *Agent) StreamResponse(requestId string, response *http.Response) error {
	// 1. Send response start frame
	var meta struct {
		StatusCode int
		Headers    http.Header
	}

	meta.StatusCode = response.StatusCode
	meta.Headers = response.Header.Clone()

	startFrameData, _ := json.Marshal(meta)

	_, err := a.SendFrame(&Frame{
		Type:      FrameResponseStart,
		RequestID: requestId,
		Data:      startFrameData,
	})
	if err != nil {
		logger.Log.Error("Failed to send response start frame", "error", err)
		return err
	}

	// 2. Stream response body in chunks
	buf := make([]byte, 1024*32) // 32KB buffer
	for {
		n, err := response.Body.Read(buf)
		if n > 0 {
			chunkData := buf[:n]
			_, err := a.SendFrame(&Frame{
				Type:      FrameResponseBodyChunk,
				RequestID: requestId,
				Data:      append([]byte(nil), chunkData...), // Copy the chunk data
			})
			if err != nil {
				logger.Log.Error("Failed to send response body chunk", "error", err)
				return err
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			logger.Log.Error("Error reading response body", "error", err)
			return err
		}
	}

	// 3. Send response end frame
	_, err = a.SendFrame(&Frame{
		Type:      FrameResponseBodyEnd,
		RequestID: requestId,
		Data:      nil,
	})
	if err != nil {
		logger.Log.Error("Failed to send response end frame", "error", err)
		return err
	}

	return nil
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
				frame := outBoundMessage.frame
				if err := state.conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
					logger.Log.Error("websocket write error", "error", err)
					a.signalClosed(state)
					return
				}
			}
		}
	}
}
