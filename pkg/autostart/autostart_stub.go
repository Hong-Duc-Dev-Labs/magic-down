//go:build !windows

package autostart

func defaultStore() Store {
	return nil
}
