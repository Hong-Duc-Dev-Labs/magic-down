package i18n

import (
	"strings"
	"testing"
)

func TestCatalogKeyParity(t *testing.T) {
	en := Catalog(LangEN)
	vi := Catalog(LangVI)

	if len(en) == 0 {
		t.Fatal("english catalog must not be empty")
	}
	if len(vi) == 0 {
		t.Fatal("vietnamese catalog must not be empty")
	}

	for k, v := range en {
		if strings.TrimSpace(v) == "" {
			t.Errorf("en key %q has empty value", k)
		}
		viVal, ok := vi[k]
		if !ok {
			t.Errorf("missing vi translation for key %q", k)
			continue
		}
		if strings.TrimSpace(viVal) == "" {
			t.Errorf("vi key %q has empty value", k)
		}
	}

	for k := range vi {
		if _, ok := en[k]; !ok {
			t.Errorf("extra vi key %q not present in en catalog", k)
		}
	}
}

func TestNormalizeAndSwitch(t *testing.T) {
	prev := CurrentLang()
	defer SetLang(prev)

	cases := map[string]Lang{
		"vi":    LangVI,
		"vi-VN": LangVI,
		"VI_vn": LangVI,
		"en":    LangEN,
		"en-US": LangEN,
		"fr-FR": LangEN,
		"":      LangEN,
	}
	for raw, want := range cases {
		if got := Normalize(raw); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", raw, got, want)
		}
	}

	SetLang(LangVI)
	if CurrentLang() != LangVI {
		t.Fatalf("expected LangVI, got %q", CurrentLang())
	}
	if got := T("toolbar.new_download"); got != "Thêm liên kết" {
		t.Errorf("unexpected vi translation: %q", got)
	}

	SetLang(LangEN)
	if got := T("toolbar.new_download"); got != "New Download" {
		t.Errorf("unexpected en translation: %q", got)
	}

	if got := Tf("msg.cleared_done", 3); got != "Cleared 3 completed download(s)" {
		t.Errorf("unexpected formatted string: %q", got)
	}
}
