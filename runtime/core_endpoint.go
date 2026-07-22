package gsr

import (
	"context"
	"encoding/binary"
)

const (
	coreResolveNameCommand   CommandID = ^CommandID(0)
	maxCoreServiceNameLength           = 1024
)

// ResolveRemote resolves a ServiceName in one known node's local registry.
func (r *Runtime) ResolveRemote(ctx context.Context, node NodeID, name ServiceName) (ServiceRef, error) {
	if node == r.node {
		return r.Resolve(name)
	}
	if node == "" {
		return ServiceRef{}, ErrRemoteUnavailable
	}
	target := ServiceRef{Node: node}
	value, err := r.call(ctx, ServiceRef{}, target, coreResolveNameCommand, name, []ServiceRef{target})
	if err != nil {
		return ServiceRef{}, err
	}
	ref, ok := value.(ServiceRef)
	if !ok || ref.Node != node || ref.ID == 0 {
		return ServiceRef{}, ErrInvalidClusterEnvelope
	}
	return ref, nil
}

func encodeCoreResolveName(value any) ([]byte, error) {
	name, ok := value.(ServiceName)
	if !ok || len(name) > maxCoreServiceNameLength {
		return nil, ErrInvalidClusterEnvelope
	}
	return []byte(name), nil
}

func decodeCoreResolveName(payload []byte) (ServiceName, error) {
	if len(payload) > maxCoreServiceNameLength {
		return "", ErrInvalidClusterEnvelope
	}
	return ServiceName(payload), nil
}

func encodeCoreResolveResponse(value any, node NodeID) ([]byte, error) {
	ref, ok := value.(ServiceRef)
	if !ok || ref.Node != node || ref.ID == 0 {
		return nil, ErrInvalidClusterEnvelope
	}
	payload := make([]byte, 8)
	binary.BigEndian.PutUint64(payload, uint64(ref.ID))
	return payload, nil
}

func decodeCoreResolveResponse(payload []byte, node NodeID) (ServiceRef, error) {
	if len(payload) != 8 {
		return ServiceRef{}, ErrInvalidClusterEnvelope
	}
	id := ServiceID(binary.BigEndian.Uint64(payload))
	if id == 0 {
		return ServiceRef{}, ErrInvalidClusterEnvelope
	}
	return ServiceRef{Node: node, ID: id}, nil
}
