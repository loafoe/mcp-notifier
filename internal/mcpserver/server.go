// Package mcpserver wires the Slack and Teams notifiers into an MCP server
// exposing notification tools.
package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/loafoe/mcp-notifier/internal/config"
	"github.com/loafoe/mcp-notifier/internal/notify/slack"
	"github.com/loafoe/mcp-notifier/internal/notify/teams"
	"github.com/loafoe/mcp-notifier/internal/notify/telegram"
)

// Version is set at build time via -ldflags.
var Version = "dev"

type sender interface {
	Send(ctx context.Context, targetName, message string) error
	Targets() []string
}

// New builds an MCP server with tools for every configured notification
// provider. Providers with no configured webhook targets are skipped, so a
// deployment can enable Slack only, Teams only, or both.
func New(cfg *config.Config, logger *slog.Logger) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "mcp-notifier",
		Version: Version,
	}, nil)

	var slackNotifier *slack.Notifier
	if len(cfg.Slack.Webhooks) > 0 {
		slackNotifier = slack.New(cfg.Slack.Webhooks)
		registerSendTool(server, "send_slack_notification",
			"Send a message to a Slack channel via an Incoming Webhook. Supports Markdown (bold, italics, links, lists, tables, code blocks), which is converted to Slack's mrkdwn format.",
			slackNotifier, logger)
	}

	var teamsNotifier *teams.Notifier
	if len(cfg.Teams.Webhooks) > 0 {
		teamsNotifier = teams.New(cfg.Teams.Webhooks)
		registerSendTool(server, "send_teams_notification",
			"Send a message to a Microsoft Teams channel via a Power Automate workflow webhook, rendered as an Adaptive Card. Supports Markdown text and tables.",
			teamsNotifier, logger)
	}

	var telegramNotifier *telegram.Notifier
	if len(cfg.Telegram.Bots) > 0 {
		telegramNotifier = telegram.New(cfg.Telegram.Bots)
		registerSendTool(server, "send_telegram_notification",
			"Send a message to a Telegram chat via a Bot API bot account. Supports Markdown (bold, italics, strikethrough, links, lists, tables, code blocks), which is converted to Telegram's HTML message format.",
			telegramNotifier, logger)
	}

	registerListChannelsTool(server, slackNotifier, teamsNotifier, telegramNotifier)

	return server
}

// sendNotificationInput is the shared input schema for send_* tools.
type sendNotificationInput struct {
	Channel string `json:"channel,omitempty" jsonschema:"Name of the configured webhook target to send to. Defaults to 'default' if omitted."`
	Message string `json:"message" jsonschema:"The message body to send. Markdown is supported."`
}

type sendNotificationOutput struct {
	Sent    bool   `json:"sent"`
	Channel string `json:"channel"`
}

func registerSendTool(server *mcp.Server, name, description string, n sender, logger *slog.Logger) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        name,
		Description: description,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in sendNotificationInput) (*mcp.CallToolResult, sendNotificationOutput, error) {
		channel := in.Channel
		if channel == "" {
			channel = "default"
		}

		if err := n.Send(ctx, channel, in.Message); err != nil {
			logger.ErrorContext(ctx, "notification send failed", "tool", name, "channel", channel, "error", err)
			return nil, sendNotificationOutput{}, fmt.Errorf("%s: %w", name, err)
		}

		logger.InfoContext(ctx, "notification sent", "tool", name, "channel", channel)
		return nil, sendNotificationOutput{Sent: true, Channel: channel}, nil
	})
}

type listChannelsInput struct{}

type providerChannels struct {
	Provider string   `json:"provider"`
	Channels []string `json:"channels"`
}

type listChannelsOutput struct {
	Providers []providerChannels `json:"providers"`
}

func registerListChannelsTool(server *mcp.Server, slackNotifier *slack.Notifier, teamsNotifier *teams.Notifier, telegramNotifier *telegram.Notifier) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_channels",
		Description: "List the configured notification providers and their named webhook channels.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ listChannelsInput) (*mcp.CallToolResult, listChannelsOutput, error) {
		out := listChannelsOutput{}

		if slackNotifier != nil {
			targets := slackNotifier.Targets()
			sort.Strings(targets)
			out.Providers = append(out.Providers, providerChannels{Provider: "slack", Channels: targets})
		}
		if teamsNotifier != nil {
			targets := teamsNotifier.Targets()
			sort.Strings(targets)
			out.Providers = append(out.Providers, providerChannels{Provider: "teams", Channels: targets})
		}
		if telegramNotifier != nil {
			targets := telegramNotifier.Targets()
			sort.Strings(targets)
			out.Providers = append(out.Providers, providerChannels{Provider: "telegram", Channels: targets})
		}

		return nil, out, nil
	})
}
