package nativemessaging

import (
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"strings"

	"magicdown/pkg/ipc"
	"magicdown/pkg/version"
)

func safeURLHost(raw string) string {
	if u, err := url.Parse(strings.TrimSpace(raw)); err == nil {
		return u.Hostname()
	}
	return ""
}

// InboundMessage represents a message received from the browser extension.
type InboundMessage struct {
	Type             string                   `json:"type,omitempty"`
	ProtocolVersion  int                      `json:"protocolVersion,omitempty"`
	ExtensionVersion string                   `json:"extensionVersion,omitempty"`
	Browser          string                   `json:"browser,omitempty"`
	Action           string                   `json:"action"`
	URL              string                   `json:"url,omitempty"`
	AudioURL         string                   `json:"audio_url,omitempty"`
	Filename         string                   `json:"filename,omitempty"`
	Referrer         string                   `json:"referrer,omitempty"`
	Cookies          string                   `json:"cookies,omitempty"`
	Headers          map[string]string        `json:"headers,omitempty"`
	NumSegments      int                      `json:"num_segments,omitempty"`
	TotalBytes       int64                    `json:"total_bytes,omitempty"`
	ConvertFormat    string                   `json:"convert_format,omitempty"`
	TargetFormat     string                   `json:"target_format,omitempty"`
	OutputFormat     string                   `json:"output_format,omitempty"`
	AudioBitrate     string                   `json:"audio_bitrate,omitempty"`
	Plan             json.RawMessage          `json:"plan,omitempty"`
	AutoStart        bool                     `json:"auto_start,omitempty"`
	SkipDialog       bool                     `json:"skip_dialog,omitempty"`
	Subfolder        string                   `json:"subfolder,omitempty"`
	Items            []ipc.AddDownloadRequest `json:"items,omitempty"`
}

// OutboundMessage represents a response sent back to the browser extension.
type OutboundMessage struct {
	Type            string `json:"type,omitempty"`
	ProtocolVersion int    `json:"protocolVersion,omitempty"`
	HostVersion     string `json:"hostVersion,omitempty"`
	Status          string `json:"status,omitempty"`
	Success         bool   `json:"success"`
	Action          string `json:"action,omitempty"`
	Message         string `json:"message,omitempty"`
	TaskID          string `json:"taskId,omitempty"`
	Filename        string `json:"filename,omitempty"`
	Added           int    `json:"added,omitempty"`
	Failed          int    `json:"failed,omitempty"`
	AppRunning      bool   `json:"app_running,omitempty"`
	Error           string `json:"error,omitempty"`
}

// Handler processes native messaging messages and talks to the IPC client.
type Handler struct {
	ipcClient             *ipc.Client
	lastBrowser           string
	detectParentBrowserFn func() string
}

// NewHandler creates a handler using the given IPC client.
func NewHandler(client *ipc.Client) *Handler {
	return &Handler{
		ipcClient:             client,
		detectParentBrowserFn: DetectParentBrowserID,
	}
}

func (h *Handler) resolveBrowserID(declared string) string {
	if h.detectParentBrowserFn != nil {
		if detected := strings.ToLower(strings.TrimSpace(h.detectParentBrowserFn())); detected != "" {
			return detected
		}
	}
	if b := strings.ToLower(strings.TrimSpace(declared)); b != "" {
		return b
	}
	return "chrome"
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

	action := strings.ToLower(strings.TrimSpace(in.Action))
	if strings.EqualFold(strings.TrimSpace(in.Type), "HELLO") {
		action = "hello"
	} else if strings.EqualFold(strings.TrimSpace(in.Type), "DISCONNECT") {
		action = "disconnect"
	}

	switch action {
	case "hello":
		browser := h.resolveBrowserID(in.Browser)
		h.lastBrowser = browser
		protoVer := in.ProtocolVersion
		if protoVer <= 0 {
			protoVer = 1
		}
		welcome, err := h.ipcClient.SendHello(ipc.HelloRequest{
			Type:             "HELLO",
			ProtocolVersion:  protoVer,
			ExtensionVersion: in.ExtensionVersion,
			Browser:          browser,
			HostVersion:      version.Version,
		})
		if err != nil {
			LogEvent("WARN", "hello_handshake", map[string]any{
				"browser":          browser,
				"declared_browser": strings.ToLower(strings.TrimSpace(in.Browser)),
				"status":           "offline",
			})
			resp := OutboundMessage{
				Type:            "WELCOME",
				ProtocolVersion: 1,
				HostVersion:     version.Version,
				Status:          "offline",
				Success:         false,
				Action:          "hello",
				AppRunning:      false,
				Error:           "MagicDown desktop app is not running: " + err.Error(),
			}
			return json.Marshal(resp)
		}
		LogEvent("INFO", "hello_handshake", map[string]any{
			"browser":          browser,
			"declared_browser": strings.ToLower(strings.TrimSpace(in.Browser)),
			"status":           welcome.Status,
		})
		resp := OutboundMessage{
			Type:            "WELCOME",
			ProtocolVersion: welcome.ProtocolVersion,
			HostVersion:     welcome.HostVersion,
			Status:          welcome.Status,
			Success:         true,
			Action:          "hello",
			AppRunning:      true,
			Message:         "connected",
		}
		return json.Marshal(resp)

	case "disconnect":
		browser := h.resolveBrowserID(in.Browser)
		_ = h.ipcClient.SendDisconnect(browser)
		resp := OutboundMessage{
			Success: true,
			Action:  "disconnect",
			Message: "disconnected",
		}
		return json.Marshal(resp)

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
		if err := ipc.ValidateHTTPURL(in.URL); err != nil {
			LogEvent("WARN", "download_rejected", map[string]any{"error": err.Error()})
			resp := OutboundMessage{
				Success: false,
				Action:  "download",
				Error:   err.Error(),
			}
			return json.Marshal(resp)
		}

		// Check if desktop app is running
		if !h.ipcClient.IsAppRunning() {
			LogEvent("WARN", "download_app_offline", map[string]any{"host": safeURLHost(in.URL)})
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
			LogEvent("ERROR", "download_forward_failed", map[string]any{"host": safeURLHost(in.URL), "error": err.Error()})
			resp := OutboundMessage{
				Success: false,
				Action:  "download",
				Error:   "failed to add download: " + err.Error(),
			}
			return json.Marshal(resp)
		}

		LogEvent("INFO", "download_forwarded", map[string]any{"host": safeURLHost(in.URL), "has_filename": strings.TrimSpace(in.Filename) != ""})
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

	LogEvent("INFO", "host_startup", map[string]any{"ipc_port": port})
	defer func() {
		// Note: Do NOT call client.SendDisconnect on normal stdio EOF.
		// Chromium's one-shot chrome.runtime.sendNativeMessage closes stdin
		// immediately after exchanging a single HELLO/WELCOME message; sending
		// disconnect here would immediately erase the handshake. Connection
		// liveness is tracked by periodic HELLO heartbeats and IPC handshakeTTL.
		LogEvent("INFO", "host_shutdown", map[string]any{"ipc_port": port, "browser": handler.lastBrowser})
	}()

	for {
		msgBytes, err := ReadMessage(in)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			LogEvent("ERROR", "comm_error", map[string]any{"stage": "read", "error": err.Error()})
			return err
		}

		respBytes, err := handler.HandleMessage(msgBytes)
		if err != nil {
			LogEvent("ERROR", "comm_error", map[string]any{"stage": "handle", "error": err.Error()})
			continue
		}

		if err := WriteMessage(out, respBytes); err != nil {
			LogEvent("ERROR", "comm_error", map[string]any{"stage": "write", "error": err.Error()})
			return err
		}
	}
}

// RunHostWithStdio runs standard Stdio native host.
func RunHostWithStdio(port int) error {
	return RunStdioHost(os.Stdin, os.Stdout, port)
}
