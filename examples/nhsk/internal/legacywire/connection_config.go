package legacywire

import (
	"errors"
	"math"
	"time"
)

// ConnectionConfig controls one Legacy GameMaster connection owner's timeouts
// and reconnect backoff policy.
type ConnectionConfig struct {
	DialTimeout       time.Duration
	OriginTimeout     time.Duration
	InitialBackoff    time.Duration
	MaxBackoff        time.Duration
	BackoffMultiplier float64
	JitterRatio       float64
	StableResetAfter  time.Duration
}

// DefaultConnectionConfig returns the RFC-0410 Legacy connection defaults.
func DefaultConnectionConfig() ConnectionConfig {
	return ConnectionConfig{
		DialTimeout:       5 * time.Second,
		OriginTimeout:     5 * time.Second,
		InitialBackoff:    time.Second,
		MaxBackoff:        30 * time.Second,
		BackoffMultiplier: 2,
		JitterRatio:       0.2,
		StableResetAfter:  60 * time.Second,
	}
}

func (config ConnectionConfig) validate() error {
	if config.DialTimeout <= 0 {
		return errors.New("legacywire: dial timeout must be positive")
	}
	if config.OriginTimeout <= 0 {
		return errors.New("legacywire: origin timeout must be positive")
	}
	if config.InitialBackoff <= 0 {
		return errors.New("legacywire: initial backoff must be positive")
	}
	if config.MaxBackoff <= 0 {
		return errors.New("legacywire: maximum backoff must be positive")
	}
	if config.InitialBackoff > config.MaxBackoff {
		return errors.New("legacywire: initial backoff must not exceed maximum backoff")
	}
	if math.IsNaN(config.BackoffMultiplier) || math.IsInf(config.BackoffMultiplier, 0) || config.BackoffMultiplier <= 1 {
		return errors.New("legacywire: backoff multiplier must be greater than one")
	}
	if math.IsNaN(config.JitterRatio) || math.IsInf(config.JitterRatio, 0) || config.JitterRatio <= 0 || config.JitterRatio >= 1 {
		return errors.New("legacywire: jitter ratio must be greater than zero and less than one")
	}
	if config.StableResetAfter <= 0 {
		return errors.New("legacywire: stable reset must be positive")
	}
	return nil
}

type backoffPolicy struct {
	config ConnectionConfig
	base   time.Duration
	random func() float64
}

func newBackoffPolicy(config ConnectionConfig, random func() float64) (*backoffPolicy, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	if random == nil {
		return nil, errors.New("legacywire: backoff random source is required")
	}
	return &backoffPolicy{config: config, base: config.InitialBackoff, random: random}, nil
}

func (policy *backoffPolicy) next() time.Duration {
	random := policy.random()
	if random < 0 {
		random = 0
	} else if random > 1 {
		random = 1
	}
	factor := 1 - policy.config.JitterRatio + 2*policy.config.JitterRatio*random
	wait := time.Duration(float64(policy.base) * factor)

	nextBase := float64(policy.base) * policy.config.BackoffMultiplier
	if nextBase >= float64(policy.config.MaxBackoff) {
		policy.base = policy.config.MaxBackoff
	} else {
		policy.base = time.Duration(nextBase)
	}
	return wait
}

func (policy *backoffPolicy) resetIfStable(readyFor time.Duration) {
	if readyFor >= policy.config.StableResetAfter {
		policy.base = policy.config.InitialBackoff
	}
}
