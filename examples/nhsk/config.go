package nhsk

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lijiawang/GameServiceRuntime/examples/nhsk/internal/legacywire"
)

type processRole string

const processRoleGameLogic processRole = "gamelogic"

type appConfig struct {
	Role            processRole      `json:"role"`
	Node            nodeConfig       `json:"node"`
	LegacyGM        legacyGMConfig   `json:"legacy_gm"`
	MySQL           mysqlConfig      `json:"mysql"`
	Redis           redisConfig      `json:"redis"`
	WeChat          weChatConfig     `json:"wechat"`
	CustomDeck      customDeckConfig `json:"custom_deck"`
	Logging         loggingConfig    `json:"logging"`
	ShutdownTimeout configDuration   `json:"shutdown_timeout"`
}

type nodeConfig struct {
	ID               string `json:"id"`
	Workers          int    `json:"workers"`
	MaxActiveBattles uint32 `json:"max_active_battles"`
}

type legacyGMConfig struct {
	Address           string         `json:"address"`
	DialTimeout       configDuration `json:"dial_timeout"`
	OriginTimeout     configDuration `json:"origin_timeout"`
	InitialBackoff    configDuration `json:"initial_backoff"`
	MaxBackoff        configDuration `json:"max_backoff"`
	BackoffMultiplier float64        `json:"backoff_multiplier"`
	Jitter            float64        `json:"jitter"`
	StableReset       configDuration `json:"stable_reset"`
}

type mysqlConfig struct {
	Enabled bool   `json:"enabled"`
	DSN     string `json:"dsn"`
}

type redisConfig struct {
	Enabled  bool   `json:"enabled"`
	Address  string `json:"address"`
	Password string `json:"password"`
	DB       int    `json:"db"`
}

type weChatConfig struct {
	Enabled   bool   `json:"enabled"`
	AppID     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
}

type customDeckConfig struct {
	Enabled         bool           `json:"enabled"`
	FilePath        string         `json:"file"`
	AllowAnyAccount bool           `json:"allow_any_account"`
	AllowedAccounts []uint32       `json:"allowed_accounts"`
	QueueSize       int            `json:"queue_size"`
	Workers         int            `json:"workers"`
	LoadTimeout     configDuration `json:"load_timeout"`
}

type loggingConfig struct {
	Level string `json:"level"`
}

type configDuration time.Duration

func (duration *configDuration) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return errors.New("duration must be a string")
	}
	parsed, err := time.ParseDuration(text)
	if err != nil {
		return errors.New("duration has invalid syntax")
	}
	*duration = configDuration(parsed)
	return nil
}

func loadConfig(path string) (appConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return appConfig{}, fmt.Errorf("nhsk config: open: %w", err)
	}
	defer file.Close()

	config := defaultConfig()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return appConfig{}, fmt.Errorf("nhsk config: decode: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return appConfig{}, err
	}
	if err := applyEnvironment(&config); err != nil {
		return appConfig{}, err
	}
	if err := config.validate(); err != nil {
		return appConfig{}, err
	}
	return config, nil
}

func defaultConfig() appConfig {
	connection := legacywire.DefaultConnectionConfig()
	return appConfig{
		LegacyGM: legacyGMConfig{
			DialTimeout:       configDuration(connection.DialTimeout),
			OriginTimeout:     configDuration(connection.OriginTimeout),
			InitialBackoff:    configDuration(connection.InitialBackoff),
			MaxBackoff:        configDuration(connection.MaxBackoff),
			BackoffMultiplier: connection.BackoffMultiplier,
			Jitter:            connection.JitterRatio,
			StableReset:       configDuration(connection.StableResetAfter),
		},
		Logging: loggingConfig{Level: "info"},
	}
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("nhsk config: decode: trailing JSON document")
	}
	return fmt.Errorf("nhsk config: decode trailing data: %w", err)
}

func applyEnvironment(config *appConfig) error {
	overrideString("NHSK_NODE_ID", &config.Node.ID)
	overrideString("NHSK_LEGACY_GM_ADDRESS", &config.LegacyGM.Address)
	overrideString("NHSK_MYSQL_DSN", &config.MySQL.DSN)
	overrideString("NHSK_REDIS_ADDRESS", &config.Redis.Address)
	overrideString("NHSK_REDIS_PASSWORD", &config.Redis.Password)
	overrideString("NHSK_WECHAT_APP_ID", &config.WeChat.AppID)
	overrideString("NHSK_WECHAT_APP_SECRET", &config.WeChat.AppSecret)
	overrideString("NHSK_LOG_LEVEL", &config.Logging.Level)

	if err := overrideInt("NHSK_WORKERS", &config.Node.Workers); err != nil {
		return err
	}
	if err := overrideUint32("NHSK_MAX_ACTIVE_BATTLES", &config.Node.MaxActiveBattles); err != nil {
		return err
	}
	if err := overrideDuration("NHSK_GM_DIAL_TIMEOUT", &config.LegacyGM.DialTimeout); err != nil {
		return err
	}
	if err := overrideDuration("NHSK_GM_ORIGIN_TIMEOUT", &config.LegacyGM.OriginTimeout); err != nil {
		return err
	}
	if err := overrideDuration("NHSK_GM_INITIAL_BACKOFF", &config.LegacyGM.InitialBackoff); err != nil {
		return err
	}
	if err := overrideDuration("NHSK_GM_MAX_BACKOFF", &config.LegacyGM.MaxBackoff); err != nil {
		return err
	}
	if err := overrideFloat("NHSK_GM_BACKOFF_MULTIPLIER", &config.LegacyGM.BackoffMultiplier); err != nil {
		return err
	}
	if err := overrideFloat("NHSK_GM_JITTER", &config.LegacyGM.Jitter); err != nil {
		return err
	}
	if err := overrideDuration("NHSK_GM_STABLE_RESET", &config.LegacyGM.StableReset); err != nil {
		return err
	}
	return overrideDuration("NHSK_SHUTDOWN_TIMEOUT", &config.ShutdownTimeout)
}

func overrideString(name string, target *string) {
	if value, ok := os.LookupEnv(name); ok {
		*target = value
	}
}

func overrideInt(name string, target *int) error {
	value, ok := os.LookupEnv(name)
	if !ok {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return environmentError(name)
	}
	*target = parsed
	return nil
}

func overrideUint32(name string, target *uint32) error {
	value, ok := os.LookupEnv(name)
	if !ok {
		return nil
	}
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return environmentError(name)
	}
	*target = uint32(parsed)
	return nil
}

func overrideDuration(name string, target *configDuration) error {
	value, ok := os.LookupEnv(name)
	if !ok {
		return nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return environmentError(name)
	}
	*target = configDuration(parsed)
	return nil
}

func overrideFloat(name string, target *float64) error {
	value, ok := os.LookupEnv(name)
	if !ok {
		return nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return environmentError(name)
	}
	*target = parsed
	return nil
}

func environmentError(name string) error {
	return fmt.Errorf("nhsk config: environment %s has invalid syntax", name)
}

func (config appConfig) validate() error {
	if config.Role != processRoleGameLogic {
		return configFieldError("role", "must be gamelogic")
	}
	if strings.TrimSpace(config.Node.ID) == "" {
		return configFieldError("node.id", "is required")
	}
	if config.Node.Workers <= 0 {
		return configFieldError("node.workers", "must be positive")
	}
	if config.Node.MaxActiveBattles == 0 {
		return configFieldError("node.max_active_battles", "must be positive")
	}
	if err := validateAddress(config.LegacyGM.Address); err != nil {
		return configFieldError("legacy_gm.address", "must be a valid host:port")
	}
	if config.LegacyGM.DialTimeout <= 0 {
		return configFieldError("legacy_gm.dial_timeout", "must be positive")
	}
	if config.LegacyGM.OriginTimeout <= 0 {
		return configFieldError("legacy_gm.origin_timeout", "must be positive")
	}
	if config.LegacyGM.InitialBackoff <= 0 {
		return configFieldError("legacy_gm.initial_backoff", "must be positive")
	}
	if config.LegacyGM.MaxBackoff <= 0 {
		return configFieldError("legacy_gm.max_backoff", "must be positive")
	}
	if config.LegacyGM.InitialBackoff > config.LegacyGM.MaxBackoff {
		return configFieldError("legacy_gm.initial_backoff", "must not exceed max_backoff")
	}
	if math.IsNaN(config.LegacyGM.BackoffMultiplier) || math.IsInf(config.LegacyGM.BackoffMultiplier, 0) || config.LegacyGM.BackoffMultiplier <= 1 {
		return configFieldError("legacy_gm.backoff_multiplier", "must be greater than 1")
	}
	if math.IsNaN(config.LegacyGM.Jitter) || math.IsInf(config.LegacyGM.Jitter, 0) || config.LegacyGM.Jitter <= 0 || config.LegacyGM.Jitter >= 1 {
		return configFieldError("legacy_gm.jitter", "must be greater than 0 and less than 1")
	}
	if config.LegacyGM.StableReset <= 0 {
		return configFieldError("legacy_gm.stable_reset", "must be positive")
	}
	if config.ShutdownTimeout <= 0 {
		return configFieldError("shutdown_timeout", "must be positive")
	}
	if _, err := parseLogLevel(config.Logging.Level); err != nil {
		return configFieldError("logging.level", "must be debug, info, warn, or error")
	}
	if config.MySQL.Enabled && strings.TrimSpace(config.MySQL.DSN) == "" {
		return configFieldError("mysql.dsn", "is required when enabled")
	}
	if config.Redis.Enabled {
		if err := validateAddress(config.Redis.Address); err != nil {
			return configFieldError("redis.address", "must be a valid host:port when enabled")
		}
		if config.Redis.DB < 0 {
			return configFieldError("redis.db", "must not be negative")
		}
	}
	if config.WeChat.Enabled {
		if strings.TrimSpace(config.WeChat.AppID) == "" {
			return configFieldError("wechat.app_id", "is required when enabled")
		}
		if strings.TrimSpace(config.WeChat.AppSecret) == "" {
			return configFieldError("wechat.app_secret", "is required when enabled")
		}
	}
	if config.CustomDeck.Enabled && strings.TrimSpace(config.CustomDeck.FilePath) == "" {
		return configFieldError("custom_deck.file", "is required when custom_deck.enabled is true")
	}
	if config.CustomDeck.QueueSize < 0 {
		return configFieldError("custom_deck.queue_size", "must not be negative")
	}
	if config.CustomDeck.Workers < 0 {
		return configFieldError("custom_deck.workers", "must not be negative")
	}
	if config.CustomDeck.LoadTimeout < 0 {
		return configFieldError("custom_deck.load_timeout", "must not be negative")
	}
	return nil
}

func validateAddress(address string) error {
	host, portText, err := net.SplitHostPort(address)
	if err != nil || strings.TrimSpace(host) == "" {
		return errors.New("invalid address")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return errors.New("invalid port")
	}
	return nil
}

func configFieldError(field, rule string) error {
	return fmt.Errorf("nhsk config: %s %s", field, rule)
}
