package nativemessaging

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSupportedBrowsersCatalog(t *testing.T) {
	browsers := SupportedBrowsers()
	expectedIDs := map[string]bool{
		"chrome":  false,
		"edge":    false,
		"dia":     false,
		"arc":     false,
		"brave":   false,
		"vivaldi": false,
		"opera":   false,
		"operagx": false,
		"coccoc":  false,
		"comet":   false,
		"thorium": false,
		"firefox": false,
		"zen":     false,
	}
	for _, b := range browsers {
		if _, ok := expectedIDs[b.ID]; ok {
			expectedIDs[b.ID] = true
		}
		if b.Name == "" || b.NativeMessagingKey == "" || b.ExtensionsURL == "" || b.Family == "" {
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

	diaPaths := CandidateBrowserPaths("dia", pf, pf86, local)
	wantDia := filepath.Join(local, "Microsoft", "WindowsApps", "Dia.exe")
	if len(diaPaths) == 0 || diaPaths[0] != wantDia {
		t.Fatalf("diaPaths = %v, want first %q", diaPaths, wantDia)
	}

	coccocPaths := CandidateBrowserPaths("coccoc", pf, pf86, local)
	if len(coccocPaths) < 3 {
		t.Fatalf("expected >= 3 coccoc candidate paths, got %v", coccocPaths)
	}
}

func TestAllNativeMessagingKeys(t *testing.T) {
	var dia BrowserInfo
	for _, b := range SupportedBrowsers() {
		if b.ID == "dia" {
			dia = b
			break
		}
	}
	keys := dia.AllNativeMessagingKeys()
	if len(keys) < 2 {
		t.Fatalf("expected Dia to have >= 2 native messaging keys (Chrome + Dia), got %v", keys)
	}
	if !strings.Contains(keys[0], `Google\Chrome\NativeMessagingHosts`) {
		t.Fatalf("expected Dia primary key to be Chrome NativeMessagingHosts, got %q", keys[0])
	}
	if !strings.Contains(keys[1], `TheBrowserCompany\Dia\NativeMessagingHosts`) {
		t.Fatalf("expected Dia extra key to be TheBrowserCompany\\Dia, got %q", keys[1])
	}
}

func TestExtractExecutablePath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`"C:\Program Files\Google\Chrome\Application\chrome.exe"`, `C:\Program Files\Google\Chrome\Application\chrome.exe`},
		{`"C:\Program Files\Brave\brave.exe" -- "%1"`, `C:\Program Files\Brave\brave.exe`},
		{`C:\Program Files\Internet Explorer\iexplore.exe`, `C:\Program Files\Internet Explorer\iexplore.exe`},
		{`C:\Custom Browser\mybrowser.exe -url %1`, `C:\Custom Browser\mybrowser.exe`},
		{"", ""},
	}
	for _, tc := range cases {
		if got := ExtractExecutablePath(tc.in); got != tc.want {
			t.Errorf("ExtractExecutablePath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestClassifyBrowserFamily(t *testing.T) {
	if got := ClassifyBrowserFamily("dia", "Dia", `C:\Users\u\AppData\Local\Microsoft\WindowsApps\Dia.exe`); got != BrowserFamilyChromium {
		t.Fatalf("Dia family = %q, want %q", got, BrowserFamilyChromium)
	}
	if got := ClassifyBrowserFamily("Zen", "Zen Browser", `C:\Program Files\Zen Browser\zen.exe`); got != BrowserFamilyFirefox {
		t.Fatalf("Zen family = %q, want %q", got, BrowserFamilyFirefox)
	}
	if got := ClassifyBrowserFamily("IEXPLORE.EXE", "Internet Explorer", `C:\Program Files\Internet Explorer\iexplore.exe`); got != BrowserFamilyOther {
		t.Fatalf("IE family = %q, want %q", got, BrowserFamilyOther)
	}
}

func TestMergeDiscoveredBrowsers(t *testing.T) {
	catalog := SupportedBrowsers()
	// Pretend Chrome is already detected from filesystem.
	for i := range catalog {
		if catalog[i].ID == "chrome" {
			catalog[i].Installed = true
			catalog[i].ExecutablePath = `C:\Program Files\Google\Chrome\Application\chrome.exe`
		}
	}

	discovered := []StartMenuBrowserEntry{
		// Duplicate of already-installed Chrome -> should be deduplicated
		{Key: "Google Chrome", DisplayName: "Google Chrome", Command: `"C:\Program Files\Google\Chrome\Application\chrome.exe"`},
		// Legacy IE -> should be skipped
		{Key: "IEXPLORE.EXE", DisplayName: "Internet Explorer", Command: `C:\Program Files\Internet Explorer\iexplore.exe`},
		// Dia from WindowsApps -> should match catalog "dia" entry
		{Key: "Dia", DisplayName: "Dia", Command: `"C:\Users\alice\AppData\Local\Microsoft\WindowsApps\Dia.exe"`},
		// Brand-new Chromium browser registered in StartMenuInternet
		{Key: "Helium", DisplayName: "Helium Browser", Command: `"C:\Program Files\Helium\helium.exe" -- "%1"`},
		// Brand-new Firefox-family browser registered in StartMenuInternet
		{Key: "LibreWolf", DisplayName: "LibreWolf", Command: `"C:\Program Files\LibreWolf\librewolf.exe"`},
	}

	merged := MergeDiscoveredBrowsers(catalog, discovered, func(string) bool { return true })

	byID := make(map[string]BrowserInfo, len(merged))
	for _, b := range merged {
		if _, dup := byID[b.ID]; dup {
			t.Fatalf("duplicate browser ID %q in merged result", b.ID)
		}
		byID[b.ID] = b
	}

	if _, hasIE := byID["internet-explorer"]; hasIE {
		t.Fatalf("expected Internet Explorer to be skipped")
	}

	dia, ok := byID["dia"]
	if !ok || !dia.Installed || !strings.HasSuffix(dia.ExecutablePath, "Dia.exe") {
		t.Fatalf("expected Dia to be installed from discovery, got %+v", dia)
	}

	helium, ok := byID["helium-browser"]
	if !ok || !helium.Installed || helium.Family != BrowserFamilyChromium || helium.ExtensionsURL != "chrome://extensions" {
		t.Fatalf("expected dynamic Helium Browser (chromium), got %+v", helium)
	}

	lw, ok := byID["librewolf"]
	if !ok || !lw.Installed || lw.Family != BrowserFamilyFirefox || lw.ExtensionsURL != "about:addons" {
		t.Fatalf("expected dynamic LibreWolf (firefox), got %+v", lw)
	}

	// Verify installed browsers are ordered before uninstalled ones.
	seenUninstalled := false
	for _, b := range merged {
		if !b.Installed {
			seenUninstalled = true
		} else if seenUninstalled {
			t.Fatalf("installed browser %q appeared after uninstalled browser", b.ID)
		}
	}
}
