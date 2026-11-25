package nativemessaging

import (
	"encoding/json"
	"errors"
	"io"
	"os"

	"magicdown/pkg/ipc"
)

// InboundMessage represents a message received from the browser extension.
type InboundMessage struct {
	Action        string            `json:"action"`
	URL           string            `json:"url,omitempty"`
	AudioURL      string            `json:"audio_url,omitempty"`
	Filename      string            `json:"filename,omitempty"`
	Referrer      string            `json:"referrer,omitempty"`
	Cookies       string            `json:"cookies,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	NumSegments   int               `json:"num_segments,omitempty"`
	TotalBytes    int64             `json:"total_bytes,omitempty"`
	ConvertFormat string                   `json:"convert_format,omitempty"`
	TargetFormat  string                   `json:"target_format,omitempty"`
	OutputFormat  string                   `json:"output_format,omitempty"`
	AudioBitrate  string                   `json:"audio_bitrate,omitempty"`
	Plan          json.RawMessage          `json:"plan,omitempty"`
	AutoStart     bool                     `json:"auto_start,omitempty"`
	SkipDialog    bool                     `json:"skip_dialog,omitempty"`
	Subfolder     string                   `json:"subfolder,omitempty"`
	Items         []ipc.AddDownloadRequest `json:"items,omitempty"`
}

// OutboundMessage represents a response sent back to the browser extension.
type OutboundMessage struct {
	Success    bool   `json:"success"`
	Action     string `json:"action,omitempty"`
	Message    string `json:"message,omitempty"`
	TaskID     string `json:"taskId,omitempty"`
	Filename   string `json:"filename,omitempty"`
	Added      int    `json:"added,omitempty"`
	Failed     int    `json:"failed,omitempty"`
	AppRunning bool   `json:"app_running,omitempty"`
	Error      string `json:"error,omitempty"`
}

// Handler processes native messaging messages and talks to the IPC client.
type Handler struct {
	ipcClient *ipc.Client
}

// NewHandler creates a handler using the given IPC client.
func NewHandler(client *ipc.Client) *Handler {
	return &Handler{ipcClient: client}
}

// HandleMessage parses an inbound message and returns the outbound response bytes.
func (h *Handler) HandleMessage(msgBytes []byte) ([]byte, error) {
	var in InboundMessage
	if err := json.Unmarshal(msgBytes, &in); err != nil {
		resp := OutboundMessage{
			Success: false,
			Error:   "invalid JSON payload: " + err.Error(),
		}
		return json.Marshal(resp)
	}

	switch in.Action {
	case "ping":
		appRunning := h.ipcClient.IsAppRunning()
		resp := OutboundMessage{
			Success:    true,
			Action:     "ping",
			Message:    "pong",
			AppRunning: appRunning,
		}
		return json.Marshal(resp)

	case "status":
		status, err := h.ipcClient.GetStatus()
		if err != nil {
			resp := OutboundMessage{
				Success:    false,
				Action:     "status",
				AppRunning: false,
				Error:      "MagicDown desktop app is not running: " + err.Error(),
			}
			return json.Marshal(resp)
		}
		resp := OutboundMessage{
			Success:    true,
			Action:     "status",
			AppRunning: true,
			Message:    status.Status,
		}
		return json.Marshal(resp)

	case "download":
		if in.URL == "" {
			resp := OutboundMessage{
				Success: false,
				Action:  "download",
				Error:   "missing download URL",
			}
			return json.Marshal(resp)
		}

		// Check if desktop app is running
		if !h.ipcClient.IsAppRunning() {
			resp := OutboundMessage{
				Success: false,
				Action:  "download",
				Error:   "MagicDown desktop app is not running. Please open MagicDown first.",
			}
			return json.Marshal(resp)
		}

		// Forward download job to desktop app
		taskResp, err := h.ipcClient.AddDownload(ipc.AddDownloadRequest{
			URL:           in.URL,
			AudioURL:      in.AudioURL,
			Filename:      in.Filename,
			Referrer:      in.Referrer,
			Cookies:       in.Cookies,
			Headers:       in.Headers,
			NumSegments:   in.NumSegments,
			TotalBytes:    in.TotalBytes,
			ConvertFormat: in.ConvertFormat,
			TargetFormat:  in.TargetFormat,
			OutputFormat:  in.OutputFormat,
			AudioBitrate:  in.AudioBitrate,
			Plan:          in.Plan,
			AutoStart:     true, // default to starting automatically from browser
			SkipDialog:    in.SkipDialog,
			Subfolder:     in.Subfolder,
		})
		if err != nil {
			resp := OutboundMessage{
				Success: false,
				Action:  "download",
				Error:   "failed to add download: " + err.Error(),
			}
			return json.Marshal(resp)
		}

		resp := OutboundMessage{
			Success:  true,
			Action:   "download",
			Message:  "Download added to MagicDown",
			TaskID:   taskResp.Task.ID,
			Filename: taskResp.Task.Filename,
		}
		return json.Marshal(resp)

	case "batch_download":
		if len(in.Items) == 0 {
			resp := OutboundMessage{
				Success: false,
				Action:  "batch_download",
				Error:   "missing batch download items",
			}
			return json.Marshal(resp)
		}

		if !h.ipcClient.IsAppRunning() {
			resp := OutboundMessage{
				Success: false,
				Action:  "batch_download",
				Error:   "MagicDown desktop app is not running. Please open MagicDown first.",
			}
			return json.Marshal(resp)
		}

		batchResp, err := h.ipcClient.AddBatchDownload(ipc.BatchDownloadRequest{
			Items:     in.Items,
			Subfolder: in.Subfolder,
			Referrer:  in.Referrer,
			Cookies:   in.Cookies,
			Headers:   in.Headers,
			AutoStart: true,
		})
		if err != nil {
			resp := OutboundMessage{
				Success: false,
				Action:  "batch_download",
				Error:   "failed to add batch downloads: " + err.Error(),
			}
			return json.Marshal(resp)
		}

		resp := OutboundMessage{
			Success: batchResp.Success,
			Action:  "batch_download",
			Message: batchResp.Message,
			Added:   batchResp.Added,
			Failed:  batchResp.Failed,
		}
		return json.Marshal(resp)

	default:
		resp := OutboundMessage{
			Success: false,
			Action:  in.Action,
			Error:   "unknown action: " + in.Action,
		}
		return json.Marshal(resp)
	}
}

// RunStdioHost runs the native messaging loop reading from in (stdin) and writing to out (stdout).
func RunStdioHost(in io.Reader, out io.Writer, port int) error {
	client := ipc.NewClient(port)
	handler := NewHandler(client)

	for {
		msgBytes, err := ReadMessage(in)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		respBytes, err := handler.HandleMessage(msgBytes)
		if err != nil {
			continue
		}

		if err := WriteMessage(out, respBytes); err != nil {
			return err
		}
	}
}

// RunHostWithStdio runs standard Stdio native host.
func RunHostWithStdio(port int) error {
	return RunStdioHost(os.Stdin, os.Stdout, port)
}
