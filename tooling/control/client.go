package control

import (
	"context"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// NewClient creates a typed ClusterObserverService client.
func NewClient(caller CommandCaller, target gsr.ServiceRef) (*Client, error) {
	if isNil(caller) {
		return nil, ErrInvalidCaller
	}
	if !validAgent(target.Node, target) {
		return nil, ErrInvalidConfig
	}
	return &Client{caller: caller, target: target}, nil
}

// ListNodes returns sorted independent cached node details.
func (c *Client) ListNodes(ctx context.Context) ([]NodeDetail, error) {
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
	for _, detail := range response.Nodes {
		if !validDetail(detail) {
			return nil, ErrInvalidResponse
		}
	}
	return cloneNodeDetails(response.Nodes), nil
}

// GetNodeDetail returns one independent cached node detail without refreshing it.
func (c *Client) GetNodeDetail(ctx context.Context, node gsr.NodeID) (NodeDetail, error) {
	if !validNode(node) {
		return NodeDetail{}, ErrInvalidNode
	}
	value, err := c.caller.Call(ctx, c.target, commandGetNodeDetail, getNodeDetailRequest{Node: node})
	if err != nil {
		return NodeDetail{}, err
	}
	return nodeDetailFromResponse(value)
}

// RefreshNode refreshes one enabled node through its configured NodeAgentService.
func (c *Client) RefreshNode(ctx context.Context, node gsr.NodeID) (NodeDetail, error) {
	if !validNode(node) {
		return NodeDetail{}, ErrInvalidNode
	}
	value, err := c.caller.Call(ctx, c.target, commandRefreshNode, refreshNodeRequest{Node: node})
	if err != nil {
		return NodeDetail{}, err
	}
	return nodeDetailFromResponse(value)
}

func nodeDetailFromResponse(value any) (NodeDetail, error) {
	response, ok := value.(nodeDetailResponse)
	if !ok {
		return NodeDetail{}, ErrInvalidResponse
	}
	if err := errorFromCode(response.Error); err != nil {
		return NodeDetail{}, err
	}
	if !validDetail(response.Detail) {
		return NodeDetail{}, ErrInvalidResponse
	}
	return cloneNodeDetail(response.Detail), nil
}
