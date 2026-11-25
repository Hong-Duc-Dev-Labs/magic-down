package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AddTaskRequest specifies parameters for adding a new download task.
type AddTaskRequest struct {
	URL         string            `json:"url"`
	Filename    string            `json:"filename,omitempty"`
	SaveDir     string            `json:"save_dir,omitempty"`
	NumSegments int               `json:"num_segments,omitempty"`
	Referrer    string            `json:"referrer,omitempty"`
	Cookies     string            `json:"cookies,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	AutoStart   bool              `json:"auto_start"`
}

// Engine coordinates download tasks, queues, concurrency, and persistence.
type Engine struct {
	mu           sync.RWMutex
	config       Config
	tasks        []*Task
	tasksByID    map[string]*Task
	httpClient   *http.Client
	ctx          context.Context
	cancel       context.CancelFunc
	subscribers  map[chan *Task]struct{}
	subMu        sync.RWMutex
	saveTimer    *time.Timer
	saveTimerMu  sync.Mutex
}

// NewEngine initializes a new download engine.
func NewEngine(cfg Config) (*Engine, error) {
	if cfg.MaxConcurrentDownloads <= 0 {
		cfg.MaxConcurrentDownloads = 3
	}
	if cfg.DefaultSegments <= 0 {
		cfg.DefaultSegments = 8
	}
	if cfg.DownloadDir == "" {
		cfg.DownloadDir = defaultDownloadDir()
	}
	if cfg.StateFilePath == "" {
		cfg.StateFilePath = filepath.Join(cfg.DownloadDir, ".magicdown_state.json")
	}

	_ = os.MkdirAll(cfg.DownloadDir, 0755)

	ctx, cancel := context.WithCancel(context.Background())
	eng := &Engine{
		config:      cfg,
		tasks:       make([]*Task, 0),
		tasksByID:   make(map[string]*Task),
		httpClient:  &http.Client{Timeout: 0}, // No client-level timeout for downloads
		ctx:         ctx,
		cancel:      cancel,
		subscribers: make(map[chan *Task]struct{}),
	}

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
	if req.URL == "" {
		return nil, errors.New("empty URL")
	}

	saveDir := req.SaveDir
	if saveDir == "" {
		saveDir = e.config.DownloadDir
	}
	_ = os.MkdirAll(saveDir, 0755)

	numSegs := req.NumSegments
	if numSegs <= 0 {
		numSegs = e.config.DefaultSegments
	}

	filename := ""
	if req.Filename != "" {
		filename = SanitizeFilename(req.Filename)
	}

	task := &Task{
		ID:          generateID(),
		URL:         req.URL,
		Filename:    filename,
		Status:      StatusQueued,
		NumSegments: numSegs,
		Referrer:    req.Referrer,
		Cookies:     req.Cookies,
		Headers:     req.Headers,
		CreatedAt:   time.Now(),
	}

	if task.Filename != "" {
		task.SavePath = filepath.Join(saveDir, task.Filename)
		task.PartPath = task.SavePath + ".magicpart"
	}

	e.mu.Lock()
	e.tasks = append(e.tasks, task)
	e.tasksByID[task.ID] = task
	e.mu.Unlock()

	snap := task.Clone()
	e.notify(snap)
	e.scheduleSave()

	if req.AutoStart {
		go e.processQueue()
	}

	return snap, nil
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
		if st == StatusDownloading || st == StatusProbing {
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
		if st == StatusDownloading || st == StatusProbing {
			activeCount++
		} else if st == StatusQueued && candidate == nil {
			candidate = t
		}
	}

	if activeCount >= e.config.MaxConcurrentDownloads || candidate == nil {
		e.mu.Unlock()
		return
	}

	// Mark candidate as probing while holding e.mu so no duplicate worker claims it
	candidate.SetStatus(StatusProbing)
	downloadDir := e.config.DownloadDir
	e.mu.Unlock()

	e.notify(candidate.Clone())

	go func(t *Task) {
		defer func() {
			go e.processQueue()
		}()

		// Probe if needed
		snap := t.Clone()
		if snap.TotalBytes <= 0 || snap.SavePath == "" {
			if err := t.Probe(e.ctx, e.httpClient, downloadDir); err != nil {
				t.fail(fmt.Sprintf("probe failed: %v", err), e.notify)
				e.scheduleSave()
				return
			}
			e.notify(t.Clone())
		}

		// Run download
		if err := t.Start(e.ctx, e.httpClient, e.notify); err != nil {
			// Pause or error handled internally
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
	e.subMu.RLock()
	defer e.subMu.RUnlock()

	for ch := range e.subscribers {
		select {
		case ch <- t:
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

	_ = os.MkdirAll(filepath.Dir(statePath), 0755)
	tmpPath := statePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	_ = os.Remove(statePath)
	return os.Rename(tmpPath, statePath)
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
		// If app exited while downloading/probing, reset to Paused so user can resume
		if t.Status == StatusDownloading || t.Status == StatusProbing {
			t.Status = StatusPaused
		}
		t.Speed = 0
		t.ETA = 0
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

// UpdateConfig updates the engine config.
func (e *Engine) UpdateConfig(cfg Config) {
	e.mu.Lock()
	e.config = cfg
	e.mu.Unlock()
	e.scheduleSave()
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
