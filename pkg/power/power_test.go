package power

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestControllerInjectableHooks(t *testing.T) {
	var opened string
	var slept, shutDown atomic.Int32

	ctrl := &Controller{
		OpenFileFn: func(p string) error { opened = p; return nil },
		SleepFn:    func() error { slept.Add(1); return nil },
		ShutdownFn: func() error { shutDown.Add(1); return nil },
	}

	if err := ctrl.OpenFile("C:\\Downloads\\file.zip"); err != nil || opened != "C:\\Downloads\\file.zip" {
		t.Fatalf("OpenFile hook failed: err=%v opened=%q", err, opened)
	}
	if err := ctrl.Execute(ActionSleep); err != nil || slept.Load() != 1 {
		t.Fatalf("Sleep hook failed: err=%v count=%d", err, slept.Load())
	}
	if err := ctrl.Execute(ActionShutdown); err != nil || shutDown.Load() != 1 {
		t.Fatalf("Shutdown hook failed: err=%v count=%d", err, shutDown.Load())
	}
	if err := ctrl.Execute(ActionNone); err != nil {
		t.Fatalf("ActionNone failed: %v", err)
	}
}

func TestCountdownCancellationAndExecution(t *testing.T) {
	var executed atomic.Int32
	execCh := make(chan Action, 1)

	cd := NewCountdown(nil, func(act Action) {
		executed.Add(1)
		execCh <- act
	})

	// Start and cancel before expiry -> must NOT execute.
	cd.Start(ActionSleep, 150*time.Millisecond)
	active, act, secs := cd.State()
	if !active || act != ActionSleep || secs <= 0 {
		t.Fatalf("expected active sleep countdown, got active=%v act=%q secs=%d", active, act, secs)
	}
	cd.Cancel()
	time.Sleep(220 * time.Millisecond)
	if executed.Load() != 0 {
		t.Fatalf("expected cancelled countdown not to execute, got %d", executed.Load())
	}

	// Start and let expire -> must execute once.
	cd.Start(ActionShutdown, 80*time.Millisecond)
	select {
	case got := <-execCh:
		if got != ActionShutdown {
			t.Fatalf("expected ActionShutdown, got %q", got)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for countdown execution")
	}
	if active, _, _ := cd.State(); active {
		t.Fatalf("expected countdown inactive after execution")
	}
}
