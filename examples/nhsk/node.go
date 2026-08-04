package nhsk

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

type nodeReadiness string

const (
	nodeNotReady nodeReadiness = "not_ready"
	nodeReady    nodeReadiness = "ready"
	nodeDegraded nodeReadiness = "degraded"
)

type nodeHealthSnapshot struct {
	GMLinkReady        bool
	QuarantinedBattles int
}

type nodeHealthSource interface {
	NodeHealth() nodeHealthSnapshot
}

type closeOwner interface {
	Close(context.Context) error
}

type nodeRuntime interface {
	Stop(context.Context, gsr.ServiceRef) error
	Close(context.Context) error
}

type nodeShutdownStatus struct {
	Closing      bool
	Closed       bool
	CurrentOwner string
	FailedOwners []string
}

type gameLogicNode struct {
	shutdownTimeout time.Duration
	health          nodeHealthSource
	connection      closeOwner
	factory         closeOwner
	runtime         nodeRuntime
	services        []gsr.ServiceRef

	closeOnce sync.Once
	closeErr  error
	mu        sync.Mutex
	shutdown  nodeShutdownStatus
}

func newGameLogicNode(
	shutdownTimeout time.Duration,
	health nodeHealthSource,
	connection closeOwner,
	factory closeOwner,
	runtime nodeRuntime,
	services []gsr.ServiceRef,
) (*gameLogicNode, error) {
	if shutdownTimeout <= 0 || health == nil || connection == nil || factory == nil || runtime == nil {
		return nil, fmt.Errorf("nhsk node: invalid dependency")
	}
	for _, ref := range services {
		if ref.Node == "" || ref.ID == 0 {
			return nil, fmt.Errorf("nhsk node: invalid root service ref")
		}
	}
	return &gameLogicNode{
		shutdownTimeout: shutdownTimeout,
		health:          health,
		connection:      connection,
		factory:         factory,
		runtime:         runtime,
		services:        append([]gsr.ServiceRef(nil), services...),
	}, nil
}

func (node *gameLogicNode) readiness() nodeReadiness {
	node.mu.Lock()
	stopping := node.shutdown.Closing || node.shutdown.Closed
	node.mu.Unlock()
	if stopping {
		return nodeNotReady
	}
	health := node.health.NodeHealth()
	if !health.GMLinkReady {
		return nodeNotReady
	}
	if health.QuarantinedBattles > 0 {
		return nodeDegraded
	}
	return nodeReady
}

func (node *gameLogicNode) Close(parent context.Context) error {
	node.closeOnce.Do(func() {
		if parent == nil {
			parent = context.Background()
		}
		ctx, cancel := context.WithTimeout(parent, node.shutdownTimeout)
		defer cancel()
		node.closeErr = node.close(ctx)
	})
	return node.closeErr
}

func (node *gameLogicNode) close(ctx context.Context) error {
	node.mu.Lock()
	node.shutdown.Closing = true
	node.mu.Unlock()

	var failures []error
	appendFailure := func(owner string, action func(context.Context) error) {
		if err := node.closeOwner(ctx, owner, action); err != nil {
			failures = append(failures, err)
		}
	}
	appendFailure("connection", node.connection.Close)
	appendFailure("factory", node.factory.Close)
	for index := len(node.services) - 1; index >= 0; index-- {
		ref := node.services[index]
		owner := "service:" + string(ref.Node) + "/" + serviceIDText(ref.ID)
		appendFailure(owner, func(ctx context.Context) error { return node.runtime.Stop(ctx, ref) })
	}
	appendFailure("runtime", node.runtime.Close)

	node.mu.Lock()
	node.shutdown.Closing = false
	node.shutdown.Closed = true
	node.shutdown.CurrentOwner = ""
	node.mu.Unlock()
	return errors.Join(failures...)
}

func (node *gameLogicNode) closeOwner(ctx context.Context, owner string, action func(context.Context) error) error {
	node.mu.Lock()
	node.shutdown.CurrentOwner = owner
	node.mu.Unlock()

	err := action(ctx)
	node.mu.Lock()
	node.shutdown.CurrentOwner = ""
	if err != nil {
		node.shutdown.FailedOwners = append(node.shutdown.FailedOwners, owner)
	}
	node.mu.Unlock()
	if err != nil {
		return fmt.Errorf("nhsk node: close %s: %w", owner, err)
	}
	return nil
}

func (node *gameLogicNode) shutdownStatus() nodeShutdownStatus {
	node.mu.Lock()
	defer node.mu.Unlock()
	status := node.shutdown
	status.FailedOwners = append([]string(nil), node.shutdown.FailedOwners...)
	return status
}

func serviceIDText(id gsr.ServiceID) string {
	return strconv.FormatUint(uint64(id), 10)
}
