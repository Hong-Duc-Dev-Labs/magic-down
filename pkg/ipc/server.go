package ipc

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"magicdown/pkg/engine"
	"magicdown/pkg/version"
)

const (
	DefaultPort   = 28888
	MaxBatchItems = 2000
)

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
	YTDLPFormat   string            `json:"ytdlp_format,omitempty"`
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

// HelloRequest is sent by Native Messaging Host when an extension initiates HELLO handshake.
type HelloRequest struct {
	Type             string `json:"type"`
	ProtocolVersion  int    `json:"protocolVersion"`
	ExtensionVersion string `json:"extensionVersion"`
	Browser          string `json:"browser"`
	HostVersion      string `json:"hostVersion,omitempty"`
}

// WelcomeResponse is returned by /handshake/hello upon valid handshake.
type WelcomeResponse struct {
	Type            string `json:"type"`
	ProtocolVersion int    `json:"protocolVersion"`
	HostVersion     string `json:"hostVersion"`
	AppVersion      string `json:"appVersion"`
	Status          string `json:"status"`
}

// HandshakeRecord tracks real extension connectivity per browser.
type HandshakeRecord struct {
	Browser          string    `json:"browser"`
	ExtensionVersion string    `json:"extensionVersion"`
	ProtocolVersion  int       `json:"protocolVersion"`
	HostVersion      string    `json:"hostVersion"`
	Connected        bool      `json:"connected"`
	LastSeenAt       time.Time `json:"lastSeenAt"`
}

// Server provides the local HTTP IPC server for MagicDown desktop app.
type Server struct {
	engine             *engine.Engine
	port               int
	httpServer         *http.Server
	listener           net.Listener
	mu                 sync.Mutex
	running            bool
	authToken          string
	tokenPath          string
	handshakeTTL       time.Duration
	handshakes         map[string]HandshakeRecord
	onIncomingDownload func(AddDownloadRequest)
	onHandshakeChange  func(browser string, connected bool)
}

// SetOnIncomingDownload registers an optional callback that intercepts incoming
// IPC download requests (e.g. to display the Download Info Dialog in the GUI).
func (s *Server) SetOnIncomingDownload(fn func(AddDownloadRequest)) {
	s.mu.Lock()
	s.onIncomingDownload = fn
	s.mu.Unlock()
}

// SetOnHandshakeChange registers an optional callback invoked whenever a browser
// extension connects (HELLO) or disconnects, so the GUI can refresh immediately.
func (s *Server) SetOnHandshakeChange(fn func(browser string, connected bool)) {
	s.mu.Lock()
	s.onHandshakeChange = fn
	s.mu.Unlock()
}

// NewServer creates a new IPC server. If port < 0, it uses DefaultPort (28888).
// If port == 0, it binds to an OS-chosen ephemeral port.
func NewServer(eng *engine.Engine, port int) *Server {
	if port < 0 {
		port = DefaultPort
	}
	return &Server{
		engine:       eng,
		port:         port,
		tokenPath:    DefaultTokenPath(),
		handshakeTTL: 150 * time.Second,
		handshakes:   make(map[string]HandshakeRecord),
	}
}

// SetTokenPath overrides the file path where the IPC auth token is persisted.
// Passing an empty string disables writing the token to disk.
func (s *Server) SetTokenPath(path string) {
	s.mu.Lock()
	s.tokenPath = strings.TrimSpace(path)
	s.mu.Unlock()
}

// AuthToken returns the active session IPC auth token.
func (s *Server) AuthToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authToken
}

// SetHandshakeTTL overrides the expiration window for extension heartbeats (used in tests).
func (s *Server) SetHandshakeTTL(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d > 0 {
		s.handshakeTTL = d
	}
}

// GetHandshakeSnapshots returns a copy of current browser handshake states,
// automatically marking expired entries as disconnected.
func (s *Server) GetHandshakeSnapshots() map[string]HandshakeRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	out := make(map[string]HandshakeRecord, len(s.handshakes))
	for k, rec := range s.handshakes {
		if rec.Connected && s.handshakeTTL > 0 && now.Sub(rec.LastSeenAt) > s.handshakeTTL {
			rec.Connected = false
		}
		out[k] = rec
	}
	return out
}

// Start begins listening on localhost.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	if s.authToken == "" {
		tok, err := GenerateAuthToken()
		if err != nil {
			return fmt.Errorf("failed to generate IPC auth token: %w", err)
		}
		s.authToken = tok
	}
	if s.tokenPath != "" {
		if err := WriteAuthToken(s.tokenPath, s.authToken); err != nil {
			return fmt.Errorf("failed to write IPC auth token: %w", err)
		}
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
	mux.HandleFunc("/handshake/hello", s.handleHandshakeHello)
	mux.HandleFunc("/handshake/disconnect", s.handleHandshakeDisconnect)
	mux.HandleFunc("/handshake/status", s.handleHandshakeStatus)
	mux.HandleFunc("/downloads", s.handleDownloads)
	mux.HandleFunc("/downloads/add", s.handleAddDownload)
	mux.HandleFunc("/downloads/batch", s.handleAddBatchDownload)
	mux.HandleFunc("/downloads/pause", s.handlePause)
	mux.HandleFunc("/downloads/resume", s.handleResume)
	mux.HandleFunc("/downloads/delete", s.handleDelete)
	mux.HandleFunc("/youtube/resolve", s.handleResolveYouTube)
	mux.HandleFunc("/config", s.handleConfig)

	s.httpServer = &http.Server{
		Handler:      s.securityMiddleware(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 35 * time.Second,
	}

	go func() {
		_ = s.httpServer.Serve(ln)
	}()

	return nil
}

func (s *Server) securityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reject any cross-origin browser requests (no Access-Control-Allow-Origin: *).
		if strings.TrimSpace(r.Header.Get("Origin")) != "" {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
		s.mu.Lock()
		expectedToken := s.authToken
		s.mu.Unlock()
		provided := strings.TrimSpace(r.Header.Get(TokenHeader))
		if !constantTimeTokenEqual(expectedToken, provided) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
		next.ServeHTTP(w, r)
	})
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
	if s.tokenPath != "" && ReadAuthToken(s.tokenPath) == s.authToken {
		_ = os.Remove(s.tokenPath)
	}
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
		Version:         version.Version,
		ActiveDownloads: s.engine.ActiveCount(),
		TotalTasks:      len(tasks),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleHandshakeHello(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req HelloRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid json: %v", err), http.StatusBadRequest)
		return
	}
	if req.ProtocolVersion <= 0 {
		req.ProtocolVersion = 1
	}
	browser := strings.ToLower(strings.TrimSpace(req.Browser))
	if browser == "" {
		browser = "chrome"
	}
	hostVer := strings.TrimSpace(req.HostVersion)
	if hostVer == "" {
		hostVer = version.Version
	}

	s.mu.Lock()
	s.handshakes[browser] = HandshakeRecord{
		Browser:          browser,
		ExtensionVersion: strings.TrimSpace(req.ExtensionVersion),
		ProtocolVersion:  req.ProtocolVersion,
		HostVersion:      hostVer,
		Connected:        true,
		LastSeenAt:       time.Now(),
	}
	onChange := s.onHandshakeChange
	s.mu.Unlock()
	if onChange != nil {
		onChange(browser, true)
	}

	resp := WelcomeResponse{
		Type:            "WELCOME",
		ProtocolVersion: 1,
		HostVersion:     hostVer,
		AppVersion:      version.Version,
		Status:          "connected",
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleHandshakeDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Browser string `json:"browser"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	browser := strings.ToLower(strings.TrimSpace(req.Browser))
	if browser != "" {
		s.mu.Lock()
		if rec, ok := s.handshakes[browser]; ok {
			rec.Connected = false
			s.handshakes[browser] = rec
		}
		onChange := s.onHandshakeChange
		s.mu.Unlock()
		if onChange != nil {
			onChange(browser, false)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
}

func (s *Server) handleHandshakeStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.GetHandshakeSnapshots())
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

	if err := ValidateHTTPURL(req.URL); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.AudioURL) != "" {
		if err := ValidateHTTPURL(req.AudioURL); err != nil {
			http.Error(w, "invalid audio_url: "+err.Error(), http.StatusBadRequest)
			return
		}
	}
	if err := ValidateFilename(req.Filename); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := ValidateSaveDir(s.engine.Config().DownloadDir, req.SaveDir); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := engine.ValidateYTDLPFormat(req.YTDLPFormat); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		YTDLPFormat:   req.YTDLPFormat,
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

// ResolveYouTubeRequest is the JSON payload for /youtube/resolve.
type ResolveYouTubeRequest struct {
	URL string `json:"url"`
}

// ResolveYouTubeResponse is returned by /youtube/resolve.
type ResolveYouTubeResponse struct {
	Success      bool                         `json:"success"`
	YouTube      *engine.YouTubeResolveResult `json:"youtube,omitempty"`
	YTDLPMissing bool                         `json:"ytdlp_missing,omitempty"`
	Error        string                       `json:"error,omitempty"`
}

func (s *Server) handleResolveYouTube(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req ResolveYouTubeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid json: %v", err), http.StatusBadRequest)
		return
	}
	res, err := engine.ResolveYouTube(r.Context(), req.URL)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		resp := ResolveYouTubeResponse{
			Success:      false,
			YTDLPMissing: engine.IsYTDLPMissingError(err),
			Error:        err.Error(),
		}
		_ = json.NewEncoder(w).Encode(resp)
		return
	}
	_ = json.NewEncoder(w).Encode(ResolveYouTubeResponse{
		Success: true,
		YouTube: res,
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
	if len(req.Items) > MaxBatchItems {
		http.Error(w, fmt.Sprintf("too many batch items: max %d", MaxBatchItems), http.StatusBadRequest)
		return
	}

	baseDir := s.engine.Config().DownloadDir
	resp := BatchDownloadResponse{
		Success: true,
	}

	for _, item := range req.Items {
		if err := ValidateHTTPURL(item.URL); err != nil {
			resp.Failed++
			resp.Errors = append(resp.Errors, err.Error())
			continue
		}
		if strings.TrimSpace(item.AudioURL) != "" {
			if err := ValidateHTTPURL(item.AudioURL); err != nil {
				resp.Failed++
				resp.Errors = append(resp.Errors, "invalid audio_url: "+err.Error())
				continue
			}
		}
		if err := ValidateFilename(item.Filename); err != nil {
			resp.Failed++
			resp.Errors = append(resp.Errors, err.Error())
			continue
		}
		if err := ValidateSaveDir(baseDir, item.SaveDir); err != nil {
			resp.Failed++
			resp.Errors = append(resp.Errors, err.Error())
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
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
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
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
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
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
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
