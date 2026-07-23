package control

import (
	"context"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const defaultCallTimeout = 3 * time.Second

type controlService struct {
	config ObserverConfig
}

// NewClusterControlService creates a ClusterControlService with frozen static desired nodes.
func NewClusterObserverService(config ObserverConfig) (gsr.Service, error) {
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
		if _, exists := seen[target.Config.ID]; exists {
			return nil, ErrInvalidConfig
		}
		seen[target.Config.ID] = struct{}{}
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
