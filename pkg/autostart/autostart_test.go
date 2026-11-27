package autostart

import (
	"errors"
	"testing"
)

type memoryStore struct {
	values map[string]string
	getErr error
	setErr error
	delErr error
}

func newMemoryStore() *memoryStore {
	return &memoryStore{values: make(map[string]string)}
}

func (m *memoryStore) Get(name string) (string, bool, error) {
	if m.getErr != nil {
		return "", false, m.getErr
	}
	v, ok := m.values[name]
	return v, ok, nil
}

func (m *memoryStore) Set(name, value string) error {
	if m.setErr != nil {
		return m.setErr
	}
	m.values[name] = value
	return nil
}

func (m *memoryStore) Delete(name string) error {
	if m.delErr != nil {
		return m.delErr
	}
	delete(m.values, name)
	return nil
}

func TestFormatAndParseCommand(t *testing.T) {
	exe := `C:\Users\alice\AppData\Local\Programs\MagicDown\magicdown.exe`
	cmd := FormatCommand(exe, true)
	want := `"C:\Users\alice\AppData\Local\Programs\MagicDown\magicdown.exe" --minimized`
	if cmd != want {
		t.Fatalf("FormatCommand() = %q, want %q", cmd, want)
	}

	gotExe, minimized := ParseCommand(cmd)
	if gotExe != exe || !minimized {
		t.Fatalf("ParseCommand(%q) = (%q, %v), want (%q, true)", cmd, gotExe, minimized, exe)
	}

	gotExe2, min2 := ParseCommand(FormatCommand(exe, false))
	if gotExe2 != exe || min2 {
		t.Fatalf("ParseCommand(non-minimized) = (%q, %v), want (%q, false)", gotExe2, min2, exe)
	}

	// Path with spaces and pre-existing outer quotes must be normalized cleanly.
	spacedExe := `  "C:\Program Files\Magic Down\magicdown.exe"  `
	spacedCmd := FormatCommand(spacedExe, true)
	wantSpaced := `"C:\Program Files\Magic Down\magicdown.exe" --minimized`
	if spacedCmd != wantSpaced {
		t.Fatalf("FormatCommand(spaced) = %q, want %q", spacedCmd, wantSpaced)
	}
	if FormatCommand("   ", true) != "" || FormatCommand(`""`, true) != "" {
		t.Fatalf("FormatCommand on empty input should return empty string")
	}

	// Alias flags (--tray, --autostart) and unquoted paths.
	for _, tc := range []struct {
		raw     string
		wantExe string
		wantMin bool
	}{
		{`"C:\Program Files\MagicDown\magicdown.exe" --tray`, `C:\Program Files\MagicDown\magicdown.exe`, true},
		{`"C:\Program Files\MagicDown\magicdown.exe" --autostart`, `C:\Program Files\MagicDown\magicdown.exe`, true},
		{`C:\MagicDown\magicdown.exe -minimized`, `C:\MagicDown\magicdown.exe`, true},
		{`C:\MagicDown\magicdown.exe`, `C:\MagicDown\magicdown.exe`, false},
		{`   `, ``, false},
	} {
		gExe, gMin := ParseCommand(tc.raw)
		if gExe != tc.wantExe || gMin != tc.wantMin {
			t.Errorf("ParseCommand(%q) = (%q, %v), want (%q, %v)", tc.raw, gExe, gMin, tc.wantExe, tc.wantMin)
		}
	}
}

func TestManagerEnableDisableIsEnabled(t *testing.T) {
	store := newMemoryStore()
	fakeExe := `C:\Program Files\MagicDown\magicdown.exe`
	mgr := NewManager("", store, func() (string, error) { return fakeExe, nil })

	enabled, err := mgr.IsEnabled()
	if err != nil || enabled {
		t.Fatalf("initial IsEnabled() = (%v, %v), want (false, nil)", enabled, err)
	}

	if err := mgr.SetEnabled(true); err != nil {
		t.Fatalf("SetEnabled(true) failed: %v", err)
	}
	enabled, err = mgr.IsEnabled()
	if err != nil || !enabled {
		t.Fatalf("after Enable IsEnabled() = (%v, %v), want (true, nil)", enabled, err)
	}

	raw, ok, _ := store.Get(AppName)
	if !ok {
		t.Fatalf("expected registry value under %q", AppName)
	}
	gotExe, minimized := ParseCommand(raw)
	if gotExe != fakeExe || !minimized {
		t.Fatalf("stored command = %q -> (%q, %v), want (%q, true)", raw, gotExe, minimized, fakeExe)
	}

	if err := mgr.SetEnabled(false); err != nil {
		t.Fatalf("SetEnabled(false) failed: %v", err)
	}
	enabled, err = mgr.IsEnabled()
	if err != nil || enabled {
		t.Fatalf("after Disable IsEnabled() = (%v, %v), want (false, nil)", enabled, err)
	}
}

func TestManagerErrorHandlingAndNilStore(t *testing.T) {
	// Non-Windows stub behavior (nil store).
	nilMgr := NewManager(AppName, nil, func() (string, error) { return `C:\app.exe`, nil })
	if enabled, err := nilMgr.IsEnabled(); err != nil || enabled {
		t.Fatalf("nil store IsEnabled() = (%v, %v), want (false, nil)", enabled, err)
	}
	if err := nilMgr.SetEnabled(true); err == nil {
		t.Fatalf("expected error from Enable with nil store")
	}
	if err := nilMgr.SetEnabled(false); err == nil {
		t.Fatalf("expected error from Disable with nil store")
	}

	// Executable resolution failure.
	store := newMemoryStore()
	badExeMgr := NewManager(AppName, store, func() (string, error) { return "", errors.New("exe missing") })
	if err := badExeMgr.Enable(); err == nil {
		t.Fatalf("expected error when exePath fails")
	}

	// Registry read/write/delete error propagation.
	store.getErr = errors.New("access denied read")
	mgr := NewManager(AppName, store, func() (string, error) { return `C:\MagicDown\magicdown.exe`, nil })
	if _, err := mgr.IsEnabled(); err == nil {
		t.Fatalf("expected IsEnabled to propagate Get error")
	}
	store.getErr = nil
	store.setErr = errors.New("access denied write")
	if err := mgr.SetEnabled(true); err == nil {
		t.Fatalf("expected SetEnabled(true) to propagate Set error")
	}
	store.setErr = nil
	store.delErr = errors.New("access denied delete")
	if err := mgr.SetEnabled(false); err == nil {
		t.Fatalf("expected SetEnabled(false) to propagate Delete error")
	}
}
