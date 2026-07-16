package gsr_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceGoroutinePolicyDetectsDirectGoStatements(t *testing.T) {
	source := `package sample

type badService struct{}

func (*badService) Init(any) error { return nil }
func (*badService) Handle(any, any) error {
	return nil
}
func (*badService) start() {
	go func() {}()
}
func (*badService) Stop(any) error { return nil }
func (*badService) Close() error { return nil }

type runtime struct{}

func (*runtime) Close() error {
	go func() {}()
	return nil
}
`
	violations, err := serviceGoroutineViolationsFromSource("policy.go", source)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 {
		t.Fatalf("violations = %v, want one Service violation", violations)
	}
	if violation := violations[0]; violation.receiver != "badService" || violation.method != "start" {
		t.Fatalf("violation = %+v", violation)
	}
}

func TestProjectServicesDoNotStartGoroutines(t *testing.T) {
	repositoryRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	violations, err := scanServiceGoroutineViolations(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) == 0 {
		return
	}
	lines := make([]string, 0, len(violations))
	for _, violation := range violations {
		lines = append(lines, violation.String())
	}
	t.Fatalf("Service implementations must not start goroutines:\n%s", strings.Join(lines, "\n"))
}
