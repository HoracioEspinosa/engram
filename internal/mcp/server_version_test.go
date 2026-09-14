package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

// initializeServerInfo drives a real MCP initialize handshake against srv and
// returns the serverInfo block it answers with.
func initializeServerInfo(t *testing.T, cfg MCPConfig) map[string]any {
	t.Helper()
	s := newMCPTestStore(t)
	srv := NewServerWithConfig(s, cfg, map[string]bool{})

	request := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`)
	raw, err := json.Marshal(srv.HandleMessage(context.Background(), request))
	if err != nil {
		t.Fatalf("marshal initialize response: %v", err)
	}
	var response struct {
		Result struct {
			ServerInfo map[string]any `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatalf("decode initialize response %s: %v", raw, err)
	}
	if response.Result.ServerInfo == nil {
		t.Fatalf("initialize answered without serverInfo: %s", raw)
	}
	return response.Result.ServerInfo
}

// TestServerVersionComesFromTheBuild pins that the version an MCP host sees is
// the binary's own version rather than a constant frozen at the first release.
func TestServerVersionComesFromTheBuild(t *testing.T) {
	t.Run("configured version", func(t *testing.T) {
		info := initializeServerInfo(t, MCPConfig{ServerVersion: "1.4.2"})
		if info["version"] != "1.4.2" {
			t.Fatalf("expected the build version, got %#v", info["version"])
		}
		if info["name"] != "engram" {
			t.Fatalf("the server name must not change, got %#v", info["name"])
		}
	})

	t.Run("unset version falls back to dev", func(t *testing.T) {
		info := initializeServerInfo(t, MCPConfig{})
		if info["version"] != "dev" {
			t.Fatalf("expected the dev fallback, got %#v", info["version"])
		}
	})
}
