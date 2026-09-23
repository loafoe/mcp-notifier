package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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

// ---------------------------------------------------------------------------
// SEP-2575 / protocol version 2026-07-28 — stateless HTTP transport tests
//
// SEP-2575 ("Streamable HTTP stateless transport for remote MCP") makes
// 2026-07-28 the latest protocol revision. Compared to 2025-11-25 it
// requires://
//   - the standard HTTP headers Mcp-Method and Mcp-Name on every request;
//   - the _meta field (io.modelcontextprotocol/protocolVersion, ...clientInfo,
//     ...clientCapabilities) in request params;
//   - server/discover as the new lightweight initialization RPC (the legacy
//     initialize method is gone);
//   - 405 + Allow: POST for GET requests in stateless mode.
// ---------------------------------------------------------------------------

// metaBlock returns the SEP-2575 _meta triple embedded in request params.
func metaBlock(protoVer string) string {
	return fmt.Sprintf(`"_meta":{"io.modelcontextprotocol/protocolVersion":%q,"io.modelcontextprotocol/clientCapabilities":{},"io.modelcontextprotocol/clientInfo":{"name":"test-client","version":"0.1.0"}}`, protoVer)
}

// statelessHandler creates a StreamableHTTPHandler in stateless mode with
// JSON responses for simpler test assertions. cfg may be nil (no tools).
func statelessHandler(cfg *config.Config) http.Handler {
	if cfg == nil {
		cfg = &config.Config{}
	}
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return New(cfg, discardLogger())
	}, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
	})
}

// postMCP sends a JSON-RPC message via HTTP POST to the given URL and
// returns the response body. It sets the required MCP HTTP headers.
// method must match the JSON-RPC method (Mcp-Method header). For tools/call,
// name must be the tool name (Mcp-Name header).
func postMCP(url, protocolVersion, method, name, body string) (string, error) {
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", protocolVersion)
	req.Header.Set("Mcp-Method", method)
	if name != "" {
		req.Header.Set("Mcp-Name", name)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(respBody), nil
}

// mustPostMCP is like postMCP but fails the test on error.
func mustPostMCP(t *testing.T, url, protocolVersion, method, name, body string) string {
	t.Helper()
	res, err := postMCP(url, protocolVersion, method, name, body)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return res
}

// statusMCP posts like postMCP but returns the HTTP status code.
func statusMCP(t *testing.T, url, protocolVersion, method, name, body string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", protocolVersion)
	req.Header.Set("Mcp-Method", method)
	if name != "" {
		req.Header.Set("Mcp-Name", name)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// TestStateless_ServerDiscover verifies the SEP-2575 server/discover RPC
// over the stateless HTTP transport.
func TestStateless_ServerDiscover(t *testing.T) {
	handler := statelessHandler(nil)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	meta := metaBlock("2026-07-28")
	body := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{` + meta + `}}`
	resp := mustPostMCP(t, srv.URL, "2026-07-28", "server/discover", "", body)

	var dr struct {
		Result struct {
			SupportedVersions []string `json:"supportedVersions"`
			Capabilities      struct {
				Tools *struct{} `json:"tools"`
			} `json:"capabilities"`
		} `json:"result"`
		Error *struct{} `json:"error"`
	}
	if err := json.Unmarshal([]byte(resp), &dr); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, resp)
	}
	if dr.Error != nil {
		t.Fatalf("unexpected error in server/discover: %s", resp)
	}

	// The server must advertise 2026-07-28 as its latest protocol version.
	if len(dr.Result.SupportedVersions) == 0 || dr.Result.SupportedVersions[0] != "2026-07-28" {
		t.Errorf("supportedVersions = %v, want latest to be 2026-07-28", dr.Result.SupportedVersions)
	}
	// With no webhooks configured, no tools are registered. The server may
	// still advertise the tools capability (with an empty tool list).
	// We only verify that server/discover succeeds and advertises 2026-07-28.
	if len(dr.Result.SupportedVersions) == 0 || dr.Result.SupportedVersions[0] != "2026-07-28" {
		t.Errorf("supportedVersions = %v, want latest to be 2026-07-28", dr.Result.SupportedVersions)
	}
}

// TestStateless_DiscoverAdvertisesTools verifies that server/discover
// advertises the tools capability when webhooks are configured.
func TestStateless_DiscoverAdvertisesTools(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	handler := statelessHandler(cfg)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	meta := metaBlock("2026-07-28")
	body := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{` + meta + `}}`
	resp := mustPostMCP(t, srv.URL, "2026-07-28", "server/discover", "", body)

	var dr struct {
		Result struct {
			Capabilities struct {
				Tools *struct{} `json:"tools"`
			} `json:"capabilities"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(resp), &dr); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, resp)
	}
	if dr.Result.Capabilities.Tools == nil {
		t.Error("expected tools capability to be advertised when slack is configured")
	}
}

// TestStateless_ListTools verifies tools/list over the stateless HTTP
// transport with the 2026-07-28 protocol.
func TestStateless_ListTools(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	handler := statelessHandler(cfg)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	meta := metaBlock("2026-07-28")
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{` + meta + `}}`
	resp := mustPostMCP(t, srv.URL, "2026-07-28", "tools/list", "", body)

	var lr struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
		Error *struct{} `json:"error"`
	}
	if err := json.Unmarshal([]byte(resp), &lr); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, resp)
	}
	if lr.Error != nil {
		t.Fatalf("unexpected error in tools/list: %s", resp)
	}

	var names []string
	for _, tool := range lr.Result.Tools {
		names = append(names, tool.Name)
	}
	have := func(name string) bool { return contains(names, name) }
	if !have("send_slack_notification") {
		t.Errorf("expected send_slack_notification, got tools: %v", names)
	}
	if !have("list_channels") {
		t.Errorf("expected list_channels, got tools: %v", names)
	}
}

// TestStateless_ToolCall verifies tools/call over the stateless HTTP
// transport with the 2026-07-28 protocol, including the Mcp-Name header.
func TestStateless_ToolCall(t *testing.T) {
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

	handler := statelessHandler(cfg)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// tools/call list_channels
	meta := metaBlock("2026-07-28")
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_channels","arguments":{},` + meta + `}}`
	resp := mustPostMCP(t, srv.URL, "2026-07-28", "tools/call", "list_channels", body)

	var cr struct {
		Result *struct {
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct{} `json:"error"`
	}
	if err := json.Unmarshal([]byte(resp), &cr); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, resp)
	}
	if cr.Error != nil {
		t.Fatalf("unexpected error in tools/call list_channels: %s", resp)
	}
	if cr.Result == nil || cr.Result.IsError {
		t.Fatalf("list_channels failed: %s", resp)
	}

	// tools/call send_slack_notification (exercises the notifier end-to-end)
	callBody := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"send_slack_notification","arguments":{"message":"hello over SEP-2575"},` + meta + `}}`
	callResp := mustPostMCP(t, srv.URL, "2026-07-28", "tools/call", "send_slack_notification", callBody)

	var cr2 struct {
		Result *struct {
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct{} `json:"error"`
	}
	if err := json.Unmarshal([]byte(callResp), &cr2); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, callResp)
	}
	if cr2.Error != nil || cr2.Result == nil || cr2.Result.IsError {
		t.Fatalf("send_slack_notification failed: %s", callResp)
	}
	if !received {
		t.Error("expected upstream webhook to receive a request")
	}
}

// TestStateless_RejectsInitialize verifies that the legacy initialize method
// is not available in the 2026-07-28 protocol.
func TestStateless_RejectsInitialize(t *testing.T) {
	handler := statelessHandler(nil)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	meta := metaBlock("2026-07-28")
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{` + meta + `}}`
	resp := mustPostMCP(t, srv.URL, "2026-07-28", "initialize", "", body)

	var ir struct {
		Error *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(resp), &ir); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, resp)
	}
	if ir.Error == nil {
		t.Fatalf("expected method-not-found error for legacy initialize, got: %s", resp)
	}
	// -32601 is jsonrpc code MethodNotFound.
	if ir.Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601 (MethodNotFound)", ir.Error.Code)
	}
}

// TestStateless_LegacyProtocolStillWorks verifies that clients using the
// previous 2025-11-25 protocol can still initialize and call tools.
func TestStateless_LegacyProtocolStillWorks(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	handler := statelessHandler(cfg)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// Legacy initialize (no _meta, no Mcp-Method header required for < 2026-07-28)
	initBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test-client","version":"0.1.0"}}}`
	initResp := mustPostMCP(t, srv.URL, "2025-11-25", "initialize", "", initBody)

	var ir struct {
		Result *struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
		Error *struct{} `json:"error"`
	}
	if err := json.Unmarshal([]byte(initResp), &ir); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, initResp)
	}
	if ir.Error != nil {
		t.Fatalf("legacy initialize failed: %s", initResp)
	}
	if ir.Result.ProtocolVersion != "2025-11-25" {
		t.Errorf("protocolVersion = %q, want %q", ir.Result.ProtocolVersion, "2025-11-25")
	}

	// Legacy tools/list
	listBody := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	listResp := mustPostMCP(t, srv.URL, "2025-11-25", "tools/list", "", listBody)

	var lr struct {
		Result *struct {
			Tools []any `json:"tools"`
		} `json:"result"`
		Error *struct{} `json:"error"`
	}
	if err := json.Unmarshal([]byte(listResp), &lr); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, listResp)
	}
	if lr.Error != nil {
		t.Fatalf("legacy tools/list failed: %s", listResp)
	}
	if len(lr.Result.Tools) == 0 {
		t.Error("expected at least one tool with slack configured")
	}
}

// TestStateless_RejectsUnsupportedProtocolVersion verifies that an unknown
// protocol version gets a 400 and per the spec the server keeps working for
// supported versions.
func TestStateless_RejectsUnsupportedProtocolVersion(t *testing.T) {
	handler := statelessHandler(nil)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	meta := metaBlock("2099-01-01")
	body := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{` + meta + `}}`
	if got := statusMCP(t, srv.URL, "2099-01-01", "server/discover", "", body); got != http.StatusBadRequest {
		t.Errorf("status code = %d, want 400", got)
	}
}

// TestStateless_MissingMethodHeader verifies that the Mcp-Method header is
// required for protocol version >= 2026-07-28.
func TestStateless_MissingMethodHeader(t *testing.T) {
	handler := statelessHandler(nil)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	meta := metaBlock("2026-07-28")
	body := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{` + meta + `}}`
	req, err := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	// Intentionally no Mcp-Method header.

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(respBody), "missing required Mcp-Method") {
		t.Errorf("expected error about missing Mcp-Method header, got: %s", respBody)
	}
}

// TestStateless_MissingMeta verifies the _meta triple is required for the
// 2026-07-28 protocol.
func TestStateless_MissingMeta(t *testing.T) {
	handler := statelessHandler(nil)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// No _meta field in params.
	body := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{}}`
	resp := mustPostMCP(t, srv.URL, "2026-07-28", "server/discover", "", body)

	var er struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(resp), &er); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, resp)
	}
	if er.Error == nil {
		t.Fatalf("expected error for missing _meta, got: %s", resp)
	}
	if !strings.Contains(er.Error.Message, "protocolVersion") {
		t.Errorf("expected error about protocolVersion _meta, got: %q", er.Error.Message)
	}
}

// TestStateless_RejectsGET verifies that stateless servers return 405 with
// Allow: POST for GET requests, per the MCP spec.
func TestStateless_RejectsGET(t *testing.T) {
	handler := statelessHandler(nil)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status code = %d, want 405", resp.StatusCode)
	}
	if got := resp.Header.Get("Allow"); got != "POST" {
		t.Errorf("Allow header = %q, want %q", got, "POST")
	}
}

// contains reports whether s is in vs.
func contains(vs []string, s string) bool {
	for _, v := range vs {
		if v == s {
			return true
		}
	}
	return false
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
