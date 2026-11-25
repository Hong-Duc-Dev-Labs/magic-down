package ipc

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"magicdown/pkg/engine"
)

const DefaultPort = 28888

// AddDownloadRequest is the JSON payload for adding a download over IPC.
type AddDownloadRequest struct {
	URL           string            `json:"url"`
	Filename      string            `json:"filename,omitempty"`
	SaveDir       string            `json:"save_dir,omitempty"`
	NumSegments   int               `json:"num_segments,omitempty"`
	Referrer      string            `json:"referrer,omitempty"`
	Cookies       string            `json:"cookies,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	ConvertFormat string            `json:"convert_format,omitempty"`
	AutoStart     bool              `json:"auto_start"`
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
	engine     *engine.Engine
	port       int
	httpServer *http.Server
	listener   net.Listener
	mu         sync.Mutex
	running    bool
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

	task, err := s.engine.AddTask(engine.AddTaskRequest{
		URL:           req.URL,
		Filename:      req.Filename,
		SaveDir:       req.SaveDir,
		NumSegments:   req.NumSegments,
		Referrer:      req.Referrer,
		Cookies:       req.Cookies,
		Headers:       req.Headers,
		ConvertFormat: req.ConvertFormat,
		AutoStart:     req.AutoStart,
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
