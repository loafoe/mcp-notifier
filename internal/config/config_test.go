package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_ExpandsEnvAndValidates(t *testing.T) {
	t.Setenv("TEST_SLACK_URL", "https://hooks.slack.com/services/T000/B000/XXXX")

	path := writeConfig(t, `
slack:
  webhooks:
    default:
      webhook_url: "${TEST_SLACK_URL}"
      username: "bot"
teams:
  webhooks:
    default:
      webhook_url: "https://example.webhook.office.com/webhookb2/xxx"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got := cfg.Slack.Webhooks["default"].WebhookURL; got != "https://hooks.slack.com/services/T000/B000/XXXX" {
		t.Errorf("slack webhook_url = %q, want env expanded", got)
	}
	if got := cfg.Teams.Webhooks["default"].Title; got != "" {
		t.Errorf("teams title = %q, want empty", got)
	}
}

func TestLoad_MissingDefaultTarget(t *testing.T) {
	path := writeConfig(t, `
slack:
  webhooks:
    other:
      webhook_url: "https://hooks.slack.com/services/T000/B000/XXXX"
`)

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want error for missing default target")
	}
}

func TestLoad_RejectsNonHTTPS(t *testing.T) {
	path := writeConfig(t, `
slack:
  webhooks:
    default:
      webhook_url: "http://hooks.slack.com/services/T000/B000/XXXX"
`)

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want error for non-HTTPS webhook_url")
	}
}

func TestLoad_EmptyConfigIsValid(t *testing.T) {
	path := writeConfig(t, `{}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Slack.Webhooks) != 0 || len(cfg.Teams.Webhooks) != 0 {
		t.Errorf("expected no webhooks configured, got %+v", cfg)
	}
}
