# mcp-notifier

A standalone [MCP](https://modelcontextprotocol.io) server that sends
notifications to Slack (Incoming Webhooks), Microsoft Teams (Power Automate
workflow webhooks), and Telegram (Bot API). Extracted from
[picoclaw](https://github.com/sipeed/picoclaw)'s `slack_webhook`,
`teams_webhook`, and `telegram` channels so any MCP client can use the same
notification capability.

Built with the official
[modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk).

## Tools

- `send_slack_notification(channel?, message)` — sends `message` (Markdown,
  converted to Slack mrkdwn: bold/italic/strikethrough/links/headers/lists,
  with table support) to the named Slack webhook target.
- `send_teams_notification(channel?, message)` — sends `message` as an
  Adaptive Card to the named Teams webhook target. Markdown tables are
  rendered as native Adaptive Card tables.
- `send_telegram_notification(channel?, message)` — sends `message` (Markdown,
  converted to Telegram's HTML message format: bold/italic/strikethrough/
  links/lists/code blocks, with tables rendered as monospace blocks) to the
  named Telegram bot/chat target.
- `list_channels()` — lists configured providers and their named webhook
  targets.

`channel` defaults to `"default"`. Tools are only registered for providers
that have at least one webhook configured, so a deployment can run
Slack-only, Teams-only, or both.

## Configuration

See [`config.example.yaml`](./config.example.yaml). Multiple named webhook
targets ("channels") are supported per provider, mirroring picoclaw's
`slack_webhook`/`teams_webhook` config shape:

```yaml
slack:
  webhooks:
    default:
      webhook_url: "${SLACK_DEFAULT_WEBHOOK_URL}"
      username: "mcp-notifier"
      icon_emoji: ":bell:"
    alerts:
      webhook_url: "${SLACK_ALERTS_WEBHOOK_URL}"

teams:
  webhooks:
    default:
      webhook_url: "${TEAMS_DEFAULT_WEBHOOK_URL}"
      title: "Notification"

telegram:
  bots:
    default:
      bot_token: "${TELEGRAM_DEFAULT_BOT_TOKEN}"
      chat_id: "${TELEGRAM_DEFAULT_CHAT_ID}"
```

- Every provider section that's present requires a `default` target.
- Values may reference environment variables via `${VAR_NAME}` (expanded at
  load time), so webhook URLs can be injected from a Kubernetes Secret while
  the rest of the config lives in a plain ConfigMap.
- `webhook_url` must be `https://`.

### Telegram bot setup

Create a bot account via [@BotFather](https://t.me/BotFather) (`/newbot`) to
get `bot_token`. `chat_id` is the numeric chat/group/channel ID the bot has
been added to (or `@channelusername` for public channels) — the simplest way
to find a chat's numeric ID is to send it a message and check
`https://api.telegram.org/bot<token>/getUpdates`. An optional
`api_base_url` per target points at a self-hosted
[Telegram Bot API server](https://github.com/tdlib/telegram-bot-api) instead
of `api.telegram.org`.

## Running

```sh
go build -o mcp-notifier ./cmd/mcp-notifier
./mcp-notifier --config config.yaml --transport http --addr :8080
```

Flags (all overridable via env var):

| Flag         | Env var                 | Default                       |
|--------------|--------------------------|--------------------------------|
| `--config`   | `MCP_NOTIFIER_CONFIG`   | `/etc/mcp-notifier/config.yaml` |
| `--transport`| `MCP_NOTIFIER_TRANSPORT`| `http`                          |
| `--addr`     | `MCP_NOTIFIER_ADDR`     | `:8080`                         |
| `--path`     | `MCP_NOTIFIER_PATH`     | `/mcp`                          |

`--transport stdio` runs the server over stdio for local MCP client testing
(e.g. Claude Desktop, `mcp-inspector`). The HTTP transport serves stateless
MCP (Streamable HTTP with `Stateless: true`) at `--path` and a `/health`
endpoint for Kubernetes probes.

## Container image

Images are built with [`ko`](https://ko.build) (no Dockerfile needed) and
published to `ghcr.io/loafoe/mcp-notifier`, signed keylessly with
[cosign](https://docs.sigstore.dev/cosign/overview/) via GitHub Actions OIDC.

Verify a signature:

```sh
cosign verify ghcr.io/loafoe/mcp-notifier:latest \
  --certificate-identity-regexp 'https://github.com/loafoe/mcp-notifier/.github/workflows/release.yml@.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

Build locally:

```sh
KO_DOCKER_REPO=ko.local ko build --bare --local ./cmd/mcp-notifier
```

## Helm chart

Deployed via the `mcp-notifier` chart in
[loafoe/helm-charts](https://github.com/loafoe/helm-charts/tree/main/charts/mcp-notifier).

## Development

```sh
go vet ./...
go test ./... -race
```
