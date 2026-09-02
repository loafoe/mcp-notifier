package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/loafoe/mcp-notifier/internal/config"
)

func TestSend_UnknownChannel(t *testing.T) {
	n := New(map[string]config.SlackTarget{
		"default": {WebhookURL: "https://example.com/webhook"},
	})

	err := n.Send(context.Background(), "nope", "hi")
	if err == nil {
		t.Fatal("Send() error = nil, want error for unknown channel")
	}
}

func TestSend_DefaultsToDefaultChannel(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf [4096]byte
		n, _ := r.Body.Read(buf[:])
		gotBody = buf[:n]
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := New(map[string]config.SlackTarget{
		"default": {WebhookURL: srv.URL, Username: "bot", IconEmoji: ":robot:"},
	})

	if err := n.Send(context.Background(), "", "hello **world**"); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["username"] != "bot" {
		t.Errorf("username = %v, want bot", payload["username"])
	}
}

func TestSend_NonOKStatusIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("invalid_payload"))
	}))
	defer srv.Close()

	n := New(map[string]config.SlackTarget{"default": {WebhookURL: srv.URL}})

	err := n.Send(context.Background(), "default", "hi")
	if err == nil || !strings.Contains(err.Error(), "invalid_payload") {
		t.Fatalf("Send() error = %v, want to contain response body", err)
	}
}

func TestConvertMarkdownToMrkdwn(t *testing.T) {
	cases := map[string]string{
		"**bold**":         "*bold*",
		"*italic*":         "_italic_",
		"~~strike~~":       "~strike~",
		"[link](http://x)": "<http://x|link>",
		"# Header":         "*Header*",
		"- item":           "• item",
	}
	for in, want := range cases {
		if got := convertMarkdownToMrkdwn(in); got != want {
			t.Errorf("convertMarkdownToMrkdwn(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildBlocks_TableBecomesFormattedText(t *testing.T) {
	content := "before\n| a | b |\n|---|---|\n| 1 | 2 |\nafter"
	blocks := buildBlocks(content)
	if len(blocks) < 3 {
		t.Fatalf("expected at least 3 blocks (text, table, text), got %d: %+v", len(blocks), blocks)
	}
}

func TestSplitText_LongMessageSplitsOnBoundary(t *testing.T) {
	long := strings.Repeat("word ", 1000)
	chunks := splitText(long, maxTextBlockLength)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for _, c := range chunks {
		if len([]rune(c)) > maxTextBlockLength {
			t.Errorf("chunk exceeds maxTextBlockLength: %d runes", len([]rune(c)))
		}
	}
}
