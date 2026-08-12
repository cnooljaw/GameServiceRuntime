package nhsk

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeNHSKConfigUsesReferenceDefaultsAndReachableRules(t *testing.T) {
	base := make([]string, 23)
	base[1] = "1"
	base[6] = "0"
	base[22] = "1"
	game := "2,50,1,3;ignored"

	got := normalizeNHSKConfig(strings.Join(base, ","), game)
	if got.MsOutCard != 10*time.Second || got.MsFirstOutCard != 10*time.Second {
		t.Fatalf("timeouts = %#v, want 10 seconds", got)
	}
	if !got.OfflineAutoUsesAI || got.TimeoutAutoMove || got.RobotLevel != 1 {
		t.Fatalf("base rule projection = %#v", got)
	}
	if got.MinRobotOutCardCount != 2 || got.MinRobotOutCardRatio != 50 || got.SingleCountToSwap != 3 {
		t.Fatalf("game rule projection = %#v", got)
	}
}

func TestNormalizeNHSKConfigIgnoresMissingAndMalformedValues(t *testing.T) {
	got := normalizeNHSKConfig("broken,broken,broken,broken,broken,broken,broken", "bad,also-bad,1,bad")
	want := DefaultNHSKConfig()
	if got != want {
		t.Fatalf("malformed rules = %#v, want defaults %#v", got, want)
	}
}

func TestNormalizeNHSKConfigIgnoresGameRulePrefixAfterSemicolon(t *testing.T) {
	got := normalizeNHSKConfig("", "2,50,1,3;legacy-extra")
	if got.MinRobotOutCardCount != 2 || got.MinRobotOutCardRatio != 50 || got.SingleCountToSwap != 3 {
		t.Fatalf("game rule = %#v", got)
	}
}
