package autostart

import "testing"

type memoryStore struct {
	values map[string]string
}

func newMemoryStore() *memoryStore {
	return &memoryStore{values: make(map[string]string)}
}

func (m *memoryStore) Get(name string) (string, bool, error) {
	v, ok := m.values[name]
	return v, ok, nil
}

func (m *memoryStore) Set(name, value string) error {
	m.values[name] = value
	return nil
}

func (m *memoryStore) Delete(name string) error {
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
}

func TestManagerEnableDisableIsEnabled(t *testing.T) {
	store := newMemoryStore()
	fakeExe := `C:\Program Files\MagicDown\magicdown.exe`
	mgr := NewManager(AppName, store, func() (string, error) { return fakeExe, nil })

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
