//go:build !windows

package gui

func clipboardSequenceNumber() (uint32, bool) {
	return 0, false
}

func readClipboardText() (string, error) {
	return "", nil
}
