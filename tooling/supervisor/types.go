// Package supervisor provides bounded Service failure detection and recovery orchestration.
package supervisor

import (
	"context"
	"log/slog"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const (
	metricFailureNotices              = "supervisor_failure_notices_total"
	metricFailureNoticeDeliveryErrors = "supervisor_failure_notice_delivery_errors_total"
	metricFailureNoticesDuplicate     = "supervisor_failure_notices_duplicate_total"
	metricFailureNoticesStale         = "supervisor_failure_notices_stale_total"
	metricRestartsScheduled           = "supervisor_restarts_scheduled_total"
	metricRestartsSucceeded           = "supervisor_restarts_succeeded_total"
	metricRestartsFailed              = "supervisor_restarts_failed_total"
	metricRestartsSuppressed          = "supervisor_restarts_suppressed_total"
)

// ServiceKey identifies one logical Service across instance changes.
type ServiceKey struct {
	Namespace string
	ID        string
}

// FailureKind classifies a Service failure without exposing panic contents.
type FailureKind uint8

const (
	// FailureHandlerPanic indicates that Service.Handle panicked.
	FailureHandlerPanic FailureKind = iota + 1
)

// FailureNotice is an immutable fact emitted before Core isolates a failed Service.
type FailureNotice struct {
	Key        ServiceKey
	FailedRef  gsr.ServiceRef
	Generation uint64
	OccurredAt time.Time
	Kind       FailureKind
}

// DecoratorConfig binds a Service instance to one Supervisor registration generation.
type DecoratorConfig struct {
	Key        ServiceKey
	Generation uint64
	Supervisor gsr.ServiceRef
}

// RestartStrategy selects the terminal or recovery action for Handler panic.
type RestartStrategy uint8

const (
	// RestartNever stops automatic recovery and preserves a stopped failure state.
	RestartNever RestartStrategy = iota
	// RestartOnFailure prepares and publishes a new Service instance within policy limits.
	RestartOnFailure
	// DestroyOnFailure records intentional destruction without preparing a replacement.
	DestroyOnFailure
)

// RestartPolicy bounds recovery attempts, successful restarts, and backoff.
type RestartPolicy struct {
	Strategy    RestartStrategy
	MaxAttempts int
	MaxRestarts int
	Window      time.Duration
	MinBackoff  time.Duration
	MaxBackoff  time.Duration
}

// Registration binds a stable ServiceKey to its current committed instance and policy.
type Registration struct {
	Key        ServiceKey
	Ref        gsr.ServiceRef
	Generation uint64
	Policy     RestartPolicy
}

// ServiceStatus describes Supervisor state for one registered logical Service.
type ServiceStatus uint8

const (
	// ServiceRunning indicates that Registration.Ref is the committed current instance.
	ServiceRunning ServiceStatus = iota + 1
	// ServiceRestartStopped indicates RestartNever handled the current failure.
	ServiceRestartStopped
	// ServiceDestroyed indicates DestroyOnFailure handled the current failure.
	ServiceDestroyed
	// ServiceBackoff indicates a recovery task is waiting before Prepare.
	ServiceBackoff
	// ServicePreparing indicates Runner is preparing a replacement.
	ServicePreparing
	// ServicePublishing indicates a prepared replacement is awaiting binding publication.
	ServicePublishing
	// ServiceRecoveryFailed indicates automatic recovery ended with a terminal failure.
	ServiceRecoveryFailed
	// ServiceRestartSuppressed indicates the restart window rejected another recovery.
	ServiceRestartSuppressed
)

// RecoveryFailure is a stable failure category stored by Supervisor state.
type RecoveryFailure uint8

const (
	// RecoveryFailureNone indicates no recorded recovery failure.
	RecoveryFailureNone RecoveryFailure = iota
	// RecoveryFailureSnapshotNotFound indicates no committed recovery state exists.
	RecoveryFailureSnapshotNotFound
	// RecoveryFailurePrepare indicates factory or preparation failure.
	RecoveryFailurePrepare
	// RecoveryFailureCreate indicates Runtime could not create the replacement.
	RecoveryFailureCreate
	// RecoveryFailurePublish indicates long-lived binding publication failed.
	RecoveryFailurePublish
	// RecoveryFailureAbort indicates cleanup could not converge after a failed attempt.
	RecoveryFailureAbort
	// RecoveryFailureQueueFull indicates Runner rejected a bounded task submission.
	RecoveryFailureQueueFull
	// RecoveryFailureSuppressed indicates policy stopped another recovery.
	RecoveryFailureSuppressed
)

// Record is an independent Supervisor status snapshot for one ServiceKey.
type Record struct {
	Registration     Registration
	Status           ServiceStatus
	Attempt          uint64
	AttemptsInFault  int
	RestartsInWindow int
	LastFailure      RecoveryFailure
}

// RecoveryTask asks a non-Service Runner to perform one delayed recovery attempt.
type RecoveryTask struct {
	Supervisor gsr.ServiceRef
	Key        ServiceKey
	FailedRef  gsr.ServiceRef
	Generation uint64
	Attempt    uint64
	Delay      time.Duration
}

// RecoveryExecutor accepts bounded recovery work without blocking a Service Handler.
type RecoveryExecutor interface {
	Submit(RecoveryTask) error
}

// CommandCaller is the narrow Runtime capability required by Client and Runner.
type CommandCaller interface {
	Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error)
}

// RunnerConfig configures bounded recovery execution and result delivery.
type RunnerConfig struct {
	Workers             int
	QueueSize           int
	AttemptTimeout      time.Duration
	ResultTimeout       time.Duration
	ResultRetryInterval time.Duration
	Logger              *slog.Logger
}

// LaunchRequest identifies one recovery attempt and its target generation.
type LaunchRequest struct {
	Supervisor gsr.ServiceRef
	Key        ServiceKey
	FailedRef  gsr.ServiceRef
	Generation uint64
	Attempt    uint64
}

// Launcher prepares, publishes, and aborts replacement Service instances.
type Launcher interface {
	Prepare(context.Context, LaunchRequest) (gsr.ServiceRef, error)
	Commit(context.Context, LaunchRequest, gsr.ServiceRef) error
	Abort(context.Context, LaunchRequest, gsr.ServiceRef) error
}

// RuntimeControl is the narrow Core lifecycle capability required by RuntimeLauncher.
type RuntimeControl interface {
	CreateService(gsr.ServiceSpec) (gsr.ServiceRef, error)
	Stop(context.Context, gsr.ServiceRef) error
}

// ServiceFactory constructs an unregistered ServiceSpec for one recovery generation.
type ServiceFactory interface {
	Build(context.Context, ServiceKey, uint64) (gsr.ServiceSpec, error)
}

// ServiceFactoryFunc adapts a function to ServiceFactory.
type ServiceFactoryFunc func(context.Context, ServiceKey, uint64) (gsr.ServiceSpec, error)

// Build calls f.
func (f ServiceFactoryFunc) Build(ctx context.Context, key ServiceKey, generation uint64) (gsr.ServiceSpec, error) {
	return f(ctx, key, generation)
}

// BindingPublisher publishes and conditionally withdraws long-lived bindings.
type BindingPublisher interface {
	Publish(context.Context, ServiceKey, gsr.ServiceRef) error
	Withdraw(context.Context, ServiceKey, gsr.ServiceRef) error
}
