package main

import (
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const redactedLogValue = "[REDACTED]"

type failureCategory string

const failureCategoryTransport failureCategory = "transport"

type battleLogIdentity struct {
	BattleID             game.BattleID
	Ref                  gsr.ServiceRef
	ConnectionGeneration uint64
}

func newLogger(output io.Writer, config loggingConfig, role processRole, node nodeConfig) (*slog.Logger, error) {
	if output == nil {
		return nil, fmt.Errorf("nhsk logger: output is required")
	}
	level, err := parseLogLevel(config.Level)
	if err != nil {
		return nil, err
	}
	handler := slog.NewJSONHandler(output, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: redactLogAttr,
	})
	return slog.New(handler).With(
		slog.String("node_id", node.ID),
		slog.String("process_role", string(role)),
	), nil
}

func parseLogLevel(value string) (slog.Level, error) {
	switch value {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("nhsk logger: invalid level")
	}
}

func withBattleLogger(logger *slog.Logger, identity battleLogIdentity) *slog.Logger {
	return logger.With(
		slog.Uint64("battle_id", uint64(identity.BattleID)),
		slog.String("service_node", string(identity.Ref.Node)),
		slog.Uint64("service_id", uint64(identity.Ref.ID)),
		slog.Uint64("connection_generation", identity.ConnectionGeneration),
	)
}

func failureCategoryAttr(category failureCategory) slog.Attr {
	return slog.String("error_category", string(category))
}

func redactLogAttr(_ []string, attr slog.Attr) slog.Attr {
	if sensitiveLogKey(attr.Key) {
		return slog.String(attr.Key, redactedLogValue)
	}
	return attr
}

func sensitiveLogKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if strings.Contains(key, "token") || strings.Contains(key, "secret") || strings.Contains(key, "proof") {
		return true
	}
	switch strings.NewReplacer("-", "_", ".", "_").Replace(key) {
	case "code", "wechat_code", "wx_code", "error", "err", "cause":
		return true
	default:
		return false
	}
}
