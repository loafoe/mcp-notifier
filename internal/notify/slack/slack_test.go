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

func TestBuildBlocks_TableBecomesNativeTableBlock(t *testing.T) {
	content := "before\n| a | b |\n|---|---|\n| 1 | 2 |\nafter"
	blocks := buildBlocks(content)
	if len(blocks) < 3 {
		t.Fatalf("expected at least 3 blocks (text, table, text), got %d: %+v", len(blocks), blocks)
	}
	table, ok := blocks[1]["type"].(string)
	if !ok || table != "table" {
		t.Fatalf("expected middle block to be a native table block, got %+v", blocks[1])
	}
}

func TestBuildTableBlock_ValidTable(t *testing.T) {
	block, ok := buildTableBlock("| Name | Status |\n|---|---|\n| svc-a | OK |\n| svc-b | Degraded |")
	if !ok {
		t.Fatal("buildTableBlock() ok = false, want true")
	}
	if block["type"] != "table" {
		t.Errorf("type = %v, want table", block["type"])
	}
	rows, ok := block["rows"].([][]map[string]any)
	if !ok || len(rows) != 3 {
		t.Fatalf("expected 3 rows (header + 2 body), got %+v", block["rows"])
	}
	header := rows[0][0]
	if header["type"] != "rich_text" {
		t.Errorf("header cell type = %v, want rich_text", header["type"])
	}
	body := rows[1][0]
	if body["type"] != "raw_text" || body["text"] != "svc-a" {
		t.Errorf("body cell = %+v, want raw_text svc-a", body)
	}
}

func TestBuildTableBlock_TooManyRowsFallsBack(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("| a |\n|---|\n")
	for i := 0; i < maxTableRows+1; i++ {
		sb.WriteString("| x |\n")
	}
	if _, ok := buildTableBlock(sb.String()); ok {
		t.Fatal("buildTableBlock() ok = true, want false for table exceeding row limit")
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
