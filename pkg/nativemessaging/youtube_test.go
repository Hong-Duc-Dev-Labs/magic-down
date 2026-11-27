package nativemessaging

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"magicdown/pkg/engine"
	"magicdown/pkg/ipc"
)

func TestHandleMessage_ResolveYouTubeRejectsNonYouTubeURL(t *testing.T) {
	handler := NewHandler(ipc.NewClient(28999))
	req, _ := json.Marshal(InboundMessage{
		Action: "resolve_youtube",
		URL:    "https://example.com/video.mp4",
	})
	rawResp, err := handler.HandleMessage(req)
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	var resp OutboundMessage
	if err := json.Unmarshal(rawResp, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Success {
		t.Fatalf("expected failure for non-YouTube URL, got success")
	}
	if !strings.Contains(resp.Error, "unsupported YouTube host") {
		t.Errorf("unexpected error message: %q", resp.Error)
	}
}

func TestHandleMessage_ResolveYouTubeForwardsToIPC(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			_ = json.NewEncoder(w).Encode(ipc.StatusResponse{
				Status: "ok",
				App:    "MagicDown",
			})
		case "/youtube/resolve":
			_ = json.NewEncoder(w).Encode(ipc.ResolveYouTubeResponse{
				Success: true,
				YouTube: &engine.YouTubeResolveResult{
					VideoID: "dQw4w9WgXcQ",
					URL:     "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
					Title:   "Never Gonna Give You Up",
					Variants: []engine.YouTubeStreamOption{
						{
							URL:         "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
							Quality:     "1080p",
							Height:      1080,
							Format:      "MP4",
							Ext:         "mp4",
							YTDLPFormat: "bestvideo[height<=1080]+bestaudio/best",
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	parts := strings.Split(srv.URL, ":")
	port, _ := strconv.Atoi(parts[len(parts)-1])
	client := ipc.NewClient(port)
	handler := NewHandler(client)

	req, _ := json.Marshal(InboundMessage{
		Action: "resolve_youtube",
		URL:    "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
	})
	rawResp, err := handler.HandleMessage(req)
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	var resp OutboundMessage
	if err := json.Unmarshal(rawResp, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.Success || resp.YouTube == nil || resp.YouTube.VideoID != "dQw4w9WgXcQ" || len(resp.YouTube.Variants) != 1 {
		t.Fatalf("unexpected resolve_youtube response: %+v", resp)
	}
}
