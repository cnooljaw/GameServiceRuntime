package control

import gsr "github.com/lijiawang/GameServiceRuntime/runtime"

// NewClient creates a typed ClusterControlService client.
func NewClient(caller CommandCaller, target gsr.ServiceRef) (*Client, error) {
	if isNil(caller) {
		return nil, ErrInvalidCaller
	}
	if !validAgent(target.Node, target) {
		return nil, ErrInvalidConfig
	}
	return &Client{caller: caller, target: target}, nil
}
