//go:build !windows

package i18n

import "os"

// DetectSystemLang inspects standard POSIX locale environment variables.
func DetectSystemLang() Lang {
	for _, env := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(env); v != "" {
			return Normalize(v)
		}
	}
	return LangEN
}
