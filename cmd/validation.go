package cmd

import (
	"fmt"
	"net/url"
	"strings"
)

func normalizeServerBaseURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("server URL is empty")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid URL format")
	}

	if parsed.Scheme != "ws" && parsed.Scheme != "wss" {
		return "", fmt.Errorf("server URL must use ws or wss scheme")
	}

	if parsed.Host == "" {
		return "", fmt.Errorf("server URL must include host")
	}

	if parsed.Path != "" && parsed.Path != "/" {
		return "", fmt.Errorf("server URL must not include path")
	}

	if parsed.RawQuery != "" {
		return "", fmt.Errorf("server URL must not include query parameters")
	}

	if parsed.Fragment != "" {
		return "", fmt.Errorf("server URL must not include fragment")
	}

	parsed.Path = ""
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""

	return parsed.String(), nil
}

func normalizeServerPath(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("server path is empty")
	}

	if !strings.HasPrefix(trimmed, "/") {
		return "", fmt.Errorf("server path must start with /")
	}

	if strings.Contains(trimmed, "?") || strings.Contains(trimmed, "#") {
		return "", fmt.Errorf("server path must not include query parameters or fragments")
	}

	return trimmed, nil
}

func buildServerURL(baseURL, serverPath string) (string, error) {
	normalizedBaseURL, err := normalizeServerBaseURL(baseURL)
	if err != nil {
		return "", err
	}

	normalizedPath, err := normalizeServerPath(serverPath)
	if err != nil {
		return "", err
	}

	parsed, err := url.Parse(normalizedBaseURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL format")
	}

	parsed.Path = normalizedPath
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""

	return parsed.String(), nil
}
