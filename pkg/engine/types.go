package engine

import (
	"context"
	"sync"
	"time"
)

// TaskStatus represents the current state of a download task.
type TaskStatus string

const (
	StatusQueued      TaskStatus = "Queued"
	StatusProbing     TaskStatus = "Probing"
	StatusDownloading TaskStatus = "Downloading"
	StatusMuxing      TaskStatus = "Muxing"
	StatusPaused      TaskStatus = "Paused"
	StatusCompleted   TaskStatus = "Completed"
	StatusFailed      TaskStatus = "Failed"
	StatusCancelled   TaskStatus = "Cancelled"
)

// Segment represents one part of a multi-segment download.
type Segment struct {
	Index      int    `json:"index"`
	Start      int64  `json:"start"`      // starting byte offset in the target file
	End        int64  `json:"end"`        // ending byte offset (inclusive)
	Downloaded int64  `json:"downloaded"` // bytes downloaded so far in this segment
	Status     string `json:"status"`     // "idle", "downloading", "done", "error"

	mu sync.RWMutex `json:"-"`
}

func (s *Segment) setStatus(status string) {
	s.mu.Lock()
	s.Status = status
	s.mu.Unlock()
}

func (s *Segment) getStatus() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Status
}

// Task represents a download job with all metadata and runtime state.
type Task struct {
	ID              string            `json:"id"`
	URL             string            `json:"url"`
	AudioURL        string            `json:"audio_url,omitempty"`
	Category        string            `json:"category,omitempty"`
	Filename        string            `json:"filename"`
	SavePath        string            `json:"save_path"`
	PartPath        string            `json:"part_path"`
	TotalBytes      int64             `json:"total_bytes"`
	DownloadedBytes int64             `json:"downloaded_bytes"`
	Speed           int64             `json:"speed"` // bytes/second
	ETA             int64             `json:"eta"`   // seconds remaining
	Status          TaskStatus        `json:"status"`
	SupportsRanges  bool              `json:"supports_ranges"`
	IsHLS           bool              `json:"is_hls,omitempty"`
	Plan            *DownloadPlan     `json:"plan,omitempty"`
	ConvertFormat   string            `json:"convert_format,omitempty"`
	NumSegments     int               `json:"num_segments"`
	Segments        []*Segment        `json:"segments"`
	ErrorMessage    string            `json:"error_message,omitempty"`
	Retries         int               `json:"retries,omitempty"` // retry attempts made so far for this task
	Referrer        string            `json:"referrer,omitempty"`
	Cookies         string            `json:"cookies,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	CompletedAt     *time.Time        `json:"completed_at,omitempty"`

	// Runtime fields (excluded from JSON persistence)
	mu         sync.RWMutex       `json:"-"`
	cancelFn   context.CancelFunc `json:"-"`
	activeCtx  context.Context    `json:"-"`
	speedCalc  *speedCalculator   `json:"-"`
	limiter    *rateLimiter       `json:"-"`
	retryLimit int                `json:"-"`
}

// DefaultMaxRetries is the retry budget used by DefaultConfig. A config value of
// 0 disables retries entirely.
const DefaultMaxRetries = 3

// Config holds download engine configuration options.
type Config struct {
	MaxConcurrentDownloads int    `json:"max_concurrent_downloads"`
	DefaultSegments        int    `json:"default_segments"`
	MaxRetries             int    `json:"max_retries"`
	SpeedLimitKBps         int64  `json:"speed_limit_kbps"` // 0 = unlimited
	DownloadDir            string `json:"download_dir"`
	StateFilePath          string `json:"state_file_path"`
	AutoCategorize         bool   `json:"auto_categorize"`
	LaunchAtStartup        bool   `json:"launch_at_startup"`
	MinimizeToTray         bool   `json:"minimize_to_tray"`
	CloseToTray            bool   `json:"close_to_tray"`
	OnboardingCompleted    bool   `json:"onboarding_completed"`
}

// DefaultConfig returns reasonable default configuration values.
func DefaultConfig(downloadDir, statePath string) Config {
	return Config{
		MaxConcurrentDownloads: 3,
		DefaultSegments:        8,
		MaxRetries:             DefaultMaxRetries,
		DownloadDir:            downloadDir,
		StateFilePath:          statePath,
		AutoCategorize:         true,
		LaunchAtStartup:        false,
		MinimizeToTray:         true,
		CloseToTray:            true,
		OnboardingCompleted:    false,
	}
}
