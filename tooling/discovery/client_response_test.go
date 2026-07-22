package discovery

import (
	"context"
	"errors"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestClientRejectsSemanticallyInvalidSuccessResponses(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	lease := NodeLease{Node: "node-a", AuthorityEpoch: 1, Generation: 1, ExpiresAt: now.Add(time.Minute)}
	recordA := NodeRecord{ID: "node-a", Address: "node-a:9000", AuthorityEpoch: 1, Generation: 1, LastSeen: now, ExpiresAt: now.Add(time.Minute)}
	recordB := NodeRecord{ID: "node-b", Address: "node-b:9000", AuthorityEpoch: 1, Generation: 2, LastSeen: now, ExpiresAt: now.Add(time.Minute)}

	tests := []struct {
		name     string
		response any
		invoke   func(*Client) error
	}{
		{
			name:     "register node zero lease",
			response: leaseResponse{},
			invoke: func(client *Client) error {
				_, err := client.RegisterNode(context.Background(), "node-a", "node-a:9000")
				return err
			},
		},
		{
			name:     "register node mismatched lease",
			response: leaseResponse{Lease: NodeLease{Node: "node-b", AuthorityEpoch: 1, Generation: 1, ExpiresAt: now.Add(time.Minute)}},
			invoke: func(client *Client) error {
				_, err := client.RegisterNode(context.Background(), "node-a", "node-a:9000")
				return err
			},
		},
		{
			name:     "heartbeat changed identity",
			response: leaseResponse{Lease: NodeLease{Node: "node-a", AuthorityEpoch: 2, Generation: 1, ExpiresAt: now.Add(time.Minute)}},
			invoke: func(client *Client) error {
				_, err := client.Heartbeat(context.Background(), lease)
				return err
			},
		},
		{
			name:     "get node zero record",
			response: nodeResponse{},
			invoke: func(client *Client) error {
				_, err := client.GetNode(context.Background(), "node-a")
				return err
			},
		},
		{
			name:     "get node mismatched record",
			response: nodeResponse{Node: recordB},
			invoke: func(client *Client) error {
				_, err := client.GetNode(context.Background(), "node-a")
				return err
			},
		},
		{
			name:     "list nodes invalid element",
			response: nodesResponse{Nodes: []NodeRecord{{ID: "node-a"}}},
			invoke: func(client *Client) error {
				_, err := client.ListNodes(context.Background())
				return err
			},
		},
		{
			name:     "list nodes not sorted",
			response: nodesResponse{Nodes: []NodeRecord{recordB, recordA}},
			invoke: func(client *Client) error {
				_, err := client.ListNodes(context.Background())
				return err
			},
		},
		{
			name:     "resolve name zero ref",
			response: refResponse{},
			invoke: func(client *Client) error {
				_, err := client.ResolveName(context.Background(), ".config")
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newStaticResponseClient(t, test.response)
			if err := test.invoke(client); !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("error = %v, want ErrInvalidResponse", err)
			}
		})
	}
}

func TestClientAllowsZeroDataWhenResponseContainsDomainError(t *testing.T) {
	client := newStaticResponseClient(t, leaseResponse{Error: errorLeaseExpired})
	lease := NodeLease{Node: "node-a", AuthorityEpoch: 1, Generation: 1}
	if _, err := client.Heartbeat(context.Background(), lease); !errors.Is(err, ErrLeaseExpired) {
		t.Fatalf("Heartbeat error = %v, want ErrLeaseExpired", err)
	}
}

type staticResponseCaller struct {
	response any
}

func (c staticResponseCaller) Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error) {
	return c.response, nil
}

func newStaticResponseClient(t *testing.T, response any) *Client {
	t.Helper()
	client, err := NewClient(staticResponseCaller{response: response}, gsr.ServiceRef{Node: "discovery", ID: 1})
	if err != nil {
		t.Fatal(err)
	}
	return client
}
