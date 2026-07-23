package control

import (
	"context"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const defaultCallTimeout = 3 * time.Second

type controlService struct {
	config ControlConfig
}

// NewClusterControlService creates a ClusterControlService with frozen static desired nodes.
func NewClusterControlService(config ControlConfig) (gsr.Service, error) {
	if config.CallTimeout < 0 {
		return nil, ErrInvalidConfig
	}
	if config.CallTimeout == 0 {
		config.CallTimeout = defaultCallTimeout
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	seen := make(map[gsr.NodeID]struct{}, len(config.Nodes))
	config.Nodes = append([]NodeTarget(nil), config.Nodes...)
	for _, target := range config.Nodes {
		if !validTarget(target) {
			return nil, ErrInvalidConfig
		}
		if _, exists := seen[target.Desired.ID]; exists {
			return nil, ErrInvalidConfig
		}
		seen[target.Desired.ID] = struct{}{}
	}
	return &controlService{config: config}, nil
}

func (*controlService) Commands() []gsr.CommandID     { return nil }
func (*controlService) Init(gsr.ServiceContext) error { return nil }
func (*controlService) Handle(gsr.CommandContext, gsr.Command) error {
	return gsr.ErrCommandNotRegistered
}
func (*controlService) Stop(context.Context) error { return nil }
func (*controlService) Close() error               { return nil }
