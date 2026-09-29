package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestExportedSymbolsSmoke pins the ABI contract end to end: create → call →
// stop through the real C entry points (via the Go wrappers), envelope shapes
// intact, malloc/free discipline honored.
func TestExportedSymbolsSmoke(t *testing.T) {
	env := mustParse(t, ffiCreate([]byte(`{}`)))
	if env["ok"] != true {
		t.Fatalf("create failed: %v", env)
	}
	result := env["result"].(map[string]any)
	handle := int64(result["handle"].(float64))
	if result["peer_id"].(string) == "" {
		t.Fatal("empty peer_id")
	}

	envInfo := mustParse(t, ffiCall(handle, []byte(`{"method":"info"}`)))
	if envInfo["ok"] != true {
		t.Fatalf("info failed: %v", envInfo)
	}
	if got := envInfo["result"].(map[string]any)["peer_id"]; got != result["peer_id"] {
		t.Fatalf("peer_id mismatch: %v vs %v", got, result["peer_id"])
	}

	envErr := mustParse(t, ffiCall(handle, []byte(`{"method":"nonexistent"}`)))
	if envErr["ok"] != false || !strings.Contains(envErr["error"].(string), "unknown method") {
		t.Fatalf("expected unknown-method error, got: %v", envErr)
	}

	ffiStop(handle)

	envStopped := mustParse(t, ffiCall(handle, []byte(`{"method":"info"}`)))
	if envStopped["ok"] != false {
		t.Fatalf("call after stop must fail, got: %v", envStopped)
	}
}

func mustParse(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(b, &env); err != nil {
		t.Fatalf("envelope is not JSON: %v (%s)", err, b)
	}
	return env
}
