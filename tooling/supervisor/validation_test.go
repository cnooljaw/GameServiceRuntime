package supervisor

import (
	"context"
	"errors"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestValidateRestartPolicy(t *testing.T) {
	validRestart := RestartPolicy{
		Strategy: RestartOnFailure, MaxAttempts: 3, MaxRestarts: 2,
		Window: time.Minute, MinBackoff: time.Millisecond, MaxBackoff: time.Second,
	}
	tests := []struct {
		name   string
		policy RestartPolicy
		valid  bool
	}{
		{name: "restart", policy: validRestart, valid: true},
		{name: "never", policy: RestartPolicy{Strategy: RestartNever}, valid: true},
		{name: "destroy", policy: RestartPolicy{Strategy: DestroyOnFailure}, valid: true},
		{name: "unknown", policy: RestartPolicy{Strategy: RestartStrategy(99)}},
		{name: "never with limits", policy: RestartPolicy{Strategy: RestartNever, MaxAttempts: 1}},
		{name: "zero attempts", policy: replacePolicy(validRestart, func(p *RestartPolicy) { p.MaxAttempts = 0 })},
		{name: "zero restarts", policy: replacePolicy(validRestart, func(p *RestartPolicy) { p.MaxRestarts = 0 })},
		{name: "zero window", policy: replacePolicy(validRestart, func(p *RestartPolicy) { p.Window = 0 })},
		{name: "negative backoff", policy: replacePolicy(validRestart, func(p *RestartPolicy) { p.MinBackoff = -1 })},
		{name: "max below min", policy: replacePolicy(validRestart, func(p *RestartPolicy) { p.MinBackoff = time.Second; p.MaxBackoff = time.Millisecond })},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateRestartPolicy(test.policy)
			if test.valid && err != nil {
				t.Fatalf("validateRestartPolicy() error = %v", err)
			}
			if !test.valid && !errors.Is(err, ErrInvalidPolicy) {
				t.Fatalf("validateRestartPolicy() error = %v, want ErrInvalidPolicy", err)
			}
		})
	}
}

func TestDecorateRejectsInvalidServiceAndConfig(t *testing.T) {
	validConfig := DecoratorConfig{
		Key:        ServiceKey{Namespace: "player", ID: "42"},
		Generation: 1,
		Supervisor: gsr.ServiceRef{Node: "node-a", ID: 1},
	}
	var typedNil *recordingDecoratorService
	tests := []struct {
		name    string
		service gsr.Service
		config  DecoratorConfig
	}{
		{name: "nil", config: validConfig},
		{name: "dynamic nil", service: typedNil, config: validConfig},
		{name: "missing commands", service: noCommandsDecoratorService{}, config: validConfig},
		{name: "invalid key", service: &recordingDecoratorService{commands: []gsr.CommandID{1}}, config: replaceDecoratorConfig(validConfig, func(c *DecoratorConfig) { c.Key.ID = " " })},
		{name: "invalid utf8", service: &recordingDecoratorService{commands: []gsr.CommandID{1}}, config: replaceDecoratorConfig(validConfig, func(c *DecoratorConfig) { c.Key.ID = string([]byte{0xff}) })},
		{name: "zero generation", service: &recordingDecoratorService{commands: []gsr.CommandID{1}}, config: replaceDecoratorConfig(validConfig, func(c *DecoratorConfig) { c.Generation = 0 })},
		{name: "zero supervisor", service: &recordingDecoratorService{commands: []gsr.CommandID{1}}, config: replaceDecoratorConfig(validConfig, func(c *DecoratorConfig) { c.Supervisor = gsr.ServiceRef{} })},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Decorate(test.service, test.config); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("Decorate() error = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

type noCommandsDecoratorService struct{}

func (noCommandsDecoratorService) Init(gsr.ServiceContext) error                { return nil }
func (noCommandsDecoratorService) Handle(gsr.CommandContext, gsr.Command) error { return nil }
func (noCommandsDecoratorService) Stop(context.Context) error                   { return nil }
func (noCommandsDecoratorService) Close() error                                 { return nil }

func replacePolicy(policy RestartPolicy, change func(*RestartPolicy)) RestartPolicy {
	change(&policy)
	return policy
}

func replaceDecoratorConfig(config DecoratorConfig, change func(*DecoratorConfig)) DecoratorConfig {
	change(&config)
	return config
}
