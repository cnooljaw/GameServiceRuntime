package nhsk

import (
	"strconv"
	"strings"
	"time"
)

// NHSKConfig is the immutable rule projection consumed by an NHSK Battle.
// Values that belong to GameMaster or have no reachable NHSK behavior are not
// represented here.
type NHSKConfig struct {
	// MsFirstOutCard is the first action deadline.
	MsFirstOutCard time.Duration
	// MsOutCard is the normal action deadline.
	MsOutCard time.Duration
	// MsOutCardRobot is the optional robot minimum delay.
	MsOutCardRobot time.Duration
	// MsAITimeout is the optional external AI hard deadline.
	MsAITimeout time.Duration
	// OfflineAutoUsesAI selects the external AI path for offline automation.
	OfflineAutoUsesAI bool
	// TimeoutAutoMove enables automatic action after timeout.
	TimeoutAutoMove bool
	// RobotLevel selects the process-level robot implementation.
	RobotLevel int
	// AutoSettlementMinCount is the minimum automatic-action count used to
	// assign settlement responsibility; -1 disables this condition.
	AutoSettlementMinCount int
	// AutoSettlementRatioFactor is the reference multiplier used with the
	// automatic-action count; -1 disables this condition.
	AutoSettlementRatioFactor int
	// SingleCountToSwap controls the ordinary single-card adjustment count.
	SingleCountToSwap int
}

// DefaultNHSKConfig returns the defaults used by the reference NHSK game.
func DefaultNHSKConfig() NHSKConfig {
	return NHSKConfig{
		MsFirstOutCard:            10 * time.Second,
		MsOutCard:                 10 * time.Second,
		MsOutCardRobot:            0,
		MsAITimeout:               0,
		TimeoutAutoMove:           true,
		RobotLevel:                2,
		AutoSettlementMinCount:    -1,
		AutoSettlementRatioFactor: -1,
		SingleCountToSwap:         4,
	}
}

func normalizeNHSKConfig(baseRule, gameRule string) NHSKConfig {
	config := DefaultNHSKConfig()
	base := splitRule(baseRule)
	if value, ok := ruleInt(base, 1); ok {
		config.OfflineAutoUsesAI = value != 0
	}
	if value, ok := ruleInt(base, 6); ok {
		config.TimeoutAutoMove = value > 0
	}
	if value, ok := ruleInt(base, 22); ok {
		config.RobotLevel = value
	}
	game := splitRule(gameRule)
	if value, ok := ruleInt(game, 0); ok {
		config.AutoSettlementMinCount = value
	}
	if value, ok := ruleInt(game, 1); ok {
		config.AutoSettlementRatioFactor = value
	}
	if value, ok := ruleInt(game, 3); ok {
		config.SingleCountToSwap = value
	}
	return config
}

func normalizeReplayRuleSnapshot(baseRule string) ReplayRuleSnapshot {
	base := splitRule(baseRule)
	var snapshot ReplayRuleSnapshot
	if value, ok := ruleInt(base, 49); ok {
		snapshot.TimeOutOver = value != 0
	}
	if value, ok := ruleInt(base, 38); ok {
		snapshot.VoiceMode = value != 0
	}
	if value, ok := ruleInt(base, 15); ok {
		snapshot.RandomSeatRoundStart = value > 0
	}
	if value, ok := ruleInt(base, 11); ok {
		snapshot.GameNumToRandomSeat = value
	}
	return snapshot
}

func splitRule(rule string) []string {
	if separator := strings.IndexByte(rule, ';'); separator >= 0 {
		rule = rule[:separator]
	}
	return strings.Split(rule, ",")
}

func ruleInt(values []string, index int) (int, bool) {
	if index < 0 || index >= len(values) {
		return 0, false
	}
	value, err := strconv.Atoi(strings.TrimSpace(values[index]))
	return value, err == nil
}

func (config NHSKConfig) outCardTimeout() time.Duration {
	if config.MsOutCard <= 0 {
		return DefaultNHSKConfig().MsOutCard
	}
	return config.MsOutCard
}

func (config NHSKConfig) firstOutCardTimeout() time.Duration {
	if config.MsFirstOutCard <= 0 {
		return config.outCardTimeout()
	}
	return config.MsFirstOutCard
}
