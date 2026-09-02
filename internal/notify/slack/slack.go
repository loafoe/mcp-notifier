// Package slack sends messages to Slack via Incoming Webhooks using Block
// Kit formatting. Adapted from picoclaw's slack_webhook channel.
package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/loafoe/mcp-notifier/internal/config"
)

const maxTextBlockLength = 3000

// Notifier sends messages to one of several configured Slack webhook targets.
type Notifier struct {
	targets map[string]config.SlackTarget
	client  *http.Client
}

// New creates a Notifier from the configured Slack targets.
func New(targets map[string]config.SlackTarget) *Notifier {
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

// Send delivers a message to the named webhook target ("" means "default").
func (n *Notifier) Send(ctx context.Context, targetName, message string) error {
	if targetName == "" {
		targetName = "default"
	}

	target, ok := n.targets[targetName]
	if !ok {
		return fmt.Errorf("slack: unknown channel %q", targetName)
	}

	payload := buildPayload(target, message)

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("slack: failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.WebhookURL, bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("slack: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		// Don't expose the raw error - it may contain the webhook URL.
		return fmt.Errorf("slack: network error sending to %q", targetName)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		respText := strings.TrimSpace(string(respBody))
		if respText == "" {
			respText = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("slack: %s: status %d: %s", targetName, resp.StatusCode, respText)
	}

	return nil
}

func buildPayload(target config.SlackTarget, message string) map[string]any {
	payload := make(map[string]any)

	if target.Username != "" {
		payload["username"] = target.Username
	}
	if target.IconEmoji != "" {
		payload["icon_emoji"] = target.IconEmoji
	}

	content := message
	if content == "" {
		content = "(empty message)"
	}

	payload["blocks"] = buildBlocks(content)

	return payload
}

func buildBlocks(content string) []map[string]any {
	var blocks []map[string]any

	segments := splitContentWithTables(content)

	for _, seg := range segments {
		if seg.isTable {
			tableText := renderTable(seg.content)
			for _, chunk := range splitText(tableText, maxTextBlockLength) {
				blocks = append(blocks, textSection(chunk))
			}
		} else {
			text := strings.TrimSpace(seg.content)
			if text == "" {
				continue
			}
			converted := convertMarkdownToMrkdwn(text)
			for _, chunk := range splitText(converted, maxTextBlockLength) {
				blocks = append(blocks, textSection(chunk))
			}
		}
	}

	if len(blocks) == 0 {
		blocks = append(blocks, textSection("(empty message)"))
	}

	return blocks
}

func textSection(text string) map[string]any {
	return map[string]any{
		"type": "section",
		"text": map[string]any{
			"type": "mrkdwn",
			"text": text,
		},
	}
}

// splitText breaks text into chunks no longer than maxLen runes, preferring
// to split on newlines or spaces so words aren't cut mid-way, and re-wrapping
// any code fence that straddles a chunk boundary so each chunk stays valid
// mrkdwn.
func splitText(text string, maxLen int) []string {
	runes := []rune(text)
	if len(runes) <= maxLen {
		return []string{text}
	}

	const fencePrefix = "```\n"
	const fenceSuffix = "\n```"
	fencePrefixLen := len([]rune(fencePrefix))
	fenceSuffixLen := len([]rune(fenceSuffix))

	var chunks []string
	inFence := false

	for len(runes) > 0 {
		prefixLen := 0
		if inFence {
			prefixLen = fencePrefixLen
		}
		contentBudget := maxLen - prefixLen - fenceSuffixLen
		if contentBudget <= 0 {
			contentBudget = maxLen
		}

		splitAt := len(runes)
		if splitAt > contentBudget {
			splitAt = findSplitPoint(runes, contentBudget)
			if splitAt <= 0 || splitAt > contentBudget {
				splitAt = contentBudget
			}
		}

		chunkBody := string(runes[:splitAt])
		chunkEndsInFence := endsInsideFence(chunkBody, inFence)
		chunk := wrapFenceChunk(chunkBody, inFence, chunkEndsInFence)

		chunks = append(chunks, chunk)
		inFence = chunkEndsInFence
		runes = runes[splitAt:]
	}

	return chunks
}

func wrapFenceChunk(text string, wasInFence bool, endsInFence bool) string {
	if wasInFence && !strings.HasPrefix(strings.TrimSpace(text), "```") {
		text = "```\n" + text
	}
	if endsInFence {
		text = strings.TrimSuffix(text, "\n") + "\n```"
	}
	return text
}

func findSplitPoint(runes []rune, maxLen int) int {
	if len(runes) <= maxLen {
		return len(runes)
	}
	window := string(runes[:maxLen])

	if idx := strings.LastIndex(window, "\n"); idx > 0 {
		return len([]rune(window[:idx])) + 1
	}
	if idx := strings.LastIndex(window, " "); idx > 0 {
		return len([]rune(window[:idx])) + 1
	}
	if idx := strings.LastIndex(window, "```"); idx > 0 {
		return len([]rune(window[:idx]))
	}
	return maxLen
}

func endsInsideFence(text string, wasInFence bool) bool {
	return wasInFence != (strings.Count(text, "```")%2 == 1)
}
