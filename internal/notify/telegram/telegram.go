// Package telegram sends messages to Telegram via the Bot API's sendMessage
// method, using HTML formatting. Bot accounts are created via @BotFather;
// adapted from picoclaw's telegram channel markdown conversion.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/loafoe/mcp-notifier/internal/config"
)

const defaultAPIBaseURL = "https://api.telegram.org"

// Notifier sends messages to one of several configured Telegram bot/chat targets.
type Notifier struct {
	targets map[string]config.TelegramTarget
	client  *http.Client
}

// New creates a Notifier from the configured Telegram targets.
func New(targets map[string]config.TelegramTarget) *Notifier {
	return &Notifier{
		targets: targets,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// Targets returns the configured target names.
func (n *Notifier) Targets() []string {
	names := make([]string, 0, len(n.targets))
	for name := range n.targets {
		names = append(names, name)
	}
	return names
}

// Send delivers a message to the named bot/chat target ("" means "default").
// Messages are converted from Markdown to Telegram HTML and split into
// multiple sendMessage calls if they exceed Telegram's 4096-character limit.
func (n *Notifier) Send(ctx context.Context, targetName, message string) error {
	if targetName == "" {
		targetName = "default"
	}

	target, ok := n.targets[targetName]
	if !ok {
		return fmt.Errorf("telegram: unknown chat %q", targetName)
	}

	content := message
	if content == "" {
		content = "(empty message)"
	}

	for _, chunk := range buildChunks(content) {
		if err := n.sendChunk(ctx, targetName, target, chunk); err != nil {
			return err
		}
	}

	return nil
}

func (n *Notifier) sendChunk(ctx context.Context, targetName string, target config.TelegramTarget, chunk messageChunk) error {
	if err := n.post(ctx, target, chunk.html, "HTML"); err == nil {
		return nil
	}

	// Fall back to unformatted plain text: the HTML conversion is regex-based
	// and can occasionally produce entities Telegram rejects (e.g. unbalanced
	// tags from unusual input), so retry once without parse_mode.
	if err := n.post(ctx, target, chunk.plain, ""); err != nil {
		return fmt.Errorf("telegram: send to %q failed: %w", targetName, err)
	}

	return nil
}

func (n *Notifier) post(ctx context.Context, target config.TelegramTarget, text, parseMode string) error {
	payload := map[string]any{
		"chat_id": target.ChatID,
		"text":    text,
	}
	if parseMode != "" {
		payload["parse_mode"] = parseMode
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	baseURL := target.APIBaseURL
	if baseURL == "" {
		baseURL = defaultAPIBaseURL
	}
	url := fmt.Sprintf("%s/bot%s/sendMessage", strings.TrimRight(baseURL, "/"), target.BotToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		// Don't expose the raw error - it may contain the bot token via the URL.
		return fmt.Errorf("network error")
	}
	defer resp.Body.Close()

	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)

	if resp.StatusCode >= 400 || !result.OK {
		desc := strings.TrimSpace(result.Description)
		if desc == "" {
			desc = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("status %d: %s", resp.StatusCode, desc)
	}

	return nil
}
