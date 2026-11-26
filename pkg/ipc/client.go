package ipc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"magicdown/pkg/engine"
)

// Client is a client for the local MagicDown IPC server.
type Client struct {
	baseURL    string
	authToken  string
	tokenPath  string
	httpClient *http.Client
}

// NewClient creates a new IPC client for the given port.
func NewClient(port int) *Client {
	if port <= 0 {
		port = DefaultPort
	}
	return &Client{
		baseURL:   fmt.Sprintf("http://127.0.0.1:%d", port),
		tokenPath: DefaultTokenPath(),
		httpClient: &http.Client{
			Timeout: 3 * time.Second,
			Transport: &http.Transport{
				DisableKeepAlives: true,
			},
		},
	}
}

// SetAuthToken overrides the IPC auth token (useful in tests).
func (c *Client) SetAuthToken(token string) {
	c.authToken = strings.TrimSpace(token)
}

func (c *Client) resolveToken() string {
	if c.authToken != "" {
		return c.authToken
	}
	return ReadAuthToken(c.tokenPath)
}

func (c *Client) doRequest(method, path string, body io.Reader, contentType string) (*http.Response, error) {
	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if tok := c.resolveToken(); tok != "" {
		req.Header.Set(TokenHeader, tok)
	}
	return c.httpClient.Do(req)
}

// IsAppRunning checks if the MagicDown desktop app is reachable and authentic.
func (c *Client) IsAppRunning() bool {
	st, err := c.GetStatus()
	return err == nil && st != nil && st.App == "MagicDown"
}

// TaskResponse wraps the response when a task is added.
type TaskResponse struct {
	Success bool         `json:"success"`
	Task    *engine.Task `json:"task"`
	Message string       `json:"message"`
}

// AddDownload forwards a download job to the desktop app.
func (c *Client) AddDownload(req AddDownloadRequest) (*TaskResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := c.doRequest(http.MethodPost, "/downloads/add", bytes.NewReader(data), "application/json")
	if err != nil {
		return nil, fmt.Errorf("failed to post download to MagicDown IPC: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodySnippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		msg := strings.TrimSpace(string(bodySnippet))
		if msg != "" {
			return nil, fmt.Errorf("server returned status: %d (%s)", resp.StatusCode, msg)
		}
		return nil, fmt.Errorf("server returned status: %d", resp.StatusCode)
	}

	var taskResp TaskResponse
	if err := json.NewDecoder(resp.Body).Decode(&taskResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &taskResp, nil
}

// AddBatchDownload forwards a batch of download jobs to the desktop app without
// triggering per-item confirmation dialogs.
func (c *Client) AddBatchDownload(req BatchDownloadRequest) (*BatchDownloadResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal batch request: %w", err)
	}

	resp, err := c.doRequest(http.MethodPost, "/downloads/batch", bytes.NewReader(data), "application/json")
	if err != nil {
		return nil, fmt.Errorf("failed to post batch download to MagicDown IPC: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodySnippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		msg := strings.TrimSpace(string(bodySnippet))
		if msg != "" {
			return nil, fmt.Errorf("server returned status: %d (%s)", resp.StatusCode, msg)
		}
		return nil, fmt.Errorf("server returned status: %d", resp.StatusCode)
	}

	var batchResp BatchDownloadResponse
	if err := json.NewDecoder(resp.Body).Decode(&batchResp); err != nil {
		return nil, fmt.Errorf("failed to decode batch response: %w", err)
	}

	return &batchResp, nil
}

// GetStatus queries the desktop app status.
func (c *Client) GetStatus() (*StatusResponse, error) {
	resp, err := c.doRequest(http.MethodGet, "/status", nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status: %d", resp.StatusCode)
	}

	var st StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return nil, err
	}
	return &st, nil
}

// SendHello registers a real extension HELLO handshake with the Desktop App IPC server.
func (c *Client) SendHello(req HelloRequest) (*WelcomeResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal hello request: %w", err)
	}
	resp, err := c.doRequest(http.MethodPost, "/handshake/hello", bytes.NewReader(data), "application/json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("handshake rejected with status: %d", resp.StatusCode)
	}
	var welcome WelcomeResponse
	if err := json.NewDecoder(resp.Body).Decode(&welcome); err != nil {
		return nil, err
	}
	return &welcome, nil
}

// SendDisconnect notifies the Desktop App that a browser's Native Messaging host disconnected.
func (c *Client) SendDisconnect(browser string) error {
	payload, _ := json.Marshal(map[string]string{"browser": browser})
	resp, err := c.doRequest(http.MethodPost, "/handshake/disconnect", bytes.NewReader(payload), "application/json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}
