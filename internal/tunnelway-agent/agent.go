package tunnelwayagent

import (
	"net/url"

	"github.com/gorilla/websocket"
	"github.com/rohitvpatil0810/tunnelway-agent/pkg/logger"
)

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
		Host:   "localhost:8080",
		Path:   "/_ws/agent",
	}

	// Dial the websocket
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		logger.Log.Error("Failed to dial websocket", "error", err)
		return err
	}

	// keep a forever loop to read messages from the server and handle them
	defer conn.Close()
	for {
		mt, message, err := conn.ReadMessage()
		if err != nil {
			logger.Log.Error("websocket read error", "error", err)
			break
		}
		logger.Log.Info("received message from server", "msg", string(message))

		// example: echo the message back
		if err := conn.WriteMessage(mt, message); err != nil {
			logger.Log.Error("websocket write error", "error", err)
			break
		}
	}

	// TODO: handle reconnection logic here

	return nil
}
