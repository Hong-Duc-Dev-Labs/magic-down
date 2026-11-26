package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// AddTaskRequest specifies parameters for adding a new download task.
type AddTaskRequest struct {
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
	Plan           *DownloadPlan     `json:"plan,omitempty"`
	SpeedLimitKBps int64             `json:"speed_limit_kbps,omitempty"`
	AutoStart      bool              `json:"auto_start"`
	StartPaused    bool              `json:"start_paused,omitempty"`
}

// RefreshAddressRequest specifies a replacement URL and credentials for a
// paused or failed download whose signed link expired (e.g. 403/410).
type RefreshAddressRequest struct {
	URL      string            `json:"url"`
	Referrer string            `json:"referrer,omitempty"`
	Cookies  string            `json:"cookies,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
}

// Engine coordinates download tasks, queues, concurrency, and persistence.
type Engine struct {
	mu          sync.RWMutex
	config      Config
	configPath  string
	tasks       []*Task
	tasksByID   map[string]*Task
	httpClient  atomic.Pointer[http.Client]
	limiter     *rateLimiter
	onCompleted func(*Task)
	ctx         context.Context
	cancel      context.CancelFunc
	subscribers map[chan *Task]struct{}
	subMu       sync.RWMutex
	saveTimer   *time.Timer
	saveTimerMu sync.Mutex
	saveMu      sync.Mutex
	configMu    sync.Mutex
}

// persistedConfig stores the settings that survive restarts. Pointers are used
// so a missing field in an older file does not silently reset a default.
type persistedConfig struct {
	MaxConcurrentDownloads *int    `json:"max_concurrent_downloads,omitempty"`
	DefaultSegments        *int    `json:"default_segments,omitempty"`
	MaxRetries             *int    `json:"max_retries,omitempty"`
	SpeedLimitKBps         *int64  `json:"speed_limit_kbps,omitempty"`
	DownloadDir            *string `json:"download_dir,omitempty"`
	AutoCategorize         *bool   `json:"auto_categorize,omitempty"`
	LaunchAtStartup        *bool   `json:"launch_at_startup,omitempty"`
	MinimizeToTray         *bool   `json:"minimize_to_tray,omitempty"`
	CloseToTray            *bool   `json:"close_to_tray,omitempty"`
	OnboardingCompleted    *bool   `json:"onboarding_completed,omitempty"`
	AutoCheckUpdates       *bool   `json:"auto_check_updates,omitempty"`
	UpdateRepo             *string      `json:"update_repo,omitempty"`
	Language               *string      `json:"language,omitempty"`
	Proxy                  *ProxyConfig `json:"proxy,omitempty"`
}

// NewEngine initializes a new download engine.
func NewEngine(cfg Config) (*Engine, error) {
	if cfg.MaxConcurrentDownloads <= 0 {
		cfg.MaxConcurrentDownloads = 3
	}
	if cfg.DefaultSegments <= 0 {
		cfg.DefaultSegments = 8
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = DefaultMaxRetries
	}
	if cfg.SpeedLimitKBps < 0 {
		cfg.SpeedLimitKBps = 0
	}
	if cfg.DownloadDir == "" {
		cfg.DownloadDir = defaultDownloadDir()
	}
	if cfg.StateFilePath == "" {
		cfg.StateFilePath = filepath.Join(cfg.DownloadDir, ".magicdown_state.json")
	}

	if cfg.Proxy.Mode == "" {
		cfg.Proxy.Mode = ProxyModeSystem
	}

	ctx, cancel := context.WithCancel(context.Background())
	eng := &Engine{
		config:      cfg,
		configPath:  filepath.Join(filepath.Dir(cfg.StateFilePath), "config.json"),
		tasks:       make([]*Task, 0),
		tasksByID:   make(map[string]*Task),
		limiter:     newRateLimiter(cfg.SpeedLimitKBps * 1024),
		ctx:         ctx,
		cancel:      cancel,
		subscribers: make(map[chan *Task]struct{}),
	}

	// Persisted settings override the defaults, then ensure the final directory.
	eng.loadConfig()
	eng.swapHTTPClient(eng.config.Proxy)
	_ = os.MkdirAll(eng.config.DownloadDir, 0755)

	// Load existing persisted state
	if err := eng.loadState(); err != nil {
		// Log or ignore if file doesn't exist yet
	}

	return eng, nil
}

func defaultDownloadDir() string {
	home, err := os.UserHomeDir()
	if err == nil {
		dl := filepath.Join(home, "Downloads")
		if _, err := os.Stat(dl); err == nil {
			return filepath.Join(dl, "MagicDown")
		}
	}
	return "downloads"
}

func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// AddTask creates, stores, and optionally starts a new download job.
func (e *Engine) AddTask(req AddTaskRequest) (*Task, error) {
	task, saveDir, err := e.prepareTask(req)
	if err != nil {
		return nil, err
	}

	e.mu.Lock()
	if task.Filename != "" {
		task.SavePath = UniqueFilePath(saveDir, task.Filename)
		task.Filename = filepath.Base(task.SavePath)
		task.PartPath = task.SavePath + ".magicpart"
	}
	e.tasks = append(e.tasks, task)
	e.tasksByID[task.ID] = task
	e.mu.Unlock()

	snap := task.Clone()
	e.notify(snap)
	e.scheduleSave()

	if req.AutoStart && !req.StartPaused {
		go e.processQueue()
	}

	return snap, nil
}

// AddTaskOrFindDuplicate atomically checks for an existing duplicate task
// (by normalized URL or target save path) under e.mu.Lock() before adding.
// If a duplicate exists, it returns (nil, duplicateSnapshot, nil) without
// modifying engine state.
func (e *Engine) AddTaskOrFindDuplicate(req AddTaskRequest) (*Task, *Task, error) {
	task, saveDir, err := e.prepareTask(req)
	if err != nil {
		return nil, nil, err
	}

	candidateSavePath := ""
	if task.Filename != "" {
		candidateSavePath = filepath.Join(saveDir, task.Filename)
	}

	e.mu.Lock()
	if dup := e.findDuplicateLocked(task.URL, candidateSavePath); dup != nil {
		e.mu.Unlock()
		return nil, dup, nil
	}
	if task.Filename != "" {
		task.SavePath = UniqueFilePath(saveDir, task.Filename)
		task.Filename = filepath.Base(task.SavePath)
		task.PartPath = task.SavePath + ".magicpart"
	}
	e.tasks = append(e.tasks, task)
	e.tasksByID[task.ID] = task
	e.mu.Unlock()

	snap := task.Clone()
	e.notify(snap)
	e.scheduleSave()

	if req.AutoStart && !req.StartPaused {
		go e.processQueue()
	}

	return snap, nil, nil
}

func (e *Engine) prepareTask(req AddTaskRequest) (*Task, string, error) {
	var plan *DownloadPlan
	if req.Plan != nil {
		normalized, err := req.Plan.Normalize(req.URL)
		if err != nil {
			return nil, "", err
		}
		plan = normalized
		if req.URL == "" {
			req.URL = plan.FirstURL()
		}
	}
	if req.URL == "" {
		return nil, "", errors.New("empty URL")
	}

	saveDir := req.SaveDir
	e.mu.RLock()
	cfg := e.config
	e.mu.RUnlock()

	numSegs := req.NumSegments
	if numSegs <= 0 {
		numSegs = cfg.DefaultSegments
	}

	filename := ""
	if req.Filename != "" {
		filename = SanitizeFilename(req.Filename)
	} else if plan != nil {
		// A plan replaces the page URL as the source of truth, so the page URL
		// often has no usable name (facebook.com/video or a signed CDN path).
		name := ExtractFilename(plan.FirstURL(), nil)
		if ext := plan.Extension(); ext != "" {
			name = ReplaceExtension(name, ext)
		}
		filename = SanitizeFilename(name)
	} else if req.AudioURL != "" {
		name := ExtractFilename(req.URL, nil)
		filename = SanitizeFilename(ReplaceExtension(name, "mp4"))
	}
	convertFmt, _ := NormalizeImageFormat(req.ConvertFormat)
	if filename != "" && convertFmt != "" {
		filename = ReplaceExtension(filename, convertFmt)
	}

	rawOutputFmt := req.OutputFormat
	if rawOutputFmt == "" && plan != nil {
		rawOutputFmt = plan.OutputFormat
	}
	outputFmt, _ := NormalizeOutputFormat(rawOutputFmt)

	rawBitrate := req.AudioBitrate
	if rawBitrate == "" && plan != nil {
		rawBitrate = plan.AudioBitrate
	}
	audioBitrate := ""
	if outputFmt == "mp3" {
		audioBitrate = NormalizeAudioBitrate(rawBitrate)
		if filename == "" {
			filename = SanitizeFilename(ExtractFilename(req.URL, nil))
		}
		filename = ReplaceExtension(filename, "mp3")
	}

	targetFmt := strings.ToLower(strings.TrimSpace(req.TargetFormat))
	if targetFmt == "" && plan != nil {
		targetFmt = plan.TargetFormat
	}

	category := req.Category
	if category == "" {
		if outputFmt == "mp3" {
			category = CategoryMusic
		} else if req.AudioURL != "" {
			category = CategoryVideo
		} else {
			category = CategoryForFilename(filename, req.URL)
		}
	}
	if saveDir == "" {
		saveDir = ResolveSaveDir(cfg.DownloadDir, category, cfg.AutoCategorize)
	}
	_ = os.MkdirAll(saveDir, 0755)

	initialStatus := StatusQueued
	if req.StartPaused {
		initialStatus = StatusPaused
	}

	var headersCopy map[string]string
	if len(req.Headers) > 0 {
		headersCopy = make(map[string]string, len(req.Headers))
		for k, v := range req.Headers {
			headersCopy[k] = v
		}
	}

	speedLimit := req.SpeedLimitKBps
	if speedLimit < 0 {
		speedLimit = 0
	}

	task := &Task{
		ID:             generateID(),
		URL:            req.URL,
		AudioURL:       req.AudioURL,
		Category:       category,
		Filename:       filename,
		TotalBytes:     req.TotalBytes,
		Status:         initialStatus,
		NumSegments:    numSegs,
		SpeedLimitKBps: speedLimit,
		Referrer:       req.Referrer,
		Cookies:        req.Cookies,
		Headers:        headersCopy,
		ConvertFormat:  convertFmt,
		TargetFormat:   targetFmt,
		OutputFormat:   outputFmt,
		AudioBitrate:   audioBitrate,
		Plan:           plan,
		IsHLS:          plan == nil && req.AudioURL == "" && IsHLSURL(req.URL, ""),
		CreatedAt:      time.Now(),
		limiter:        e.limiter,
		taskLimiter:    newRateLimiter(speedLimit * 1024),
		retryLimit:     cfg.MaxRetries,
		onStateDirty:   e.scheduleSave,
	}
	return task, saveDir, nil
}

// StartTask starts or resumes a specific task by ID.
func (e *Engine) StartTask(id string) error {
	e.mu.RLock()
	task, exists := e.tasksByID[id]
	e.mu.RUnlock()
	if !exists {
		return fmt.Errorf("task not found: %s", id)
	}
	st := task.GetStatus()
	if st == StatusDownloading || st == StatusProbing {
		return nil
	}
	task.SetStatus(StatusQueued)

	e.notify(task.Clone())
	e.scheduleSave()
	go e.processQueue()
	return nil
}

// PauseTask pauses an active or queued download.
func (e *Engine) PauseTask(id string) error {
	e.mu.RLock()
	task, exists := e.tasksByID[id]
	e.mu.RUnlock()

	if !exists {
		return fmt.Errorf("task not found: %s", id)
	}

	task.Pause()
	e.notify(task)
	e.scheduleSave()

	// Another queued download may now run
	go e.processQueue()
	return nil
}

// ResumeTask resumes a paused download.
func (e *Engine) ResumeTask(id string) error {
	return e.StartTask(id)
}

// CancelTask cancels an active or queued task.
func (e *Engine) CancelTask(id string) error {
	e.mu.RLock()
	task, exists := e.tasksByID[id]
	e.mu.RUnlock()

	if !exists {
		return fmt.Errorf("task not found: %s", id)
	}

	task.Cancel()
	e.notify(task)
	e.scheduleSave()

	go e.processQueue()
	return nil
}

// DeleteTask removes a task from the engine and optionally removes files on disk.
func (e *Engine) DeleteTask(id string, deleteFiles bool) error {
	e.mu.Lock()
	task, exists := e.tasksByID[id]
	if !exists {
		e.mu.Unlock()
		return fmt.Errorf("task not found: %s", id)
	}

	task.Cancel()

	delete(e.tasksByID, id)
	for i, t := range e.tasks {
		if t.ID == id {
			e.tasks = append(e.tasks[:i], e.tasks[i+1:]...)
			break
		}
	}
	e.mu.Unlock()

	if deleteFiles {
		_ = os.Remove(task.PartPath)
		_ = os.Remove(task.PartPath + ".video")
		_ = os.Remove(task.PartPath + ".audio")
		_ = os.Remove(task.SavePath)
	}

	e.scheduleSave()
	go e.processQueue()
	return nil
}

// PauseAll pauses all active tasks.
func (e *Engine) PauseAll() {
	e.mu.RLock()
	tasks := make([]*Task, len(e.tasks))
	copy(tasks, e.tasks)
	e.mu.RUnlock()

	for _, t := range tasks {
		st := t.GetStatus()
		if st == StatusDownloading || st == StatusProbing || st == StatusQueued {
			t.Pause()
			e.notify(t.Clone())
		}
	}
	e.scheduleSave()
}

// ResumeAll queues all paused or failed tasks.
func (e *Engine) ResumeAll() {
	e.mu.RLock()
	tasks := make([]*Task, len(e.tasks))
	copy(tasks, e.tasks)
	e.mu.RUnlock()

	for _, t := range tasks {
		st := t.GetStatus()
		if st == StatusPaused || st == StatusFailed {
			t.SetStatus(StatusQueued)
			e.notify(t.Clone())
		}
	}
	e.scheduleSave()
	go e.processQueue()
}

// GetTasks returns thread-safe snapshots of all current tasks.
func (e *Engine) GetTasks() []*Task {
	e.mu.RLock()
	tasks := make([]*Task, len(e.tasks))
	copy(tasks, e.tasks)
	e.mu.RUnlock()

	result := make([]*Task, len(tasks))
	for i, t := range tasks {
		result[i] = t.Clone()
	}
	return result
}

// GetTask returns a thread-safe snapshot of a single task by ID.
func (e *Engine) GetTask(id string) (*Task, error) {
	e.mu.RLock()
	task, exists := e.tasksByID[id]
	e.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("task not found: %s", id)
	}
	return task.Clone(), nil
}

// ActiveCount returns the number of currently downloading or probing tasks.
func (e *Engine) ActiveCount() int {
	e.mu.RLock()
	tasks := make([]*Task, len(e.tasks))
	copy(tasks, e.tasks)
	e.mu.RUnlock()

	count := 0
	for _, t := range tasks {
		st := t.GetStatus()
		if st == StatusDownloading || st == StatusProbing || st == StatusMuxing {
			count++
		}
	}
	return count
}

// processQueue checks the queue and starts tasks up to MaxConcurrentDownloads.
func (e *Engine) processQueue() {
	e.mu.Lock()
	activeCount := 0
	var candidate *Task

	for _, t := range e.tasks {
		st := t.GetStatus()
		if st == StatusDownloading || st == StatusProbing || st == StatusMuxing {
			activeCount++
		} else if st == StatusQueued && candidate == nil {
			candidate = t
		}
	}

	if activeCount >= e.config.MaxConcurrentDownloads || candidate == nil {
		e.mu.Unlock()
		return
	}

	// Mark candidate as probing while holding e.mu so no duplicate worker claims it.
	// The cancel func is installed in the same critical section so a pause or
	// cancel issued right after queueing can interrupt the probe.
	candidate.SetStatus(StatusProbing)
	downloadDir := e.config.DownloadDir
	maxRetries := e.config.MaxRetries
	runCtx, runCancel := context.WithCancel(e.ctx)
	done := make(chan struct{})
	candidate.mu.Lock()
	candidate.retryLimit = maxRetries
	candidate.cancelFn = runCancel
	candidate.workerDone = done
	candidate.mu.Unlock()
	e.mu.Unlock()

	e.notify(candidate.Clone())

	go func(t *Task) {
		defer func() {
			close(done)
			runCancel()
			go e.processQueue()
		}()

		// Probe if needed. A browser-supplied plan or muxed video+audio job
		// already describes what to fetch, so probing the base URL is noise.
		snap := t.Clone()
		if snap.Plan == nil && snap.AudioURL == "" && (snap.TotalBytes <= 0 || snap.SavePath == "") {
			probeDir := downloadDir
			if snap.SavePath != "" {
				probeDir = filepath.Dir(snap.SavePath)
			} else {
				probeDir = ResolveSaveDir(downloadDir, snap.Category, e.Config().AutoCategorize)
			}
			if err := t.Probe(runCtx, e.getHTTPClient(), probeDir); err != nil {
				if runCtx.Err() != nil {
					// Paused or cancelled while probing; the status is already set.
					e.notify(t.Clone())
					return
				}
				t.fail(fmt.Sprintf("probe failed: %v", err), e.notify)
				e.scheduleSave()
				return
			}
			e.notify(t.Clone())
		}

		// Run download
		if err := t.Start(runCtx, e.getHTTPClient(), e.notify); err != nil {
			// Pause or error handled internally
		} else if t.GetStatus() == StatusCompleted {
			e.notifyCompleted(t)
		}
		e.scheduleSave()
	}(candidate)

	// Recursively check if more slots are available
	go e.processQueue()
}

// Subscribe registers a listener for task updates.
func (e *Engine) Subscribe() chan *Task {
	ch := make(chan *Task, 64)
	e.subMu.Lock()
	e.subscribers[ch] = struct{}{}
	e.subMu.Unlock()
	return ch
}

// Unsubscribe removes a listener.
func (e *Engine) Unsubscribe(ch chan *Task) {
	e.subMu.Lock()
	delete(e.subscribers, ch)
	e.subMu.Unlock()
	close(ch)
}

func (e *Engine) notify(t *Task) {
	if t == nil {
		return
	}
	e.subMu.RLock()
	hasSubs := len(e.subscribers) > 0
	e.subMu.RUnlock()
	if !hasSubs {
		return
	}

	snap := t.Clone()

	e.subMu.RLock()
	defer e.subMu.RUnlock()

	for ch := range e.subscribers {
		select {
		case ch <- snap:
		default:
			// Non-blocking drop if channel is full to prevent lag
		}
	}
}

func (e *Engine) scheduleSave() {
	e.saveTimerMu.Lock()
	defer e.saveTimerMu.Unlock()

	if e.saveTimer != nil {
		e.saveTimer.Stop()
	}
	e.saveTimer = time.AfterFunc(500*time.Millisecond, func() {
		_ = e.saveState()
	})
}

func (e *Engine) saveState() error {
	e.saveMu.Lock()
	defer e.saveMu.Unlock()

	e.mu.RLock()
	tasksCopy := make([]*Task, len(e.tasks))
	for i, t := range e.tasks {
		tasksCopy[i] = t.Clone()
	}
	statePath := e.config.StateFilePath
	e.mu.RUnlock()

	data, err := json.MarshalIndent(tasksCopy, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(statePath)
	_ = os.MkdirAll(dir, 0755)
	tmpFile, err := os.CreateTemp(dir, "state-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	_ = os.Remove(statePath)
	if err := os.Rename(tmpPath, statePath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func (e *Engine) loadState() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	data, err := os.ReadFile(e.config.StateFilePath)
	if err != nil {
		return err
	}

	var loadedTasks []*Task
	if err := json.Unmarshal(data, &loadedTasks); err != nil {
		return err
	}

	for _, t := range loadedTasks {
		// If app exited while downloading/probing/muxing, reset to Paused so user can resume
		if t.Status == StatusDownloading || t.Status == StatusProbing || t.Status == StatusMuxing {
			t.Status = StatusPaused
		}
		t.Speed = 0
		t.ETA = 0
		t.limiter = e.limiter
		t.taskLimiter = newRateLimiter(t.SpeedLimitKBps * 1024)
		t.retryLimit = e.config.MaxRetries
		t.onStateDirty = e.scheduleSave
		e.tasks = append(e.tasks, t)
		e.tasksByID[t.ID] = t
	}

	return nil
}

// Config returns the active config.
func (e *Engine) Config() Config {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.config
}

// UpdateConfig updates the engine config, applies the speed limit immediately,
// re-evaluates the queue, and persists tunable settings for the next launch.
func (e *Engine) UpdateConfig(cfg Config) {
	if cfg.MaxConcurrentDownloads <= 0 {
		cfg.MaxConcurrentDownloads = 3
	}
	if cfg.DefaultSegments <= 0 {
		cfg.DefaultSegments = 8
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.SpeedLimitKBps < 0 {
		cfg.SpeedLimitKBps = 0
	}

	cfg.Proxy.Mode = NormalizeProxyMode(cfg.Proxy.Mode)

	e.mu.Lock()
	cfg.StateFilePath = e.config.StateFilePath
	e.config = cfg
	e.mu.Unlock()

	e.limiter.setRate(cfg.SpeedLimitKBps * 1024)
	e.swapHTTPClient(cfg.Proxy)
	_ = os.MkdirAll(cfg.DownloadDir, 0755)
	_ = e.saveConfig()
	e.scheduleSave()
	go e.processQueue()
}

// SetOnTaskCompleted registers a callback invoked asynchronously whenever a task
// finishes successfully. It must not block; the engine calls it on its own goroutine.
func (e *Engine) SetOnTaskCompleted(fn func(*Task)) {
	e.mu.Lock()
	e.onCompleted = fn
	e.mu.Unlock()
}

func (e *Engine) notifyCompleted(t *Task) {
	e.mu.RLock()
	fn := e.onCompleted
	e.mu.RUnlock()
	if fn == nil {
		return
	}
	snap := t.Clone()
	go func() {
		defer func() { _ = recover() }()
		fn(snap)
	}()
}

// saveConfig persists the tunable settings next to the task state file.
func (e *Engine) saveConfig() error {
	e.configMu.Lock()
	defer e.configMu.Unlock()

	cfg := e.Config()
	pc := persistedConfig{
		MaxConcurrentDownloads: &cfg.MaxConcurrentDownloads,
		DefaultSegments:        &cfg.DefaultSegments,
		MaxRetries:             &cfg.MaxRetries,
		SpeedLimitKBps:         &cfg.SpeedLimitKBps,
		DownloadDir:            &cfg.DownloadDir,
		AutoCategorize:         &cfg.AutoCategorize,
		LaunchAtStartup:        &cfg.LaunchAtStartup,
		MinimizeToTray:         &cfg.MinimizeToTray,
		CloseToTray:            &cfg.CloseToTray,
		OnboardingCompleted:    &cfg.OnboardingCompleted,
		AutoCheckUpdates:       &cfg.AutoCheckUpdates,
		UpdateRepo:             &cfg.UpdateRepo,
		Language:               &cfg.Language,
		Proxy:                  &cfg.Proxy,
	}
	data, err := json.MarshalIndent(pc, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := e.configPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	_ = os.Remove(e.configPath)
	return os.Rename(tmpPath, e.configPath)
}

// loadConfig applies previously persisted settings over the passed defaults.
func (e *Engine) loadConfig() {
	data, err := os.ReadFile(e.configPath)
	if err != nil {
		return
	}
	var pc persistedConfig
	if err := json.Unmarshal(data, &pc); err != nil {
		return
	}
	if pc.MaxConcurrentDownloads != nil && *pc.MaxConcurrentDownloads > 0 {
		e.config.MaxConcurrentDownloads = *pc.MaxConcurrentDownloads
	}
	if pc.DefaultSegments != nil && *pc.DefaultSegments > 0 {
		e.config.DefaultSegments = *pc.DefaultSegments
	}
	if pc.MaxRetries != nil && *pc.MaxRetries >= 0 {
		e.config.MaxRetries = *pc.MaxRetries
	}
	if pc.SpeedLimitKBps != nil && *pc.SpeedLimitKBps >= 0 {
		e.config.SpeedLimitKBps = *pc.SpeedLimitKBps
	}
	if pc.DownloadDir != nil && *pc.DownloadDir != "" {
		e.config.DownloadDir = *pc.DownloadDir
	}
	if pc.AutoCategorize != nil {
		e.config.AutoCategorize = *pc.AutoCategorize
	}
	if pc.LaunchAtStartup != nil {
		e.config.LaunchAtStartup = *pc.LaunchAtStartup
	}
	if pc.MinimizeToTray != nil {
		e.config.MinimizeToTray = *pc.MinimizeToTray
	}
	if pc.CloseToTray != nil {
		e.config.CloseToTray = *pc.CloseToTray
	}
	if pc.OnboardingCompleted != nil {
		e.config.OnboardingCompleted = *pc.OnboardingCompleted
	}
	if pc.AutoCheckUpdates != nil {
		e.config.AutoCheckUpdates = *pc.AutoCheckUpdates
	}
	if pc.UpdateRepo != nil {
		e.config.UpdateRepo = *pc.UpdateRepo
	}
	if pc.Language != nil {
		e.config.Language = *pc.Language
	}
	if pc.Proxy != nil {
		e.config.Proxy = *pc.Proxy
		e.config.Proxy.Mode = NormalizeProxyMode(e.config.Proxy.Mode)
	}
	e.limiter.setRate(e.config.SpeedLimitKBps * 1024)
}

func (e *Engine) getHTTPClient() *http.Client {
	if c := e.httpClient.Load(); c != nil {
		return c
	}
	c := &http.Client{Timeout: 0}
	e.httpClient.Store(c)
	return c
}

func (e *Engine) swapHTTPClient(proxyCfg ProxyConfig) {
	newClient, err := BuildHTTPClient(proxyCfg)
	if err != nil {
		// Fall back to direct transport if explicit proxy config is incomplete.
		newClient = &http.Client{Timeout: 0}
	}
	if old := e.httpClient.Swap(newClient); old != nil {
		old.CloseIdleConnections()
	}
}

// SetTaskSpeedLimit updates a single task's bandwidth ceiling in KB/s (0 = use
// global limit only). Takes effect immediately on active downloads.
func (e *Engine) SetTaskSpeedLimit(id string, kbps int64) error {
	if kbps < 0 {
		kbps = 0
	}
	e.mu.RLock()
	task, exists := e.tasksByID[id]
	e.mu.RUnlock()
	if !exists {
		return fmt.Errorf("task not found: %s", id)
	}

	task.mu.Lock()
	task.SpeedLimitKBps = kbps
	if task.taskLimiter == nil {
		task.taskLimiter = newRateLimiter(kbps * 1024)
	} else {
		task.taskLimiter.setRate(kbps * 1024)
	}
	task.mu.Unlock()

	e.notify(task.Clone())
	e.scheduleSave()
	return nil
}

// IsIdle reports whether no tasks are active (Downloading/Probing/Muxing) or
// waiting in the queue (StatusQueued), checked atomically under e.mu.RLock().
func (e *Engine) IsIdle() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, t := range e.tasks {
		st := t.GetStatus()
		if st == StatusDownloading || st == StatusProbing || st == StatusMuxing || st == StatusQueued {
			return false
		}
	}
	return true
}

// RefreshTaskAddress replaces the URL (and optionally cookies, headers,
// referrer) of a Paused or Failed task after verifying the new link's content
// length and byte-range compatibility, then resumes downloading from existing
// segments / .magicpart without losing progress.
func (e *Engine) RefreshTaskAddress(id string, req RefreshAddressRequest) error {
	newURL := strings.TrimSpace(req.URL)
	if newURL == "" || (!strings.HasPrefix(newURL, "http://") && !strings.HasPrefix(newURL, "https://")) {
		return errors.New("invalid refresh URL: must start with http:// or https://")
	}
	if _, err := url.ParseRequestURI(newURL); err != nil {
		return fmt.Errorf("invalid refresh URL: %w", err)
	}

	e.mu.RLock()
	task, exists := e.tasksByID[id]
	e.mu.RUnlock()
	if !exists {
		return fmt.Errorf("task not found: %s", id)
	}

	st := task.GetStatus()
	if st != StatusPaused && st != StatusFailed {
		return fmt.Errorf("can only refresh address for paused or failed tasks (current status: %s)", st)
	}

	// Wait for any prior worker goroutine to exit before mutating URL/Headers/Cookies.
	if err := task.waitWorkerDone(e.ctx); err != nil {
		return err
	}

	task.mu.RLock()
	st = task.Status
	expectedTotal := task.TotalBytes
	hadRanges := task.SupportsRanges
	hasSegments := len(task.Segments) > 0
	effectiveReferrer := task.Referrer
	effectiveCookies := task.Cookies
	var effectiveHeaders map[string]string
	if len(req.Headers) > 0 {
		effectiveHeaders = make(map[string]string, len(req.Headers))
		for k, v := range req.Headers {
			effectiveHeaders[k] = v
		}
	} else if len(task.Headers) > 0 {
		effectiveHeaders = make(map[string]string, len(task.Headers))
		for k, v := range task.Headers {
			effectiveHeaders[k] = v
		}
	}
	task.mu.RUnlock()

	if st != StatusPaused && st != StatusFailed {
		return fmt.Errorf("can only refresh address for paused or failed tasks (current status: %s)", st)
	}
	if strings.TrimSpace(req.Referrer) != "" {
		effectiveReferrer = strings.TrimSpace(req.Referrer)
	}
	if req.Cookies != "" {
		effectiveCookies = req.Cookies
	}

	// Probe outside e.mu and task.mu.
	probeCtx, cancel := context.WithTimeout(e.ctx, 30*time.Second)
	defer cancel()

	probedTotal, probedRanges, err := probeURLMetadata(
		probeCtx,
		e.getHTTPClient(),
		newURL,
		effectiveHeaders,
		effectiveCookies,
		effectiveReferrer,
	)
	if err != nil {
		return fmt.Errorf("failed to verify new download address: %w", err)
	}

	if expectedTotal > 0 && probedTotal > 0 && probedTotal != expectedTotal {
		return fmt.Errorf("file size mismatch: expected %d bytes, new URL returned %d bytes", expectedTotal, probedTotal)
	}
	if hasSegments && hadRanges && !probedRanges {
		return errors.New("new URL does not support byte ranges required to resume existing segments")
	}

	task.mu.Lock()
	if task.Status != StatusPaused && task.Status != StatusFailed {
		curStatus := task.Status
		task.mu.Unlock()
		return fmt.Errorf("task status changed during refresh (current status: %s)", curStatus)
	}
	task.URL = newURL
	task.Referrer = effectiveReferrer
	task.Cookies = effectiveCookies
	task.Headers = effectiveHeaders
	task.ErrorMessage = ""
	if task.TotalBytes <= 0 && probedTotal > 0 {
		task.TotalBytes = probedTotal
		task.SupportsRanges = probedRanges
	}
	task.Status = StatusQueued
	task.mu.Unlock()

	e.notify(task.Clone())
	e.scheduleSave()
	go e.processQueue()
	return nil
}

// FindDuplicate searches existing non-cancelled tasks for a match by normalized
// URL or identical target save path, returning a thread-safe snapshot if found.
func (e *Engine) FindDuplicate(rawURL, savePath string) *Task {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.findDuplicateLocked(rawURL, savePath)
}

func (e *Engine) findDuplicateLocked(rawURL, savePath string) *Task {
	targetExact, targetStripped, targetHasFile := normalizeDuplicateURL(rawURL)
	targetPath := cleanComparePath(savePath)

	for _, t := range e.tasks {
		st := t.GetStatus()
		if st == StatusCancelled {
			continue
		}
		t.mu.RLock()
		tURL := t.URL
		tSavePath := t.SavePath
		t.mu.RUnlock()

		if targetExact != "" {
			ex, stp, hasFile := normalizeDuplicateURL(tURL)
			if ex != "" && ex == targetExact {
				return t.Clone()
			}
			if targetHasFile && hasFile && targetStripped != "" && targetStripped == stp {
				return t.Clone()
			}
		}

		if targetPath != "" {
			if existingPath := cleanComparePath(tSavePath); existingPath != "" && sameFilePath(targetPath, existingPath) {
				return t.Clone()
			}
		}
	}
	return nil
}

// NormalizeDuplicateURL returns a canonical comparison key for URL deduplication.
// Exported for reuse in GUI pending-queue deduplication.
func NormalizeDuplicateURL(rawURL string) string {
	exact, stripped, hasFile := normalizeDuplicateURL(rawURL)
	if hasFile && stripped != "" {
		return stripped
	}
	return exact
}

func normalizeDuplicateURL(rawURL string) (exact string, stripped string, hasFilename bool) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", "", false
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Host == "" {
		return trimmed, "", false
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	exact = u.String()

	base := filepath.Base(u.Path)
	hasFilename = base != "" && base != "." && base != "/"

	q := u.Query()
	for k := range q {
		if isEphemeralQueryParam(k) {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	stripped = u.String()
	return exact, stripped, hasFilename
}

func isEphemeralQueryParam(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	if strings.HasPrefix(k, "x-amz-") || strings.HasPrefix(k, "x-goog-") || strings.HasPrefix(k, "utm_") {
		return true
	}
	switch k {
	case "token", "sig", "signature", "expires", "exp", "auth", "hdnts",
		"e", "h", "oe", "oh", "nonce", "ts", "timestamp", "_t", "fbclid", "gclid":
		return true
	}
	return false
}

func cleanComparePath(p string) string {
	trimmed := strings.TrimSpace(p)
	if trimmed == "" {
		return ""
	}
	return filepath.Clean(trimmed)
}

func sameFilePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// Close gracefully stops all tasks and flushes state.
func (e *Engine) Close() {
	e.cancel()
	e.PauseAll()
	e.saveTimerMu.Lock()
	if e.saveTimer != nil {
		e.saveTimer.Stop()
	}
	e.saveTimerMu.Unlock()
	_ = e.saveState()
}
