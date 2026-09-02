package teams

import (
	"context"
	"errors"
	"testing"

	goteamsnotify "github.com/atc0005/go-teams-notify/v2"

	"github.com/loafoe/mcp-notifier/internal/config"
)

type fakeSender struct {
	lastURL string
	err     error
}

func (f *fakeSender) SendWithContext(_ context.Context, webhookURL string, _ goteamsnotify.TeamsMessage) error {
	f.lastURL = webhookURL
	return f.err
}

func TestSend_UnknownChannel(t *testing.T) {
	n := &Notifier{targets: map[string]config.TeamsTarget{
		"default": {WebhookURL: "https://example.webhook.office.com/x"},
	}, client: &fakeSender{}}

	if err := n.Send(context.Background(), "nope", "hi"); err == nil {
		t.Fatal("Send() error = nil, want error for unknown channel")
	}
}

func TestSend_DefaultsToDefaultChannelAndPropagatesURL(t *testing.T) {
	fake := &fakeSender{}
	n := &Notifier{targets: map[string]config.TeamsTarget{
		"default": {WebhookURL: "https://example.webhook.office.com/x", Title: "Alerts"},
	}, client: fake}

	if err := n.Send(context.Background(), "", "hello world"); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if fake.lastURL != "https://example.webhook.office.com/x" {
		t.Errorf("lastURL = %q, want default target URL", fake.lastURL)
	}
}

func TestSend_SendErrorIsWrapped(t *testing.T) {
	fake := &fakeSender{err: errors.New("401 Unauthorized")}
	n := &Notifier{targets: map[string]config.TeamsTarget{
		"default": {WebhookURL: "https://example.webhook.office.com/x"},
	}, client: fake}

	err := n.Send(context.Background(), "default", "hi")
	if err == nil {
		t.Fatal("Send() error = nil, want wrapped send error")
	}
}

func TestSplitContentWithTables(t *testing.T) {
	content := "before\n| a | b |\n|---|---|\n| 1 | 2 |\nafter"
	segs := splitContentWithTables(content)

	var tableCount int
	for _, s := range segs {
		if s.isTable {
			tableCount++
		}
	}
	if tableCount != 1 {
		t.Errorf("expected exactly one table segment, got %d in %+v", tableCount, segs)
	}
}

func TestParseMarkdownTable(t *testing.T) {
	table := "| a | b |\n|---|---|\n| 1 | 2 |"
	elem, err := parseMarkdownTable(table)
	if err != nil {
		t.Fatalf("parseMarkdownTable() error = %v", err)
	}
	if elem.Type != "Table" {
		t.Errorf("elem.Type = %q, want Table", elem.Type)
	}
}
