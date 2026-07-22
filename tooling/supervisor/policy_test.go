package supervisor

import (
	"testing"
	"time"
)

func TestRestartBackoffIsExponentialAndSaturates(t *testing.T) {
	policy := RestartPolicy{MinBackoff: 10 * time.Millisecond, MaxBackoff: 25 * time.Millisecond}
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 1, want: 10 * time.Millisecond},
		{attempt: 2, want: 20 * time.Millisecond},
		{attempt: 3, want: 25 * time.Millisecond},
		{attempt: 1000, want: 25 * time.Millisecond},
	}
	for _, test := range tests {
		if got := restartBackoff(policy, test.attempt); got != test.want {
			t.Fatalf("restartBackoff(attempt=%d) = %v, want %v", test.attempt, got, test.want)
		}
	}
}
