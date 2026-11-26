package power

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Action identifies what MagicDown should do when all downloads finish.
type Action string

const (
	ActionNone     Action = "none"
	ActionSleep    Action = "sleep"
	ActionShutdown Action = "shutdown"
)

// NormalizeAction converts a raw string into a supported Action.
func NormalizeAction(raw string) Action {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(ActionSleep):
		return ActionSleep
	case string(ActionShutdown):
		return ActionShutdown
	default:
		return ActionNone
	}
}

// Controller executes OS file-open and system power actions. Hooks can be
// overridden in tests so power operations never touch the host machine.
type Controller struct {
	OpenFileFn func(path string) error
	SleepFn    func() error
	ShutdownFn func() error
}

// NewController returns a Controller wired to the platform's real OS actions.
func NewController() *Controller {
	return &Controller{
		OpenFileFn: defaultOpenFile,
		SleepFn:    defaultSleep,
		ShutdownFn: defaultShutdown,
	}
}

// OpenFile opens a file with the OS default handler.
func (c *Controller) OpenFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if c != nil && c.OpenFileFn != nil {
		return c.OpenFileFn(path)
	}
	return defaultOpenFile(path)
}

// Execute runs the requested power action (Sleep or Shutdown).
func (c *Controller) Execute(action Action) error {
	switch NormalizeAction(string(action)) {
	case ActionNone:
		return nil
	case ActionSleep:
		if c != nil && c.SleepFn != nil {
			return c.SleepFn()
		}
		return defaultSleep()
	case ActionShutdown:
		if c != nil && c.ShutdownFn != nil {
			return c.ShutdownFn()
		}
		return defaultShutdown()
	default:
		return fmt.Errorf("unsupported power action: %q", action)
	}
}

// DefaultCountdownDuration is the 30-second grace period before Sleep/Shutdown.
const DefaultCountdownDuration = 30 * time.Second

// Countdown manages a cancelable countdown timer before executing a power action.
type Countdown struct {
	mu        sync.Mutex
	active    bool
	action    Action
	deadline  time.Time
	stopCh    chan struct{}
	onTick    func()
	onExecute func(Action)
}

// NewCountdown creates a new inactive Countdown manager.
func NewCountdown(onTick func(), onExecute func(Action)) *Countdown {
	return &Countdown{
		onTick:    onTick,
		onExecute: onExecute,
	}
}

// Start begins (or restarts) a countdown for action over duration d.
func (cd *Countdown) Start(action Action, d time.Duration) {
	act := NormalizeAction(string(action))
	if act == ActionNone {
		cd.Cancel()
		return
	}
	if d <= 0 {
		d = DefaultCountdownDuration
	}

	cd.mu.Lock()
	if cd.stopCh != nil {
		close(cd.stopCh)
	}
	stopCh := make(chan struct{})
	cd.stopCh = stopCh
	cd.active = true
	cd.action = act
	cd.deadline = time.Now().Add(d)
	cd.mu.Unlock()

	if cd.onTick != nil {
		cd.onTick()
	}

	go func() {
		tickInterval := 250 * time.Millisecond
		if d < tickInterval {
			tickInterval = d / 4
			if tickInterval < 5*time.Millisecond {
				tickInterval = 5 * time.Millisecond
			}
		}
		ticker := time.NewTicker(tickInterval)
		defer ticker.Stop()

		for {
			select {
			case <-stopCh:
				return
			case now := <-ticker.C:
				cd.mu.Lock()
				if !cd.active || cd.stopCh != stopCh {
					cd.mu.Unlock()
					return
				}
				expired := !now.Before(cd.deadline)
				targetAction := cd.action
				if expired {
					cd.active = false
					cd.stopCh = nil
				}
				cd.mu.Unlock()

				if cd.onTick != nil {
					cd.onTick()
				}
				if expired {
					if cd.onExecute != nil {
						cd.onExecute(targetAction)
					}
					return
				}
			}
		}
	}()
}

// Cancel stops any active countdown without executing the action.
func (cd *Countdown) Cancel() {
	cd.mu.Lock()
	wasActive := cd.active
	cd.active = false
	cd.action = ActionNone
	if cd.stopCh != nil {
		close(cd.stopCh)
		cd.stopCh = nil
	}
	cd.mu.Unlock()

	if wasActive && cd.onTick != nil {
		cd.onTick()
	}
}

// State returns whether a countdown is active, its target Action, and whole
// seconds remaining (rounded up, minimum 0).
func (cd *Countdown) State() (active bool, action Action, remainingSeconds int) {
	cd.mu.Lock()
	defer cd.mu.Unlock()
	if !cd.active {
		return false, ActionNone, 0
	}
	rem := time.Until(cd.deadline)
	if rem <= 0 {
		return true, cd.action, 0
	}
	secs := int((rem + time.Second - 1) / time.Second)
	return true, cd.action, secs
}
