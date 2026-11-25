package ipc

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"magicdown/pkg/engine"
)

const DefaultPort = 28888

// AddDownloadRequest is the JSON payload for adding a download over IPC.
type AddDownloadRequest struct {
	URL           string            `json:"url"`
	AudioURL      string            `json:"audio_url,omitempty"`
	Category      string            `json:"category,omitempty"`
	Filename      string            `json:"filename,omitempty"`
	SaveDir       string            `json:"save_dir,omitempty"`
	NumSegments   int               `json:"num_segments,omitempty"`
	TotalBytes    int64             `json:"total_bytes,omitempty"`
	Referrer      string            `json:"referrer,omitempty"`
	Cookies       string            `json:"cookies,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	ConvertFormat string            `json:"convert_format,omitempty"`
	TargetFormat  string            `json:"target_format,omitempty"`
	OutputFormat  string            `json:"output_format,omitempty"`
	AudioBitrate  string            `json:"audio_bitrate,omitempty"`
	Plan          json.RawMessage   `json:"plan,omitempty"`
	AutoStart     bool              `json:"auto_start"`
	StartPaused   bool              `json:"start_paused,omitempty"`
	SkipDialog    bool              `json:"skip_dialog,omitempty"`
	Subfolder     string            `json:"subfolder,omitempty"`
}

// BatchDownloadRequest is the JSON payload for enqueuing multiple downloads at once.
type BatchDownloadRequest struct {
	Items     []AddDownloadRequest `json:"items"`
	Subfolder string               `json:"subfolder,omitempty"`
	Referrer  string               `json:"referrer,omitempty"`
	Cookies   string               `json:"cookies,omitempty"`
	Headers   map[string]string    `json:"headers,omitempty"`
	AutoStart bool                 `json:"auto_start"`
}

// BatchDownloadResponse is returned by /downloads/batch.
type BatchDownloadResponse struct {
	Success bool     `json:"success"`
	Added   int      `json:"added"`
	Failed  int      `json:"failed"`
	TaskIDs []string `json:"task_ids,omitempty"`
	Errors  []string `json:"errors,omitempty"`
	Message string   `json:"message"`
}

// StatusResponse is returned by /status.
type StatusResponse struct {
	Status          string `json:"status"`
	App             string `json:"app"`
	Version         string `json:"version"`
	ActiveDownloads int    `json:"active_downloads"`
	TotalTasks      int    `json:"total_tasks"`
}

// Server provides the local HTTP IPC server for MagicDown desktop app.
type Server struct {
	engine             *engine.Engine
	port               int
	httpServer         *http.Server
	listener           net.Listener
	mu                 sync.Mutex
	running            bool
	onIncomingDownload func(AddDownloadRequest)
}

// SetOnIncomingDownload registers an optional callback that intercepts incoming
// IPC download requests (e.g. to display the Download Info Dialog in the GUI).
func (s *Server) SetOnIncomingDownload(fn func(AddDownloadRequest)) {
	s.mu.Lock()
	s.onIncomingDownload = fn
	s.mu.Unlock()
}

// NewServer creates a new IPC server. If port < 0, it uses DefaultPort (28888).
// If port == 0, it binds to an OS-chosen ephemeral port.
func NewServer(eng *engine.Engine, port int) *Server {
	if port < 0 {
		port = DefaultPort
	}
	return &Server{
		engine: eng,
		port:   port,
	}
}

// Start begins listening on localhost.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(s.port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to bind IPC server to %s: %w", addr, err)
	}
	s.listener = ln
	s.running = true

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/status", s.handleHealth)
	mux.HandleFunc("/downloads", s.handleDownloads)
	mux.HandleFunc("/downloads/add", s.handleAddDownload)
	mux.HandleFunc("/downloads/batch", s.handleAddBatchDownload)
	mux.HandleFunc("/downloads/pause", s.handlePause)
	mux.HandleFunc("/downloads/resume", s.handleResume)
	mux.HandleFunc("/downloads/delete", s.handleDelete)
	mux.HandleFunc("/config", s.handleConfig)

	s.httpServer = &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		_ = s.httpServer.Serve(ln)
	}()

	return nil
}

// Port returns the bound port.
func (s *Server) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.listener.Addr().(*net.TCPAddr).Port
	}
	return s.port
}

// Stop closes the IPC server.
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}
	s.running = false
	if s.httpServer != nil {
		return s.httpServer.Close()
	}
	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	tasks := s.engine.GetTasks()
	resp := StatusResponse{
		Status:          "ok",
		App:             "MagicDown",
		Version:         "1.0.0",
		ActiveDownloads: s.engine.ActiveCount(),
		TotalTasks:      len(tasks),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleDownloads(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		tasks := s.engine.GetTasks()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tasks)
	case http.MethodPost:
		s.handleAddDownload(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAddDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req AddDownloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid json: %v", err), http.StatusBadRequest)
		return
	}

	if req.URL == "" {
		http.Error(w, "missing url", http.StatusBadRequest)
		return
	}

	var plan *engine.DownloadPlan
	if len(req.Plan) > 0 {
		parsed, err := engine.ParseDownloadPlan(req.Plan, req.URL)
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid download plan: %v", err), http.StatusBadRequest)
			return
		}
		plan = parsed
	}

	s.mu.Lock()
	onIncoming := s.onIncomingDownload
	s.mu.Unlock()
	if onIncoming != nil && !req.SkipDialog {
		onIncoming(req)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"task": &engine.Task{
				URL:      req.URL,
				Filename: req.Filename,
			},
			"message": "download queued for confirmation",
		})
		return
	}

	saveDir := s.resolveSaveDir(req.SaveDir, req.Subfolder)
	task, err := s.engine.AddTask(engine.AddTaskRequest{
		URL:           req.URL,
		AudioURL:      req.AudioURL,
		Category:      req.Category,
		Filename:      req.Filename,
		SaveDir:       saveDir,
		NumSegments:   req.NumSegments,
		TotalBytes:    req.TotalBytes,
		Referrer:      req.Referrer,
		Cookies:       req.Cookies,
		Headers:       req.Headers,
		ConvertFormat: req.ConvertFormat,
		TargetFormat:  req.TargetFormat,
		OutputFormat:  req.OutputFormat,
		AudioBitrate:  req.AudioBitrate,
		Plan:          plan,
		AutoStart:     req.AutoStart,
		StartPaused:   req.StartPaused,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to add task: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"task":    task,
		"message": "download added successfully",
	})
}

func sanitizeSubfolder(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"..", "_",
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	s = strings.TrimSpace(replacer.Replace(s))
	s = strings.Trim(s, ". ")
	if len(s) > 80 {
		s = strings.TrimSpace(s[:80])
	}
	return s
}

func (s *Server) resolveSaveDir(explicitDir, subfolder string) string {
	if strings.TrimSpace(explicitDir) != "" {
		return explicitDir
	}
	safeSub := sanitizeSubfolder(subfolder)
	if safeSub == "" {
		return ""
	}
	base := s.engine.Config().DownloadDir
	if base == "" {
		return ""
	}
	return filepath.Join(base, safeSub)
}

func (s *Server) handleAddBatchDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req BatchDownloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid json: %v", err), http.StatusBadRequest)
		return
	}

	if len(req.Items) == 0 {
		http.Error(w, "missing items", http.StatusBadRequest)
		return
	}

	resp := BatchDownloadResponse{
		Success: true,
	}

	for _, item := range req.Items {
		if strings.TrimSpace(item.URL) == "" {
			resp.Failed++
			resp.Errors = append(resp.Errors, "missing url")
			continue
		}

		ref := item.Referrer
		if ref == "" {
			ref = req.Referrer
		}
		cookies := item.Cookies
		if cookies == "" {
			cookies = req.Cookies
		}
		headers := item.Headers
		if len(headers) == 0 && len(req.Headers) > 0 {
			headers = req.Headers
		}
		subfolder := item.Subfolder
		if subfolder == "" {
			subfolder = req.Subfolder
		}
		autoStart := item.AutoStart || req.AutoStart

		task, err := s.engine.AddTask(engine.AddTaskRequest{
			URL:           item.URL,
			AudioURL:      item.AudioURL,
			Category:      item.Category,
			Filename:      item.Filename,
			SaveDir:       s.resolveSaveDir(item.SaveDir, subfolder),
			NumSegments:   item.NumSegments,
			TotalBytes:    item.TotalBytes,
			Referrer:      ref,
			Cookies:       cookies,
			Headers:       headers,
			ConvertFormat: item.ConvertFormat,
			TargetFormat:  item.TargetFormat,
			OutputFormat:  item.OutputFormat,
			AudioBitrate:  item.AudioBitrate,
			AutoStart:     autoStart,
			StartPaused:   item.StartPaused,
		})
		if err != nil {
			resp.Failed++
			resp.Errors = append(resp.Errors, err.Error())
			continue
		}
		resp.Added++
		resp.TaskIDs = append(resp.TaskIDs, task.ID)
	}

	if resp.Added == 0 && resp.Failed > 0 {
		resp.Success = false
		resp.Message = "all batch downloads failed"
	} else {
		resp.Message = fmt.Sprintf("added %d downloads (%d failed)", resp.Added, resp.Failed)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handlePause(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		s.engine.PauseAll()
	} else {
		if err := s.engine.PauseTask(id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
}

func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		s.engine.ResumeAll()
	} else {
		if err := s.engine.ResumeTask(id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.engine.Config())
	case http.MethodPost:
		var cfg engine.Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, fmt.Sprintf("invalid json: %v", err), http.StatusBadRequest)
			return
		}
		current := s.engine.Config()
		// The state location is owned by the running process, not by clients.
		cfg.StateFilePath = current.StateFilePath
		if cfg.DownloadDir == "" {
			cfg.DownloadDir = current.DownloadDir
		}
		s.engine.UpdateConfig(cfg)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.engine.Config())
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	deleteFiles := r.URL.Query().Get("delete_files") == "true"
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	if err := s.engine.DeleteTask(id, deleteFiles); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
}
