package engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseDownloadPlan(t *testing.T) {
	longParts := make([]PlanPart, maxPlanParts+1)
	for i := range longParts {
		longParts[i] = PlanPart{URL: fmt.Sprintf("https://cdn.example.com/seg-%d.m4s", i)}
	}

	tests := []struct {
		name      string
		raw       string
		fallback  string
		wantKind  string
		wantURL   string
		wantParts int
		wantFmt   string
		wantSize  int64
		wantErr   bool
	}{
		{
			name:      "range plan inherits base URL and kind",
			raw:       `{"url":"https://cdn.example.com/v.mp4","format":"MP4","parts":[{"start":0,"end":99},{"start":200,"end":299}]}`,
			wantKind:  "range",
			wantURL:   "https://cdn.example.com/v.mp4",
			wantParts: 2,
			wantFmt:   "mp4",
			wantSize:  200,
		},
		{
			name:      "segments plan with mixed hosts",
			raw:       `{"kind":"segments","parts":[{"url":"https://a.example.com/init.m4s"},{"url":"https://b.example.com/seg1.m4s"},{"url":"https://b.example.com/seg2.m4s"}]}`,
			fallback:  "https://page.example.com/watch",
			wantKind:  "segments",
			wantURL:   "https://page.example.com/watch",
			wantParts: 3,
			wantSize:  0,
		},
		{
			name:      "parts inherit fallback URL for ranges",
			raw:       `{"parts":[{"start":10,"end":19}]}`,
			fallback:  "https://cdn.example.com/base.mp4",
			wantKind:  "range",
			wantURL:   "https://cdn.example.com/base.mp4",
			wantParts: 1,
			wantSize:  10,
		},
		{
			name:      "open ended tail is unknown size",
			raw:       `{"url":"https://cdn.example.com/v.mp4","parts":[{"start":0,"end":100},{"start":101}]}`,
			wantKind:  "range",
			wantURL:   "https://cdn.example.com/v.mp4",
			wantParts: 2,
			wantSize:  0,
		},
		{name: "missing parts", raw: `{"url":"https://cdn.example.com/v.mp4"}`, wantErr: true},
		{name: "unsupported scheme", raw: `{"parts":[{"url":"javascript:alert(1)"}]}`, wantErr: true},
		{name: "relative URL", raw: `{"parts":[{"url":"/seg1.m4s"}]}`, wantErr: true},
		{name: "reversed range", raw: `{"parts":[{"url":"https://cdn.example.com/v.mp4","start":90,"end":10}]}`, wantErr: true},
		{name: "negative start", raw: `{"parts":[{"url":"https://cdn.example.com/v.mp4","start":-1,"end":10}]}`, wantErr: true},
		{name: "unknown kind", raw: `{"kind":"weird","parts":[{"url":"https://cdn.example.com/v.mp4"}]}`, wantErr: true},
		{name: "range kind spanning two URLs", raw: `{"kind":"range","parts":[{"url":"https://a.example.com/1.mp4"},{"url":"https://b.example.com/2.mp4"}]}`, wantErr: true},
		{name: "too many parts", raw: mustJSON(DownloadPlan{Parts: longParts}), wantErr: true},
		{name: "not json", raw: `not json`, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ParseDownloadPlan([]byte(tc.raw), tc.fallback)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got plan %+v", p)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.Kind != tc.wantKind {
				t.Errorf("kind = %q, want %q", p.Kind, tc.wantKind)
			}
			if p.URL != tc.wantURL {
				t.Errorf("url = %q, want %q", p.URL, tc.wantURL)
			}
			if len(p.Parts) != tc.wantParts {
				t.Errorf("parts = %d, want %d", len(p.Parts), tc.wantParts)
			}
			if p.Format != tc.wantFmt {
				t.Errorf("format = %q, want %q", p.Format, tc.wantFmt)
			}
			if got := p.KnownSize(); got != tc.wantSize {
				t.Errorf("known size = %d, want %d", got, tc.wantSize)
			}
			for i, part := range p.Parts {
				if part.URL == "" {
					t.Errorf("part %d has empty URL after normalization", i)
				}
			}
		})
	}
}

func TestDownloadPlanExtension(t *testing.T) {
	tests := []struct {
		name string
		plan DownloadPlan
		want string
	}{
		{name: "explicit format wins", plan: DownloadPlan{Format: "ts", Parts: []PlanPart{{URL: "https://cdn.example.com/a.mp4"}}}, want: "ts"},
		{name: "segment extension", plan: DownloadPlan{Parts: []PlanPart{{URL: "https://cdn.example.com/a.m4s"}}}, want: "m4s"},
		{name: "manifest extension ignored", plan: DownloadPlan{Parts: []PlanPart{{URL: "https://cdn.example.com/a.mpd"}}}, want: ""},
		{name: "nil plan", plan: DownloadPlan{}, want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.plan.Extension(); got != tc.want {
				t.Errorf("Extension() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseContentRangeStart(t *testing.T) {
	cases := map[string]int64{
		"bytes 100-200/300": 100,
		"bytes 0-0/1234":    0,
		"":                  -1,
		"items 1-2/3":       -1,
		"bytes */1234":      -1,
	}
	for header, want := range cases {
		if got := parseContentRangeStart(header); got != want {
			t.Errorf("parseContentRangeStart(%q) = %d, want %d", header, got, want)
		}
	}
}

// TestPlanDownloadByteRanges concatenates three byte ranges of one URL, which is
// the Facebook single-file DASH shape.
func TestPlanDownloadByteRanges(t *testing.T) {
	payload := make([]byte, 4096)
	for i := range payload {
		payload[i] = byte(i * 7)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v.mp4" {
			http.NotFound(w, r)
			return
		}
		serveBytesWithRanges(t, w, r, payload)
	}))
	defer server.Close()

	wantBytes := bytes.Join([][]byte{payload[0:100], payload[1000:1500], payload[3000:]}, nil)
	wantHash := sha256.Sum256(wantBytes)

	plan := DownloadPlan{
		Kind:   "range",
		URL:    server.URL + "/v.mp4",
		Format: "mp4",
		Parts: []PlanPart{
			{Start: 0, End: 99},
			{Start: 1000, End: 1499},
			{Start: 3000}, // open ended: to EOF
		},
	}
	task := runPlanTask(t, AddTaskRequest{
		URL:       server.URL + "/watch?v=1",
		Filename:  "probe.mp4",
		Plan:      &plan,
		AutoStart: true,
	})
	if task.Status != StatusCompleted {
		t.Fatalf("task status %s: %s", task.Status, task.ErrorMessage)
	}
	assertFileHash(t, task.SavePath, wantHash)
	if task.TotalBytes != int64(len(wantBytes)) {
		t.Errorf("total bytes = %d, want %d", task.TotalBytes, len(wantBytes))
	}
}

// TestPlanDownloadSegments concatenates separate segment URLs and proves a part
// whose server ignores Range still lands at the right offset (prefix skipped).
func TestPlanDownloadSegments(t *testing.T) {
	initSeg := []byte("INIT-SEGMENT-BYTES")
	segOne := bytes.Repeat([]byte("one"), 500)
	segTwo := bytes.Repeat([]byte("two"), 700)
	whole := []byte("0123456789ABCDEFGHIJ")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/init.m4s":
			_, _ = w.Write(initSeg)
		case "/seg-1.m4s":
			_, _ = w.Write(segOne)
		case "/seg-2.m4s":
			_, _ = w.Write(segTwo)
		case "/whole.bin":
			// Range ignored on purpose.
			w.Header().Set("Content-Length", strconv.Itoa(len(whole)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(whole)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	wantBytes := bytes.Join([][]byte{initSeg, segOne, segTwo, whole[10:]}, nil)
	wantHash := sha256.Sum256(wantBytes)

	plan := DownloadPlan{
		Kind:   "segments",
		Format: "mp4",
		Parts: []PlanPart{
			{URL: server.URL + "/init.m4s"},
			{URL: server.URL + "/seg-1.m4s"},
			{URL: server.URL + "/seg-2.m4s"},
			{URL: server.URL + "/whole.bin", Start: 10},
		},
	}
	task := runPlanTask(t, AddTaskRequest{
		URL:       server.URL + "/watch",
		Filename:  "stream.mp4",
		Plan:      &plan,
		AutoStart: true,
	})
	if task.Status != StatusCompleted {
		t.Fatalf("task status %s: %s", task.Status, task.ErrorMessage)
	}
	assertFileHash(t, task.SavePath, wantHash)
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func runPlanTask(t *testing.T, req AddTaskRequest) *Task {
	t.Helper()
	tempDir := t.TempDir()
	eng, err := NewEngine(Config{
		MaxConcurrentDownloads: 2,
		DefaultSegments:        4,
		MaxRetries:             0,
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	t.Cleanup(eng.Close)

	task, err := eng.AddTask(req)
	if err != nil {
		t.Fatalf("AddTask failed: %v", err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		latest, err := eng.GetTask(task.ID)
		if err != nil {
			t.Fatalf("GetTask failed: %v", err)
		}
		if latest.Status == StatusCompleted || latest.Status == StatusFailed {
			return latest
		}
		time.Sleep(25 * time.Millisecond)
	}
	latest, _ := eng.GetTask(task.ID)
	t.Fatalf("task did not finish: %+v", latest)
	return nil
}

func assertFileHash(t *testing.T, path string, want [32]byte) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	got := sha256.Sum256(data)
	if got != want {
		t.Fatalf("hash mismatch: want %s, got %s (size %d)", hex.EncodeToString(want[:]), hex.EncodeToString(got[:]), len(data))
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".plan_parts") || strings.HasSuffix(entry.Name(), ".magicpart") {
			t.Errorf("leftover temp artifact %q", entry.Name())
		}
	}
}

func serveBytesWithRanges(t *testing.T, w http.ResponseWriter, r *http.Request, payload []byte) {
	t.Helper()
	total := len(payload)
	rangeHeader := r.Header.Get("Range")
	if rangeHeader == "" {
		w.Header().Set("Content-Length", strconv.Itoa(total))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
		return
	}
	var start, end int
	spec := strings.TrimPrefix(rangeHeader, "bytes=")
	startPart, endPart, ok := strings.Cut(spec, "-")
	if !ok {
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	start, err := strconv.Atoi(strings.TrimSpace(startPart))
	if err != nil || start < 0 || start >= total {
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	end = total - 1
	if strings.TrimSpace(endPart) != "" {
		parsedEnd, err := strconv.Atoi(strings.TrimSpace(endPart))
		if err != nil {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if parsedEnd < end {
			end = parsedEnd
		}
	}
	if end < start {
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
	w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(payload[start : end+1])
}
