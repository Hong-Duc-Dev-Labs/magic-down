package engine

import (
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestScheduleInWindowSameDayAndOvernight(t *testing.T) {
	// Same-day window: 01:00 -> 06:00 on Monday (1)
	sameDay := ScheduleConfig{
		Enabled:    true,
		StartTime:  "01:00",
		StopTime:   "06:00",
		DaysOfWeek: []int{int(time.Monday)},
	}
	mon0200 := time.Date(2026, 11, 23, 2, 0, 0, 0, time.Local) // Monday
	if !sameDay.InWindow(mon0200) {
		t.Fatalf("expected Mon 02:00 to be inside 01:00-06:00")
	}
	mon0600 := time.Date(2026, 11, 23, 6, 0, 0, 0, time.Local)
	if sameDay.InWindow(mon0600) {
		t.Fatalf("expected Mon 06:00 (exclusive end) to be outside window")
	}
	tue0200 := time.Date(2026, 11, 24, 2, 0, 0, 0, time.Local) // Tuesday
	if sameDay.InWindow(tue0200) {
		t.Fatalf("expected Tue 02:00 to be outside Monday-only window")
	}

	// Overnight wrap-around window: 23:00 -> 06:00 starting Monday (1)
	overnight := ScheduleConfig{
		Enabled:    true,
		StartTime:  "23:00",
		StopTime:   "06:00",
		DaysOfWeek: []int{int(time.Monday)},
	}
	mon2330 := time.Date(2026, 11, 23, 23, 30, 0, 0, time.Local)
	if !overnight.InWindow(mon2330) {
		t.Fatalf("expected Mon 23:30 to be inside overnight window")
	}
	// Tuesday 03:00 belongs to Monday's overnight window!
	tue0300 := time.Date(2026, 11, 24, 3, 0, 0, 0, time.Local)
	if !overnight.InWindow(tue0300) {
		t.Fatalf("expected Tue 03:00 to be inside Monday's overnight window")
	}
	tue0630 := time.Date(2026, 11, 24, 6, 30, 0, 0, time.Local)
	if overnight.InWindow(tue0630) {
		t.Fatalf("expected Tue 06:30 to be outside overnight window")
	}
	tue2330 := time.Date(2026, 11, 24, 23, 30, 0, 0, time.Local)
	if overnight.InWindow(tue2330) {
		t.Fatalf("expected Tue 23:30 to be outside Monday-only overnight window")
	}
}

func TestSchedulerEdgeTriggeredTransitions(t *testing.T) {
	now := time.Date(2026, 11, 23, 22, 55, 0, 0, time.Local)
	var enters, exits atomic.Int32
	var lastStopAction string

	sched := NewScheduler(
		func() time.Time { return now },
		func() { enters.Add(1) },
		func(act string) { exits.Add(1); lastStopAction = act },
	)

	cfg := ScheduleConfig{
		Enabled:    true,
		StartTime:  "23:00",
		StopTime:   "06:00",
		StopAction: "sleep",
	}

	// Before window: no events.
	sched.Evaluate(cfg)
	if enters.Load() != 0 || exits.Load() != 0 {
		t.Fatalf("expected no events before window")
	}

	// Enter window (23:05): fires enter ONCE.
	now = time.Date(2026, 11, 23, 23, 5, 0, 0, time.Local)
	sched.Evaluate(cfg)
	sched.Evaluate(cfg) // repeated tick inside window must NOT re-fire!
	if enters.Load() != 1 || exits.Load() != 0 {
		t.Fatalf("expected exactly 1 enter, got enters=%d exits=%d", enters.Load(), exits.Load())
	}

	// Exit window (06:05 next day): fires exit ONCE with stopAction="sleep".
	now = time.Date(2026, 11, 24, 6, 5, 0, 0, time.Local)
	sched.Evaluate(cfg)
	sched.Evaluate(cfg) // repeated tick outside window must NOT re-fire!
	if enters.Load() != 1 || exits.Load() != 1 || lastStopAction != "sleep" {
		t.Fatalf("expected 1 enter, 1 exit (sleep), got enters=%d exits=%d act=%q", enters.Load(), exits.Load(), lastStopAction)
	}
}

func TestSchedulerDoesNotOverrideManuallyPausedTasks(t *testing.T) {
	tempDir := t.TempDir()
	eng, err := NewEngine(Config{
		MaxConcurrentDownloads: 2,
		DefaultSegments:        4,
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
	})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer eng.Close()

	pausedTask, err := eng.AddTask(AddTaskRequest{
		URL:         "https://example.com/manual-pause.bin",
		Filename:    "manual-pause.bin",
		StartPaused: true,
	})
	if err != nil {
		t.Fatalf("AddTask failed: %v", err)
	}

	// Simulate window entry calling processQueue().
	eng.processQueue()
	time.Sleep(40 * time.Millisecond)

	snap, _ := eng.GetTask(pausedTask.ID)
	if snap.Status != StatusPaused {
		t.Fatalf("scheduler window entry must not override manually Paused task, got %s", snap.Status)
	}
}

func TestSchedulerLoopIdleZeroTicksAndWakeOnConfig(t *testing.T) {
	tempDir := t.TempDir()
	eng, err := NewEngine(Config{
		MaxConcurrentDownloads: 2,
		DefaultSegments:        4,
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
	})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer eng.Close()

	eng.schedTickInterval.Store(int64(15 * time.Millisecond))

	// With Schedule.Enabled == false, scheduler loop must sleep with zero ticks.
	time.Sleep(60 * time.Millisecond)
	if ticks := eng.schedTicks.Load(); ticks != 0 {
		t.Fatalf("expected 0 scheduler ticks while disabled, got %d", ticks)
	}

	// Enabling schedule via UpdateConfig must immediately wake the loop and start ticking.
	cfg := eng.Config()
	cfg.Schedule = ScheduleConfig{
		Enabled:   true,
		StartTime: "00:00",
		StopTime:  "23:59",
	}
	eng.UpdateConfig(cfg)

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) && eng.schedTicks.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if eng.schedTicks.Load() == 0 {
		t.Fatalf("expected scheduler loop to tick after enabling Schedule")
	}

	// Disabling schedule must stop the ticker and return to zero wakeups.
	cfg.Schedule.Enabled = false
	eng.UpdateConfig(cfg)
	time.Sleep(35 * time.Millisecond)
	afterDisable := eng.schedTicks.Load()
	time.Sleep(60 * time.Millisecond)
	if got := eng.schedTicks.Load(); got != afterDisable {
		t.Fatalf("expected scheduler ticks to stop after disabling (before=%d, after=%d)", afterDisable, got)
	}
}
