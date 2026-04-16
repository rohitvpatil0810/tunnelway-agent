package tunnelwayagent

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/rohitvpatil0810/tunnelway-agent/pkg/logger"
)

func ForwardRequest(port int16, request *TunnelRequest) (*TunnelResponse, error) {
	// forward the request to the local server and return local response
	// construct the URL
	url := fmt.Sprintf("http://localhost:%d%s", port, request.Path)

	// create a new HTTP request - method, url, body
	var bodyReader io.Reader
	if len(request.Body) > 0 {
		bodyReader = bytes.NewReader(request.Body)
	} else {
		bodyReader = nil
	}
	req, err := http.NewRequest(request.Method, url, bodyReader)
	if err != nil {
		logger.Log.Error("Error creating new HTTP request", "error", err)
		return nil, err
	}

	// set headers
	req.Header = request.Headers

	// send the request using http.DefaultClient
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.Log.Error("Error forwarding request to local server", "error", err)
		return nil, err
	}
	defer resp.Body.Close()

	// read response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Log.Error("Error reading response body from local server", "error", err)
		return nil, err
	}

	return &TunnelResponse{
		ID:      request.ID,
		Status:  resp.StatusCode,
		Headers: resp.Header,
		Body:    string(bodyBytes),
	}, nil
}
