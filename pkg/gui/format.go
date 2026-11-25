package gui

import (
	"fmt"
	"time"
)

// FormatBytes formats byte counts into human-readable strings (B, KB, MB, GB).
func FormatBytes(b int64) string {
	if b < 0 {
		return "Unknown"
	}
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.2f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// FormatSpeed formats transfer rate into bytes/sec string.
func FormatSpeed(speed int64) string {
	if speed <= 0 {
		return "-"
	}
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case speed >= gb:
		return fmt.Sprintf("%.2f GB/s", float64(speed)/float64(gb))
	case speed >= mb:
		return fmt.Sprintf("%.2f MB/s", float64(speed)/float64(mb))
	case speed >= kb:
		return fmt.Sprintf("%.1f KB/s", float64(speed)/float64(kb))
	default:
		return fmt.Sprintf("%d B/s", speed)
	}
}

// FormatETA formats remaining seconds into human-readable time.
func FormatETA(sec int64) string {
	if sec <= 0 {
		return "-"
	}
	d := time.Duration(sec) * time.Second
	if d >= time.Hour {
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh %dm", h, m)
	}
	if d >= time.Minute {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}
