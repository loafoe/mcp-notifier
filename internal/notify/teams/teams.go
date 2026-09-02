// Package teams sends messages to Microsoft Teams via Power Automate
// workflow webhooks, using Adaptive Cards for rich formatting. Adapted from
// picoclaw's teams_webhook channel.
package teams

import (
	"context"
	"fmt"
	"strings"

	goteamsnotify "github.com/atc0005/go-teams-notify/v2"
	"github.com/atc0005/go-teams-notify/v2/adaptivecard"

	"github.com/loafoe/mcp-notifier/internal/config"
)

// teamsMessageSender abstracts the Teams client for testability.
type teamsMessageSender interface {
	SendWithContext(ctx context.Context, webhookURL string, message goteamsnotify.TeamsMessage) error
}

// Notifier sends messages to one of several configured Teams webhook targets.
type Notifier struct {
	targets map[string]config.TeamsTarget
	client  teamsMessageSender
}

// New creates a Notifier from the configured Teams targets.
func New(targets map[string]config.TeamsTarget) *Notifier {
	return &Notifier{
		targets: targets,
		client:  goteamsnotify.NewTeamsClient(),
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
		return fmt.Errorf("teams: unknown channel %q", targetName)
	}

	card, err := buildAdaptiveCard(target, message)
	if err != nil {
		return fmt.Errorf("teams: failed to build card: %w", err)
	}

	teamsMsg, err := adaptivecard.NewMessageFromCard(card)
	if err != nil {
		return fmt.Errorf("teams: failed to create message: %w", err)
	}

	if err := n.client.SendWithContext(ctx, target.WebhookURL, teamsMsg); err != nil {
		// Don't expose the raw error - it may contain the webhook URL.
		return fmt.Errorf("teams: send to %q failed: %w", targetName, err)
	}

	return nil
}

// buildAdaptiveCard creates a formatted Adaptive Card from the message. It
// detects markdown tables and converts them to native Adaptive Card Table
// elements, since TextBlocks only support a limited markdown subset.
func buildAdaptiveCard(target config.TeamsTarget, message string) (adaptivecard.Card, error) {
	card := adaptivecard.NewCard()
	card.Type = adaptivecard.TypeAdaptiveCard
	card.MSTeams.Width = "Full"

	title := target.Title
	if title == "" {
		title = "Notification"
	}

	titleBlock := adaptivecard.NewTextBlock(title, true)
	titleBlock.Size = adaptivecard.SizeLarge
	titleBlock.Weight = adaptivecard.WeightBolder
	titleBlock.Style = adaptivecard.TextBlockStyleHeading

	if err := card.AddElement(false, titleBlock); err != nil {
		return card, err
	}

	content := message
	if content == "" {
		content = "(empty message)"
	}

	segments := splitContentWithTables(content)

	for _, seg := range segments {
		if seg.isTable {
			tableElement, err := parseMarkdownTable(seg.content)
			if err != nil {
				block := adaptivecard.NewTextBlock("```\n"+seg.content+"\n```", true)
				block.Wrap = true
				if err := card.AddElement(false, block); err != nil {
					return card, err
				}
				continue
			}
			if err := card.AddElement(false, tableElement); err != nil {
				return card, err
			}
		} else {
			text := strings.TrimSpace(seg.content)
			if text == "" {
				continue
			}
			block := adaptivecard.NewTextBlock(text, true)
			block.Wrap = true
			if err := card.AddElement(false, block); err != nil {
				return card, err
			}
		}
	}

	return card, nil
}
