package nativemessaging

import (
	"path/filepath"
	"testing"
)

func TestSupportedBrowsersCatalog(t *testing.T) {
	browsers := SupportedBrowsers()
	expectedIDs := map[string]bool{
		"chrome":  false,
		"edge":    false,
		"brave":   false,
		"vivaldi": false,
		"opera":   false,
		"coccoc":  false,
	}
	for _, b := range browsers {
		if _, ok := expectedIDs[b.ID]; ok {
			expectedIDs[b.ID] = true
		}
		if b.Name == "" || b.NativeMessagingKey == "" || b.ExtensionsURL == "" {
			t.Fatalf("incomplete browser entry: %+v", b)
		}
	}
	for id, found := range expectedIDs {
		if !found {
			t.Fatalf("missing expected browser ID %q in SupportedBrowsers()", id)
		}
	}
}

func TestCandidateBrowserPaths(t *testing.T) {
	pf := `C:\Program Files`
	pf86 := `C:\Program Files (x86)`
	local := `C:\Users\alice\AppData\Local`

	chromePaths := CandidateBrowserPaths("chrome", pf, pf86, local)
	if len(chromePaths) < 3 {
		t.Fatalf("expected >= 3 chrome candidate paths, got %v", chromePaths)
	}
	wantFirst := filepath.Join(pf, "Google", "Chrome", "Application", "chrome.exe")
	if chromePaths[0] != wantFirst {
		t.Fatalf("chromePaths[0] = %q, want %q", chromePaths[0], wantFirst)
	}

	coccocPaths := CandidateBrowserPaths("coccoc", pf, pf86, local)
	if len(coccocPaths) < 3 {
		t.Fatalf("expected >= 3 coccoc candidate paths, got %v", coccocPaths)
	}
}
