// Package config loads mcp-notifier's YAML configuration: a set of named
// webhook targets per notification provider (Slack, Microsoft Teams,
// Telegram).
package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the top-level mcp-notifier configuration.
type Config struct {
	Slack    SlackConfig    `yaml:"slack"`
	Teams    TeamsConfig    `yaml:"teams"`
	Telegram TelegramConfig `yaml:"telegram"`
}

// SlackConfig configures the Slack Incoming Webhook provider.
type SlackConfig struct {
	Webhooks map[string]SlackTarget `yaml:"webhooks"`
}

// SlackTarget is a single named Slack Incoming Webhook destination.
type SlackTarget struct {
	WebhookURL string `yaml:"webhook_url"`
	Username   string `yaml:"username"`
	IconEmoji  string `yaml:"icon_emoji"`
}

// TeamsConfig configures the Microsoft Teams (Power Automate) webhook provider.
type TeamsConfig struct {
	Webhooks map[string]TeamsTarget `yaml:"webhooks"`
}

// TeamsTarget is a single named Teams webhook destination.
type TeamsTarget struct {
	WebhookURL string `yaml:"webhook_url"`
	Title      string `yaml:"title"`
}

// TelegramConfig configures the Telegram Bot API provider.
type TelegramConfig struct {
	Bots map[string]TelegramTarget `yaml:"bots"`
}

// TelegramTarget is a single named Telegram bot/chat destination. BotToken
// authenticates as a Telegram bot account (created via @BotFather); ChatID
// is the numeric chat/channel ID or "@channelusername" the bot has been
// added to. APIBaseURL optionally points at a self-hosted Telegram Bot API
// server instead of api.telegram.org.
type TelegramTarget struct {
	BotToken   string `yaml:"bot_token"`
	ChatID     string `yaml:"chat_id"`
	APIBaseURL string `yaml:"api_base_url,omitempty"`
}

// Load reads and parses the config file at path. Values may reference
// environment variables using ${VAR} or $VAR syntax (expanded via
// os.Expand), so webhook URLs can be injected as Kubernetes Secret-backed
// env vars while the rest of the config lives in a plain ConfigMap.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: reading %s: %w", path, err)
	}

	expanded := expandEnv(string(raw))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("config: parsing %s: %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// expandEnv expands ${VAR} references, leaving unset variables as empty
// strings but never expanding bare $VAR (avoids surprises from webhook URL
// query strings or other YAML content that happens to contain a literal '$').
func expandEnv(s string) string {
	return os.Expand(s, func(key string) string {
		return os.Getenv(key)
	})
}

// Validate checks that every configured webhook target has a valid,
// non-empty HTTPS URL, and that each provider (if configured at all) has a
// "default" target.
func (c *Config) Validate() error {
	for name, target := range c.Slack.Webhooks {
		if err := validateWebhookURL(target.WebhookURL); err != nil {
			return fmt.Errorf("config: slack webhook %q: %w", name, err)
		}
	}
	if len(c.Slack.Webhooks) > 0 {
		if _, ok := c.Slack.Webhooks["default"]; !ok {
			return fmt.Errorf("config: slack: a %q webhook target is required", "default")
		}
	}

	for name, target := range c.Teams.Webhooks {
		if err := validateWebhookURL(target.WebhookURL); err != nil {
			return fmt.Errorf("config: teams webhook %q: %w", name, err)
		}
	}
	if len(c.Teams.Webhooks) > 0 {
		if _, ok := c.Teams.Webhooks["default"]; !ok {
			return fmt.Errorf("config: teams: a %q webhook target is required", "default")
		}
	}

	for name, target := range c.Telegram.Bots {
		if strings.TrimSpace(target.BotToken) == "" {
			return fmt.Errorf("config: telegram bot %q: empty bot_token", name)
		}
		if strings.TrimSpace(target.ChatID) == "" {
			return fmt.Errorf("config: telegram bot %q: empty chat_id", name)
		}
	}
	if len(c.Telegram.Bots) > 0 {
		if _, ok := c.Telegram.Bots["default"]; !ok {
			return fmt.Errorf("config: telegram: a %q bot target is required", "default")
		}
	}

	return nil
}

func validateWebhookURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("empty webhook_url")
	}
	if !strings.HasPrefix(strings.ToLower(raw), "https://") {
		return fmt.Errorf("webhook_url must use HTTPS")
	}
	return nil
}
