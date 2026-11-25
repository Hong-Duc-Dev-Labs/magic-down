package engine

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestParseHLSInitURI(t *testing.T) {
	tests := []struct {
		name string
		base string
		body string
		want string
	}{
		{
			name: "relative map resolved against playlist",
			base: "https://cdn.example.com/hls/master/index.m3u8",
			body: "#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:4,\nseg1.m4s\n",
			want: "https://cdn.example.com/hls/master/init.mp4",
		},
		{
			name: "absolute map with additional attributes",
			base: "https://cdn.example.com/index.m3u8",
			body: "#EXTM3U\n#EXT-X-MAP:BYTERANGE=\"720@0\",URI=\"https://media.example.com/init.mp4\"\n",
			want: "https://media.example.com/init.mp4",
		},
		{
			name: "no map section",
			base: "https://cdn.example.com/index.m3u8",
			body: "#EXTM3U\n#EXTINF:4,\nseg1.ts\n",
			want: "",
		},
		{
			name: "empty body",
			base: "https://cdn.example.com/index.m3u8",
			body: "",
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseHLSInitURI(tc.base, tc.body); got != tc.want {
				t.Errorf("ParseHLSInitURI() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestHLSFMP4InitSegmentDownload proves the EXT-X-MAP init section is fetched
// first, so fMP4 HLS output is playable instead of a bare list of media parts.
func TestHLSFMP4InitSegmentDownload(t *testing.T) {
	initSeg := []byte("FMP4-INIT-MOOV")
	media1 := bytes.Repeat([]byte{0x11, 0x22, 0x33}, 1024)
	media2 := bytes.Repeat([]byte{0x44, 0x55}, 2048)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			fmt.Fprintln(w, "#EXTM3U")
			fmt.Fprintln(w, "#EXT-X-STREAM-INF:BANDWIDTH=400000,RESOLUTION=426x240")
			fmt.Fprintln(w, "v240.m3u8")
		case "/v240.m3u8":
			fmt.Fprintln(w, "#EXTM3U")
			fmt.Fprintln(w, "#EXT-X-VERSION:6")
			fmt.Fprintln(w, `#EXT-X-MAP:URI="init.mp4"`)
			fmt.Fprintln(w, "#EXTINF:4.000,")
			fmt.Fprintln(w, "s1.m4s")
			fmt.Fprintln(w, "#EXTINF:4.000,")
			fmt.Fprintln(w, "s2.m4s")
			fmt.Fprintln(w, "#EXT-X-ENDLIST")
		case "/init.mp4":
			_, _ = w.Write(initSeg)
		case "/s1.m4s":
			_, _ = w.Write(media1)
		case "/s2.m4s":
			_, _ = w.Write(media2)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	want := sha256.Sum256(bytes.Join([][]byte{initSeg, media1, media2}, nil))

	tempDir := t.TempDir()
	eng, err := NewEngine(Config{
		MaxConcurrentDownloads: 2,
		DefaultSegments:        2,
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	task, err := eng.AddTask(AddTaskRequest{
		URL:       server.URL + "/master.m3u8",
		Filename:  "clip.mp4",
		AutoStart: true,
	})
	if err != nil {
		t.Fatalf("AddTask failed: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	var latest *Task
	for time.Now().Before(deadline) {
		latest, _ = eng.GetTask(task.ID)
		if latest.Status == StatusCompleted || latest.Status == StatusFailed {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if latest == nil || latest.Status != StatusCompleted {
		t.Fatalf("HLS fMP4 task failed: %+v", latest)
	}
	assertFileHash(t, latest.SavePath, want)
}
