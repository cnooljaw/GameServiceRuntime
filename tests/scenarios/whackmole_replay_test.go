package scenarios

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestWhackMoleReplayScenario(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "test", "../../examples/whackmole", "-run", "^TestWhackMoleRecordReplayReproducesScoreInIsolatedRuntime$", "-count=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("WhackMole replay scenario error = %v\n%s", err, output)
	}
}
