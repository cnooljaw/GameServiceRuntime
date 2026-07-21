package discovery

import (
	"context"
	"strings"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// CommandCaller is the narrow Runtime capability required by Client.
type CommandCaller interface {
	Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error)
}

// Client provides typed access to one DiscoveryService.
type Client struct {
	caller CommandCaller
	target gsr.ServiceRef
}

// NewClient binds a typed Discovery Client to a known bootstrap ServiceRef.
func NewClient(caller CommandCaller, target gsr.ServiceRef) (*Client, error) {
	if caller == nil || target.Node == "" || target.ID == 0 {
		return nil, ErrInvalidConfig
	}
	return &Client{caller: caller, target: target}, nil
}

// RegisterNode creates a new lease generation for node.
func (c *Client) RegisterNode(ctx context.Context, node gsr.NodeID, address string) (NodeLease, error) {
	if node == "" || strings.TrimSpace(address) == "" {
		return NodeLease{}, ErrInvalidNode
	}
	value, err := c.caller.Call(ctx, c.target, commandRegisterNode, registerNodeRequest{Node: node, Address: address})
	if err != nil {
		return NodeLease{}, err
	}
	response, ok := value.(leaseResponse)
	if !ok {
		return NodeLease{}, ErrInvalidResponse
	}
	if err := errorFromCode(response.Error); err != nil {
		return NodeLease{}, err
	}
	return response.Lease, nil
}

// Heartbeat renews a current node lease.
func (c *Client) Heartbeat(ctx context.Context, lease NodeLease) (NodeLease, error) {
	if !validLease(lease) {
		return NodeLease{}, ErrInvalidNode
	}
	value, err := c.caller.Call(ctx, c.target, commandHeartbeat, heartbeatRequest{Lease: lease})
	if err != nil {
		return NodeLease{}, err
	}
	response, ok := value.(leaseResponse)
	if !ok {
		return NodeLease{}, ErrInvalidResponse
	}
	if err := errorFromCode(response.Error); err != nil {
		return NodeLease{}, err
	}
	return response.Lease, nil
}

// UnregisterNode removes a node only when lease is the current generation.
func (c *Client) UnregisterNode(ctx context.Context, lease NodeLease) error {
	if !validLease(lease) {
		return ErrInvalidNode
	}
	value, err := c.caller.Call(ctx, c.target, commandUnregisterNode, unregisterNodeRequest{Lease: lease})
	if err != nil {
		return err
	}
	response, ok := value.(emptyResponse)
	if !ok {
		return ErrInvalidResponse
	}
	return errorFromCode(response.Error)
}

// GetNode returns one active node record.
func (c *Client) GetNode(ctx context.Context, node gsr.NodeID) (NodeRecord, error) {
	if node == "" {
		return NodeRecord{}, ErrInvalidNode
	}
	value, err := c.caller.Call(ctx, c.target, commandGetNode, getNodeRequest{Node: node})
	if err != nil {
		return NodeRecord{}, err
	}
	response, ok := value.(nodeResponse)
	if !ok {
		return NodeRecord{}, ErrInvalidResponse
	}
	if err := errorFromCode(response.Error); err != nil {
		return NodeRecord{}, err
	}
	return response.Node, nil
}

// ListNodes returns active node records sorted by NodeID.
func (c *Client) ListNodes(ctx context.Context) ([]NodeRecord, error) {
	value, err := c.caller.Call(ctx, c.target, commandListNodes, listNodesRequest{})
	if err != nil {
		return nil, err
	}
	response, ok := value.(nodesResponse)
	if !ok {
		return nil, ErrInvalidResponse
	}
	if err := errorFromCode(response.Error); err != nil {
		return nil, err
	}
	return append([]NodeRecord(nil), response.Nodes...), nil
}

func validLease(lease NodeLease) bool {
	return lease.Node != "" && lease.Generation != 0
}
