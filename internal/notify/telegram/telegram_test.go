package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/loafoe/mcp-notifier/internal/config"
)

func newTestNotifier(t *testing.T, handler http.HandlerFunc) (*Notifier, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	n := New(map[string]config.TelegramTarget{
		"default": {BotToken: "TEST_TOKEN", ChatID: "12345", APIBaseURL: srv.URL},
	})
	return n, srv
}

func TestSend_UnknownChat(t *testing.T) {
	n := New(map[string]config.TelegramTarget{
		"default": {BotToken: "t", ChatID: "1"},
	})

	err := n.Send(context.Background(), "nope", "hi")
	if err == nil {
		t.Fatal("Send() error = nil, want error for unknown chat")
	}
}

func TestSend_DefaultsToDefaultChat(t *testing.T) {
	var gotBody map[string]any
	n, srv := newTestNotifier(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	defer srv.Close()

	if err := n.Send(context.Background(), "", "hello **world**"); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if gotBody["chat_id"] != "12345" {
		t.Errorf("chat_id = %v, want 12345", gotBody["chat_id"])
	}
	if gotBody["parse_mode"] != "HTML" {
		t.Errorf("parse_mode = %v, want HTML", gotBody["parse_mode"])
	}
	if text, _ := gotBody["text"].(string); !strings.Contains(text, "<b>world</b>") {
		t.Errorf("text = %q, want to contain <b>world</b>", text)
	}
}

func TestSend_FallsBackToPlainTextOnAPIError(t *testing.T) {
	var calls []map[string]any
	n, srv := newTestNotifier(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		calls = append(calls, body)

		if body["parse_mode"] == "HTML" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"ok":false,"description":"Bad Request: can't parse entities"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	defer srv.Close()

	if err := n.Send(context.Background(), "default", "hello **world**"); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if len(calls) != 2 {
		t.Fatalf("expected 2 calls (HTML then plain fallback), got %d", len(calls))
	}
	if _, ok := calls[1]["parse_mode"]; ok {
		t.Errorf("fallback call should omit parse_mode, got %v", calls[1]["parse_mode"])
	}
}

func TestSend_NonOKStatusIsError(t *testing.T) {
	n, srv := newTestNotifier(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"description":"chat not found"}`))
	})
	defer srv.Close()

	err := n.Send(context.Background(), "default", "hi")
	if err == nil || !strings.Contains(err.Error(), "chat not found") {
		t.Fatalf("Send() error = %v, want to contain API description", err)
	}
}

func TestMarkdownToTelegramHTML(t *testing.T) {
	cases := map[string]string{
		"**bold**":         "<b>bold</b>",
		"*italic*":         "<i>italic</i>",
		"~~strike~~":       "<s>strike</s>",
		"[link](http://x)": `<a href="http://x">link</a>`,
		"# Header":         "Header",
		"- item":           "• item",
		"`code`":           "<code>code</code>",
		"<script>":         "&lt;script&gt;",
	}
	for in, want := range cases {
		if got := markdownToTelegramHTML(in); got != want {
			t.Errorf("markdownToTelegramHTML(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildChunks_TableBecomesPreBlock(t *testing.T) {
	content := "before\n| a | b |\n|---|---|\n| 1 | 2 |\nafter"
	chunks := buildChunks(content)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0].html, "<pre>") {
		t.Errorf("expected table rendered as <pre> block, got %q", chunks[0].html)
	}
}

func TestSplitHTML_LongMessageSplitsOnBoundary(t *testing.T) {
	long := strings.Repeat("word ", 2000)
	chunks := splitHTML(long, maxMessageLength)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for _, c := range chunks {
		if len([]rune(c)) > maxMessageLength {
			t.Errorf("chunk exceeds maxMessageLength: %d runes", len([]rune(c)))
		}
	}
}
