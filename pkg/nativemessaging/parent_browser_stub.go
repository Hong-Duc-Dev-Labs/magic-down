//go:build !windows

package nativemessaging

// DetectParentBrowserID is only implemented on Windows; other platforms rely on
// the browser ID reported by the extension in HELLO.
func DetectParentBrowserID() string {
	return ""
}
