package ipc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"magicdown/pkg/engine"
)

// Client is a client for the local MagicDown IPC server.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new IPC client for the given port.
func NewClient(port int) *Client {
	if port <= 0 {
		port = DefaultPort
	}
	return &Client{
		baseURL: fmt.Sprintf("http://127.0.0.1:%d", port),
		httpClient: &http.Client{
			Timeout: 3 * time.Second,
			Transport: &http.Transport{
				DisableKeepAlives: true,
			},
		},
	}
}

// IsAppRunning checks if the MagicDown desktop app is reachable.
func (c *Client) IsAppRunning() bool {
	resp, err := c.httpClient.Get(c.baseURL + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
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

	resp, err := c.httpClient.Post(c.baseURL+"/downloads/add", "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to post download to MagicDown IPC: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
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

	resp, err := c.httpClient.Post(c.baseURL+"/downloads/batch", "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to post batch download to MagicDown IPC: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
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
	resp, err := c.httpClient.Get(c.baseURL + "/status")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var st StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return nil, err
	}
	return &st, nil
}
