package tunnelwayagent

import (
	"net/http"

	"github.com/rohitvpatil0810/tunnelway-agent/pkg/logger"
)

func requestWorker(ID int, agent *Agent) {
	logger.Init()
	logger.Log.Info("Starting request worker", "ID", ID)
	for request := range agent.Received {
		logger.Log.Debug("Processing Request: ", "workerID", ID, "requestId", request.ID, "method", request.Method, "path", request.Path)
		response, err := ForwardRequest(agent.internalPort, request)
		if err != nil {
			logger.Log.Error("Error forwarding request", "error", err)
			// Send an error response back to the server
			response = &TunnelResponse{
				ID:     request.ID,
				Status: http.StatusInternalServerError,
				Body:   "Internal Server Error: " + err.Error(),
			}
		}
		agent.Send <- &OutBoundMessage{
			kind:     OutBoundResponse,
			response: response,
		}
	}
}
