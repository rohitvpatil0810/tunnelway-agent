package tunnelwayagent

import (
	"fmt"
	"net/http"

	"github.com/rohitvpatil0810/tunnelway-agent/pkg/logger"
)

func ForwardRequest(agent *Agent, request *RequestStream) error {
	// forward the request to the local server and return local response
	// construct the URL
	url := fmt.Sprintf("http://localhost:%d%s", agent.internalPort, request.URL)

	// create a new HTTP request with the same method, URL, headers and body as the incoming request
	req, err := http.NewRequest(request.Method, url, request.pr)
	if err != nil {
		logger.Log.Error("Failed to create new HTTP request", "error", err)
		return err
	}

	req.Header = request.Headers.Clone()

	// send the request to the local server
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		logger.Log.Error("Failed to forward request to local server", "error", err)
		return err
	}
	defer resp.Body.Close()

	// TODO: stream the response back to the agent
	err = agent.StreamResponse(request.ID, resp)
	if err != nil {
		logger.Log.Error("Failed to stream response back to agent", "error", err)
		return err
	}

	return nil
}

func requestWorker(ID int, agent *Agent) {
	logger.Init()
	logger.Log.Info("Starting request worker", "ID", ID)
	for requestStream := range agent.RequestQueue {

		logger.Log.Debug("Processing Request: ", "workerID", ID, "requestId", requestStream.ID, "method", requestStream.Method, "path", requestStream.URL)
		ForwardRequest(agent, requestStream)
	}
}
