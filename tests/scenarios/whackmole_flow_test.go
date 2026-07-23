package scenarios

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestWhackMoleCompositionRootRunsOneKick(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "run", "../../examples/whackmole")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go run ./examples/whackmole error = %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "Hit:true") {
		t.Fatalf("example output = %q, want successful kick", output)
	}
}
