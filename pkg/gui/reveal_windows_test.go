//go:build windows

package gui

import "testing"

func TestBuildExplorerCmdLines_WindowsSpecialPaths(t *testing.T) {
	paths := []string{
		`C:\Users\hongducdev\Downloads\Video\@8daytp - 이 영상 너무 귀엽고 ㅈㄴ중독돼서 계속 보는 중.mp4`,
		`D:\Tải xuống\Documents\Báo cáo [2026], bản cuối (1).pdf`,
	}

	for _, p := range paths {
		gotSelect := buildExplorerSelectCmdLine(p)
		wantSelect := `explorer.exe /select,"` + p + `"`
		if gotSelect != wantSelect {
			t.Errorf("buildExplorerSelectCmdLine(%q) = %q, want %q", p, gotSelect, wantSelect)
		}

		gotDir := buildExplorerDirCmdLine(p)
		wantDir := `explorer.exe "` + p + `"`
		if gotDir != wantDir {
			t.Errorf("buildExplorerDirCmdLine(%q) = %q, want %q", p, gotDir, wantDir)
		}
	}
}
