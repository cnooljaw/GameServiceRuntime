package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigAppliesDefaultsAndEnvironmentOverrides(t *testing.T) {
	path := writeConfig(t, `{
		"role": "gamelogic",
		"node": {
			"id": "nhsk-gl-file",
			"workers": 2,
			"max_active_battles": 10000
		},
		"legacy_gm": {
			"address": "127.0.0.1:9000"
		},
		"mysql": {"enabled": false},
		"redis": {"enabled": false},
		"wechat": {"enabled": false},
		"logging": {"level": "info"},
		"shutdown_timeout": "10s"
	}`)

	t.Setenv("NHSK_NODE_ID", "nhsk-gl-env")
	t.Setenv("NHSK_LEGACY_GM_ADDRESS", "127.0.0.1:9100")
	t.Setenv("NHSK_LOG_LEVEL", "debug")

	config, err := loadConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if config.Role != processRoleGameLogic {
		t.Fatalf("role = %q, want %q", config.Role, processRoleGameLogic)
	}
	if config.Node.ID != "nhsk-gl-env" {
		t.Fatalf("node id = %q, want environment override", config.Node.ID)
	}
	if config.LegacyGM.Address != "127.0.0.1:9100" {
		t.Fatalf("legacy GM address = %q, want environment override", config.LegacyGM.Address)
	}
	if config.Logging.Level != "debug" {
		t.Fatalf("log level = %q, want environment override", config.Logging.Level)
	}

	wantDurations := map[string]struct {
		got  time.Duration
		want time.Duration
	}{
		"dial":         {time.Duration(config.LegacyGM.DialTimeout), 5 * time.Second},
		"origin":       {time.Duration(config.LegacyGM.OriginTimeout), 5 * time.Second},
		"initial":      {time.Duration(config.LegacyGM.InitialBackoff), time.Second},
		"maximum":      {time.Duration(config.LegacyGM.MaxBackoff), 30 * time.Second},
		"stable reset": {time.Duration(config.LegacyGM.StableReset), 60 * time.Second},
	}
	for name, duration := range wantDurations {
		if duration.got != duration.want {
			t.Errorf("%s duration = %v, want %v", name, duration.got, duration.want)
		}
	}
	if config.LegacyGM.BackoffMultiplier != 2 {
		t.Errorf("backoff multiplier = %v, want 2", config.LegacyGM.BackoffMultiplier)
	}
	if config.LegacyGM.Jitter != 0.2 {
		t.Errorf("jitter = %v, want 0.2", config.LegacyGM.Jitter)
	}
	if time.Duration(config.ShutdownTimeout) != 10*time.Second {
		t.Errorf("shutdown timeout = %v, want 10s", config.ShutdownTimeout)
	}
}

func TestLoadConfigRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		mutate   string
		wantText string
	}{
		{name: "missing role", mutate: `"role": ""`, wantText: "role"},
		{name: "unsupported role", mutate: `"role": "gateway"`, wantText: "role"},
		{name: "missing node id", mutate: `"id": ""`, wantText: "node.id"},
		{name: "zero workers", mutate: `"workers": 0`, wantText: "node.workers"},
		{name: "zero battle capacity", mutate: `"max_active_battles": 0`, wantText: "node.max_active_battles"},
		{name: "invalid GM address", mutate: `"address": "not-an-address"`, wantText: "legacy_gm.address"},
		{name: "zero dial timeout", mutate: `"dial_timeout": "0s"`, wantText: "legacy_gm.dial_timeout"},
		{name: "zero origin timeout", mutate: `"origin_timeout": "0s"`, wantText: "legacy_gm.origin_timeout"},
		{name: "initial greater than maximum", mutate: `"initial_backoff": "31s"`, wantText: "legacy_gm.initial_backoff"},
		{name: "invalid multiplier", mutate: `"backoff_multiplier": 1`, wantText: "legacy_gm.backoff_multiplier"},
		{name: "zero jitter", mutate: `"jitter": 0`, wantText: "legacy_gm.jitter"},
		{name: "unit jitter", mutate: `"jitter": 1`, wantText: "legacy_gm.jitter"},
		{name: "zero stable reset", mutate: `"stable_reset": "0s"`, wantText: "legacy_gm.stable_reset"},
		{name: "zero shutdown timeout", mutate: `"shutdown_timeout": "0s"`, wantText: "shutdown_timeout"},
		{name: "invalid log level", mutate: `"level": "verbose"`, wantText: "logging.level"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := writeConfig(t, strings.Replace(validConfigJSON, validReplacementFor(test.mutate), test.mutate, 1))
			_, err := loadConfig(path)
			if err == nil {
				t.Fatal("load config succeeded, want error")
			}
			if !strings.Contains(err.Error(), test.wantText) {
				t.Fatalf("error = %q, want field %q", err, test.wantText)
			}
		})
	}
}

func TestLoadConfigRejectsMissingRequiredConfiguration(t *testing.T) {
	_, err := loadConfig(writeConfig(t, `{}`))
	if err == nil {
		t.Fatal("load config succeeded, want missing role error")
	}
	if !strings.Contains(err.Error(), "role") {
		t.Fatalf("error = %q, want role field", err)
	}
}

func TestLoadConfigRejectsUnknownFieldsAndTrailingDocuments(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "unknown field",
			body: strings.Replace(validConfigJSON, `"shutdown_timeout": "10s"`, `"shutdown_timeout": "10s", "mystery": true`, 1),
		},
		{
			name: "trailing document",
			body: validConfigJSON + ` {"role":"gamelogic"}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := loadConfig(writeConfig(t, test.body))
			if err == nil {
				t.Fatal("load config succeeded, want decode error")
			}
		})
	}
}

func TestLoadConfigValidatesEnabledExternalToolsWithoutLeakingSecrets(t *testing.T) {
	const (
		mysqlSecret  = "mysql-secret-value"
		redisSecret  = "redis-secret-value"
		wechatSecret = "wechat-secret-value"
	)
	body := strings.Replace(validConfigJSON,
		`"mysql": {"enabled": false}`,
		`"mysql": {"enabled": true, "dsn": "root:`+mysqlSecret+`@tcp(localhost:3306)/game"}`,
		1,
	)
	body = strings.Replace(body,
		`"redis": {"enabled": false}`,
		`"redis": {"enabled": true, "address": "bad-address", "password": "`+redisSecret+`", "db": 0}`,
		1,
	)
	body = strings.Replace(body,
		`"wechat": {"enabled": false}`,
		`"wechat": {"enabled": true, "app_id": "nhsk-app", "app_secret": "`+wechatSecret+`"}`,
		1,
	)

	_, err := loadConfig(writeConfig(t, body))
	if err == nil {
		t.Fatal("load config succeeded, want invalid Redis address")
	}
	for _, secret := range []string{mysqlSecret, redisSecret, wechatSecret} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("formatted error leaked secret %q: %v", secret, err)
		}
	}
}

func TestLoadConfigEnvironmentCanSupplyExternalToolSecrets(t *testing.T) {
	body := strings.Replace(validConfigJSON,
		`"mysql": {"enabled": false}`,
		`"mysql": {"enabled": true}`, 1)
	body = strings.Replace(body,
		`"redis": {"enabled": false}`,
		`"redis": {"enabled": true, "address": "127.0.0.1:6379"}`, 1)
	body = strings.Replace(body,
		`"wechat": {"enabled": false}`,
		`"wechat": {"enabled": true}`, 1)

	t.Setenv("NHSK_MYSQL_DSN", "root:password@tcp(localhost:3306)/game")
	t.Setenv("NHSK_REDIS_PASSWORD", "redis-password")
	t.Setenv("NHSK_WECHAT_APP_ID", "wechat-app")
	t.Setenv("NHSK_WECHAT_APP_SECRET", "wechat-secret")

	config, err := loadConfig(writeConfig(t, body))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if config.MySQL.DSN == "" || config.Redis.Password == "" || config.WeChat.AppSecret == "" {
		t.Fatal("environment secrets were not applied")
	}
}

func TestLoadConfigAppliesOperationalEnvironmentOverrides(t *testing.T) {
	t.Setenv("NHSK_WORKERS", "8")
	t.Setenv("NHSK_MAX_ACTIVE_BATTLES", "8000")
	t.Setenv("NHSK_GM_DIAL_TIMEOUT", "6s")
	t.Setenv("NHSK_GM_ORIGIN_TIMEOUT", "7s")
	t.Setenv("NHSK_GM_INITIAL_BACKOFF", "2s")
	t.Setenv("NHSK_GM_MAX_BACKOFF", "40s")
	t.Setenv("NHSK_GM_BACKOFF_MULTIPLIER", "3")
	t.Setenv("NHSK_GM_JITTER", "0.3")
	t.Setenv("NHSK_GM_STABLE_RESET", "70s")
	t.Setenv("NHSK_SHUTDOWN_TIMEOUT", "20s")

	config, err := loadConfig(writeConfig(t, validConfigJSON))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if config.Node.Workers != 8 || config.Node.MaxActiveBattles != 8000 {
		t.Fatalf("node overrides = (%d, %d), want (8, 8000)", config.Node.Workers, config.Node.MaxActiveBattles)
	}
	if time.Duration(config.LegacyGM.DialTimeout) != 6*time.Second ||
		time.Duration(config.LegacyGM.OriginTimeout) != 7*time.Second ||
		time.Duration(config.LegacyGM.InitialBackoff) != 2*time.Second ||
		time.Duration(config.LegacyGM.MaxBackoff) != 40*time.Second ||
		config.LegacyGM.BackoffMultiplier != 3 ||
		config.LegacyGM.Jitter != 0.3 ||
		time.Duration(config.LegacyGM.StableReset) != 70*time.Second {
		t.Fatalf("legacy GM environment overrides were not applied: %+v", config.LegacyGM)
	}
	if time.Duration(config.ShutdownTimeout) != 20*time.Second {
		t.Fatalf("shutdown timeout = %v, want 20s", config.ShutdownTimeout)
	}
}

func TestLoadConfigRejectsInvalidEnvironmentWithoutPrintingItsValue(t *testing.T) {
	const invalidValue = "a-sensitive-invalid-value"
	t.Setenv("NHSK_GM_DIAL_TIMEOUT", invalidValue)

	_, err := loadConfig(writeConfig(t, validConfigJSON))
	if err == nil {
		t.Fatal("load config succeeded, want environment syntax error")
	}
	if !strings.Contains(err.Error(), "NHSK_GM_DIAL_TIMEOUT") {
		t.Fatalf("error = %q, want environment name", err)
	}
	if strings.Contains(err.Error(), invalidValue) {
		t.Fatalf("error leaked environment value: %v", err)
	}
}

func TestLoadConfigRejectsNonFiniteBackoffEnvironment(t *testing.T) {
	t.Setenv("NHSK_GM_JITTER", "NaN")

	_, err := loadConfig(writeConfig(t, validConfigJSON))
	if err == nil || !strings.Contains(err.Error(), "legacy_gm.jitter") {
		t.Fatalf("error = %v, want jitter validation error", err)
	}
}

func TestLoadConfigRequiresEnabledExternalToolSettings(t *testing.T) {
	tests := []struct {
		name     string
		old      string
		new      string
		wantText string
	}{
		{name: "mysql DSN", old: `"mysql": {"enabled": false}`, new: `"mysql": {"enabled": true}`, wantText: "mysql.dsn"},
		{name: "redis address", old: `"redis": {"enabled": false}`, new: `"redis": {"enabled": true}`, wantText: "redis.address"},
		{name: "redis DB", old: `"redis": {"enabled": false}`, new: `"redis": {"enabled": true, "address": "127.0.0.1:6379", "db": -1}`, wantText: "redis.db"},
		{name: "wechat app ID", old: `"wechat": {"enabled": false}`, new: `"wechat": {"enabled": true}`, wantText: "wechat.app_id"},
		{name: "wechat app secret", old: `"wechat": {"enabled": false}`, new: `"wechat": {"enabled": true, "app_id": "app"}`, wantText: "wechat.app_secret"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := strings.Replace(validConfigJSON, test.old, test.new, 1)
			_, err := loadConfig(writeConfig(t, body))
			if err == nil || !strings.Contains(err.Error(), test.wantText) {
				t.Fatalf("error = %v, want field %q", err, test.wantText)
			}
		})
	}
}

const validConfigJSON = `{
	"role": "gamelogic",
	"node": {
		"id": "nhsk-gl-1",
		"workers": 2,
		"max_active_battles": 10000
	},
	"legacy_gm": {
		"address": "127.0.0.1:9000",
		"dial_timeout": "5s",
		"origin_timeout": "5s",
		"initial_backoff": "1s",
		"max_backoff": "30s",
		"backoff_multiplier": 2,
		"jitter": 0.2,
		"stable_reset": "60s"
	},
	"mysql": {"enabled": false},
	"redis": {"enabled": false},
	"wechat": {"enabled": false},
	"logging": {"level": "info"},
	"shutdown_timeout": "10s"
}`

func validReplacementFor(replacement string) string {
	switch {
	case strings.Contains(replacement, `"role"`):
		return `"role": "gamelogic"`
	case strings.Contains(replacement, `"id"`):
		return `"id": "nhsk-gl-1"`
	case strings.Contains(replacement, `"workers"`):
		return `"workers": 2`
	case strings.Contains(replacement, `"max_active_battles"`):
		return `"max_active_battles": 10000`
	case strings.Contains(replacement, `"address"`):
		return `"address": "127.0.0.1:9000"`
	case strings.Contains(replacement, `"dial_timeout"`):
		return `"dial_timeout": "5s"`
	case strings.Contains(replacement, `"origin_timeout"`):
		return `"origin_timeout": "5s"`
	case strings.Contains(replacement, `"initial_backoff"`):
		return `"initial_backoff": "1s"`
	case strings.Contains(replacement, `"backoff_multiplier"`):
		return `"backoff_multiplier": 2`
	case strings.Contains(replacement, `"jitter"`):
		return `"jitter": 0.2`
	case strings.Contains(replacement, `"stable_reset"`):
		return `"stable_reset": "60s"`
	case strings.Contains(replacement, `"shutdown_timeout"`):
		return `"shutdown_timeout": "10s"`
	case strings.Contains(replacement, `"level"`):
		return `"level": "info"`
	default:
		panic("unsupported replacement: " + replacement)
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
