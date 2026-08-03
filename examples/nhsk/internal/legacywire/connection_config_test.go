package legacywire

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestDefaultConnectionConfigMatchesRFC(t *testing.T) {
	config := DefaultConnectionConfig()
	if config.DialTimeout != 5*time.Second ||
		config.OriginTimeout != 5*time.Second ||
		config.InitialBackoff != time.Second ||
		config.MaxBackoff != 30*time.Second ||
		config.BackoffMultiplier != 2 ||
		config.JitterRatio != 0.2 ||
		config.StableResetAfter != 60*time.Second {
		t.Fatalf("default connection config = %+v", config)
	}
}

func TestBackoffPolicyUsesCappedExponentialSequence(t *testing.T) {
	policy, err := newBackoffPolicy(DefaultConnectionConfig(), func() float64 { return 0.5 })
	if err != nil {
		t.Fatalf("new backoff policy: %v", err)
	}
	want := []time.Duration{
		time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		30 * time.Second,
		30 * time.Second,
	}
	for index, duration := range want {
		if got := policy.next(); got != duration {
			t.Fatalf("backoff[%d] = %v, want %v", index, got, duration)
		}
	}
}

func TestBackoffPolicyAppliesJitterBounds(t *testing.T) {
	tests := []struct {
		name   string
		source float64
		want   time.Duration
	}{
		{name: "lower", source: 0, want: 800 * time.Millisecond},
		{name: "middle", source: 0.5, want: time.Second},
		{name: "upper", source: 1, want: 1200 * time.Millisecond},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy, err := newBackoffPolicy(DefaultConnectionConfig(), func() float64 { return test.source })
			if err != nil {
				t.Fatalf("new backoff policy: %v", err)
			}
			if got := policy.next(); got != test.want {
				t.Fatalf("jittered backoff = %v, want %v", got, test.want)
			}
		})
	}
}

func TestBackoffPolicyResetsOnlyAfterStableReadyPeriod(t *testing.T) {
	config := DefaultConnectionConfig()
	policy, err := newBackoffPolicy(config, func() float64 { return 0.5 })
	if err != nil {
		t.Fatalf("new backoff policy: %v", err)
	}
	_ = policy.next()
	_ = policy.next()

	policy.resetIfStable(config.StableResetAfter - time.Nanosecond)
	if got := policy.next(); got != 4*time.Second {
		t.Fatalf("backoff before stable reset = %v, want 4s", got)
	}
	policy.resetIfStable(config.StableResetAfter)
	if got := policy.next(); got != time.Second {
		t.Fatalf("backoff after stable reset = %v, want 1s", got)
	}
}

func TestNewBackoffPolicyRejectsInvalidConnectionConfig(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ConnectionConfig)
		field  string
	}{
		{name: "dial timeout", mutate: func(config *ConnectionConfig) { config.DialTimeout = 0 }, field: "dial timeout"},
		{name: "origin timeout", mutate: func(config *ConnectionConfig) { config.OriginTimeout = 0 }, field: "origin timeout"},
		{name: "initial backoff", mutate: func(config *ConnectionConfig) { config.InitialBackoff = 0 }, field: "initial backoff"},
		{name: "maximum backoff", mutate: func(config *ConnectionConfig) { config.MaxBackoff = 0 }, field: "maximum backoff"},
		{name: "backoff order", mutate: func(config *ConnectionConfig) { config.InitialBackoff = config.MaxBackoff + 1 }, field: "initial backoff"},
		{name: "multiplier", mutate: func(config *ConnectionConfig) { config.BackoffMultiplier = 1 }, field: "backoff multiplier"},
		{name: "multiplier NaN", mutate: func(config *ConnectionConfig) { config.BackoffMultiplier = math.NaN() }, field: "backoff multiplier"},
		{name: "jitter zero", mutate: func(config *ConnectionConfig) { config.JitterRatio = 0 }, field: "jitter ratio"},
		{name: "jitter one", mutate: func(config *ConnectionConfig) { config.JitterRatio = 1 }, field: "jitter ratio"},
		{name: "jitter infinity", mutate: func(config *ConnectionConfig) { config.JitterRatio = math.Inf(1) }, field: "jitter ratio"},
		{name: "stable reset", mutate: func(config *ConnectionConfig) { config.StableResetAfter = 0 }, field: "stable reset"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := DefaultConnectionConfig()
			test.mutate(&config)
			_, err := newBackoffPolicy(config, func() float64 { return 0.5 })
			if err == nil || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("error = %v, want field %q", err, test.field)
			}
		})
	}
}

func TestNewBackoffPolicyRequiresRandomSource(t *testing.T) {
	if _, err := newBackoffPolicy(DefaultConnectionConfig(), nil); err == nil {
		t.Fatal("backoff policy without random source succeeded")
	}
}
