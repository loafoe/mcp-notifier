package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/loafoe/mcp-notifier/internal/config"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestServer_SendSlackNotification_EndToEnd(t *testing.T) {
	var received bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Slack: config.SlackConfig{
			Webhooks: map[string]config.SlackTarget{
				"default": {WebhookURL: upstream.URL},
			},
		},
	}

	server := New(cfg, discardLogger())
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)

	ctx := context.Background()
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	session, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	defer session.Close()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "send_slack_notification",
		Arguments: map[string]any{"message": "hello"},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool() returned tool error: %+v", res.Content)
	}
	if !received {
		t.Error("expected upstream webhook to receive a request")
	}
}

func TestServer_SendTelegramNotification_EndToEnd(t *testing.T) {
	var received bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Telegram: config.TelegramConfig{
			Bots: map[string]config.TelegramTarget{
				"default": {BotToken: "test-token", ChatID: "123", APIBaseURL: upstream.URL},
			},
		},
	}

	server := New(cfg, discardLogger())
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)

	ctx := context.Background()
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	session, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	defer session.Close()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "send_telegram_notification",
		Arguments: map[string]any{"message": "hello"},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool() returned tool error: %+v", res.Content)
	}
	if !received {
		t.Error("expected upstream Bot API to receive a request")
	}
}

func TestServer_ListChannels(t *testing.T) {
	cfg := &config.Config{
		Slack: config.SlackConfig{
			Webhooks: map[string]config.SlackTarget{"default": {WebhookURL: "https://example.com"}},
		},
		Teams: config.TeamsConfig{
			Webhooks: map[string]config.TeamsTarget{"default": {WebhookURL: "https://example.com"}},
		},
		Telegram: config.TelegramConfig{
			Bots: map[string]config.TelegramTarget{"default": {BotToken: "t", ChatID: "1"}},
		},
	}

	server := New(cfg, discardLogger())
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)

	ctx := context.Background()
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	session, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	defer session.Close()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_channels"})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool() returned tool error: %+v", res.Content)
	}

	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal StructuredContent: %v", err)
	}
	var out listChannelsOutput
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("failed to decode list_channels output: %v", err)
	}
	if len(out.Providers) != 3 {
		t.Fatalf("expected 3 providers, got %d: %+v", len(out.Providers), out.Providers)
	}
}

func TestServer_OnlySlackConfigured_NoTeamsTool(t *testing.T) {
	cfg := &config.Config{
		Slack: config.SlackConfig{
			Webhooks: map[string]config.SlackTarget{"default": {WebhookURL: "https://example.com"}},
		},
	}
	server := New(cfg, discardLogger())
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)

	ctx := context.Background()
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	session, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	defer session.Close()

	var names []string
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("Tools() error = %v", err)
		}
		names = append(names, tool.Name)
	}

	for _, n := range names {
		if n == "send_teams_notification" {
			t.Errorf("expected no send_teams_notification tool when teams is unconfigured, got tools: %v", names)
		}
		if n == "send_telegram_notification" {
			t.Errorf("expected no send_telegram_notification tool when telegram is unconfigured, got tools: %v", names)
		}
	}
}
