package engine

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultScheduleTickInterval = 15 * time.Second

// ParseClockHHMM parses a 24-hour "HH:MM" clock string into hour and minute.
func ParseClockHHMM(raw string) (hour, minute int, err error) {
	s := strings.TrimSpace(raw)
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid time %q: expected HH:MM", raw)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, 0, fmt.Errorf("invalid hour in %q", raw)
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("invalid minute in %q", raw)
	}
	return h, m, nil
}

// Validate checks that StartTime and StopTime are valid when Enabled is true.
func (s ScheduleConfig) Validate() error {
	if !s.Enabled {
		return nil
	}
	sh, sm, err := ParseClockHHMM(s.StartTime)
	if err != nil {
		return fmt.Errorf("invalid start_time: %w", err)
	}
	eh, em, err := ParseClockHHMM(s.StopTime)
	if err != nil {
		return fmt.Errorf("invalid stop_time: %w", err)
	}
	if sh*60+sm == eh*60+em {
		return fmt.Errorf("start_time and stop_time cannot be equal (%s)", s.StartTime)
	}
	return nil
}

// InWindow reports whether now falls inside the configured schedule window,
// supporting both same-day windows (e.g. 01:00 -> 06:00) and overnight
// wrap-around windows (e.g. 23:00 -> 06:00) with optional weekday filtering.
func (s ScheduleConfig) InWindow(now time.Time) bool {
	if !s.Enabled {
		return false
	}
	sh, sm, err := ParseClockHHMM(s.StartTime)
	if err != nil {
		return false
	}
	eh, em, err := ParseClockHHMM(s.StopTime)
	if err != nil {
		return false
	}
	startMin := sh*60 + sm
	stopMin := eh*60 + em
	if startMin == stopMin {
		return false
	}
	nowMin := now.Hour()*60 + now.Minute()

	if startMin < stopMin {
		if !scheduleDayAllowed(s.DaysOfWeek, now.Weekday()) {
			return false
		}
		return nowMin >= startMin && nowMin < stopMin
	}

	// Overnight window: startMin > stopMin (e.g. 23:00 -> 06:00).
	if nowMin >= startMin {
		return scheduleDayAllowed(s.DaysOfWeek, now.Weekday())
	}
	if nowMin < stopMin {
		yesterday := time.Weekday((int(now.Weekday()) + 6) % 7)
		return scheduleDayAllowed(s.DaysOfWeek, yesterday)
	}
	return false
}

func scheduleDayAllowed(days []int, wd time.Weekday) bool {
	if len(days) == 0 {
		return true
	}
	target := int(wd)
	for _, d := range days {
		if d == target {
			return true
		}
	}
	return false
}

// Scheduler evaluates ScheduleConfig using edge-triggered transitions so it
// never fights manual user Pause/Resume actions inside a window.
type Scheduler struct {
	mu            sync.Mutex
	lastInWindow  bool
	nowFn         func() time.Time
	onWindowEnter func()
	onWindowExit  func(stopAction string)
}

// NewScheduler creates an edge-triggered Scheduler with an injectable clock.
func NewScheduler(nowFn func() time.Time, onEnter func(), onExit func(stopAction string)) *Scheduler {
	if nowFn == nil {
		nowFn = time.Now
	}
	return &Scheduler{
		nowFn:         nowFn,
		onWindowEnter: onEnter,
		onWindowExit:  onExit,
	}
}

// Evaluate checks the current schedule window and fires onWindowEnter once on
// false -> true transitions and onWindowExit once on true -> false transitions.
func (s *Scheduler) Evaluate(cfg ScheduleConfig) {
	s.mu.Lock()
	if !cfg.Enabled {
		s.lastInWindow = false
		s.mu.Unlock()
		return
	}
	inWindow := cfg.InWindow(s.nowFn())
	prev := s.lastInWindow
	s.lastInWindow = inWindow
	s.mu.Unlock()

	if !prev && inWindow {
		if s.onWindowEnter != nil {
			s.onWindowEnter()
		}
		return
	}
	if prev && !inWindow {
		if s.onWindowExit != nil {
			s.onWindowExit(cfg.StopAction)
		}
	}
}
