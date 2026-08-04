package nhsk

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestLoggingWritesJSONWithStableProcessFields(t *testing.T) {
	var output bytes.Buffer
	logger, err := newLogger(&output, loggingConfig{Level: "info"}, processRoleGameLogic, nodeConfig{ID: "nhsk-gl-1"})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}

	logger.Info("node started", "readiness", "not_ready")
	record := decodeLogRecord(t, output.Bytes())
	assertLogField(t, record, "level", "INFO")
	assertLogField(t, record, "msg", "node started")
	assertLogField(t, record, "node_id", "nhsk-gl-1")
	assertLogField(t, record, "process_role", "gamelogic")
	assertLogField(t, record, "readiness", "not_ready")
}

func TestLoggingHonorsConfiguredLevel(t *testing.T) {
	var output bytes.Buffer
	logger, err := newLogger(&output, loggingConfig{Level: "warn"}, processRoleGameLogic, nodeConfig{ID: "nhsk-gl-1"})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}

	logger.Info("filtered")
	logger.Warn("retained")
	record := decodeLogRecord(t, output.Bytes())
	assertLogField(t, record, "level", "WARN")
	assertLogField(t, record, "msg", "retained")
}

func TestLoggingRejectsUnknownLevel(t *testing.T) {
	_, err := newLogger(&bytes.Buffer{}, loggingConfig{Level: "verbose"}, processRoleGameLogic, nodeConfig{ID: "nhsk-gl-1"})
	if err == nil {
		t.Fatal("new logger succeeded, want invalid level error")
	}
}

func TestLoggingAddsStableRuntimeAndBusinessFields(t *testing.T) {
	var output bytes.Buffer
	logger, err := newLogger(&output, loggingConfig{Level: "info"}, processRoleGameLogic, nodeConfig{ID: "nhsk-gl-1"})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	logger = withBattleLogger(logger, battleLogIdentity{
		BattleID:             game.BattleID(42),
		Ref:                  gsr.ServiceRef{Node: "nhsk-gl-1", ID: 9},
		ConnectionGeneration: 3,
	})

	logger.Info("battle command", slog.Uint64("command_id", 0x7701))
	record := decodeLogRecord(t, output.Bytes())
	assertLogNumber(t, record, "battle_id", 42)
	assertLogField(t, record, "service_node", "nhsk-gl-1")
	assertLogNumber(t, record, "service_id", 9)
	assertLogNumber(t, record, "connection_generation", 3)
	assertLogNumber(t, record, "command_id", 0x7701)
}

func TestLoggingRedactsSensitiveAttributesIncludingGroups(t *testing.T) {
	var output bytes.Buffer
	logger, err := newLogger(&output, loggingConfig{Level: "info"}, processRoleGameLogic, nodeConfig{ID: "nhsk-gl-1"})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}

	logger.Info("authentication rejected",
		"token", "token-value",
		"app_secret", "secret-value",
		"gateway-proof", "proof-value",
		slog.Group("wechat", slog.String("code", "code-value"), slog.String("account", "alice")),
	)
	record := decodeLogRecord(t, output.Bytes())
	for _, key := range []string{"token", "app_secret", "gateway-proof"} {
		assertLogField(t, record, key, redactedLogValue)
	}
	wechat, ok := record["wechat"].(map[string]any)
	if !ok {
		t.Fatalf("wechat = %#v, want JSON object", record["wechat"])
	}
	assertLogField(t, wechat, "code", redactedLogValue)
	assertLogField(t, wechat, "account", "alice")
	for _, secret := range []string{"token-value", "secret-value", "proof-value", "code-value"} {
		if bytes.Contains(output.Bytes(), []byte(secret)) {
			t.Fatalf("log output leaked %q: %s", secret, output.Bytes())
		}
	}
}

func TestLoggingFailureCategoryIsStableAndCauseIsNotRequired(t *testing.T) {
	var output bytes.Buffer
	logger, err := newLogger(&output, loggingConfig{Level: "info"}, processRoleGameLogic, nodeConfig{ID: "nhsk-gl-1"})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}

	logger.Error("GM connection failed", failureCategoryAttr(failureCategoryTransport))
	record := decodeLogRecord(t, output.Bytes())
	assertLogField(t, record, "error_category", "transport")
	if _, exists := record["error"]; exists {
		t.Fatalf("record unexpectedly contains raw error: %#v", record)
	}
}

func TestLoggingRedactsRawErrorCause(t *testing.T) {
	var output bytes.Buffer
	logger, err := newLogger(&output, loggingConfig{Level: "info"}, processRoleGameLogic, nodeConfig{ID: "nhsk-gl-1"})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}

	logger.Error("operation failed", "error", "upstream included secret-value")
	record := decodeLogRecord(t, output.Bytes())
	assertLogField(t, record, "error", redactedLogValue)
	if bytes.Contains(output.Bytes(), []byte("secret-value")) {
		t.Fatalf("log output leaked raw error cause: %s", output.Bytes())
	}
}

func decodeLogRecord(t *testing.T, data []byte) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	var record map[string]any
	if err := decoder.Decode(&record); err != nil {
		t.Fatalf("decode log record %q: %v", data, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		t.Fatalf("log contains trailing JSON record: %#v", trailing)
	}
	return record
}

func assertLogField(t *testing.T, record map[string]any, key string, want any) {
	t.Helper()
	if got := record[key]; got != want {
		t.Fatalf("log field %q = %#v, want %#v; record=%#v", key, got, want, record)
	}
}

func assertLogNumber(t *testing.T, record map[string]any, key string, want float64) {
	t.Helper()
	assertLogField(t, record, key, want)
}
