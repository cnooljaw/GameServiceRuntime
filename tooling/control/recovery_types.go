package control

import (
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/servicegroup"
)

// BlueprintID identifies one composition-root-registered replacement Service factory.
type BlueprintID string

// RecoveryTargetState describes one target's manual replacement lifecycle.
type RecoveryTargetState string

const (
	// RecoveryTargetPending means the target has not been submitted to a local runner.
	RecoveryTargetPending RecoveryTargetState = "pending"
	// RecoveryTargetCreating means a local runner accepted the replacement task.
	RecoveryTargetCreating RecoveryTargetState = "creating"
	// RecoveryTargetCreated means a new, not-yet-published ServiceRef exists.
	RecoveryTargetCreated RecoveryTargetState = "created"
	// RecoveryTargetPublished means the Directory publish made the replacement routable.
	RecoveryTargetPublished RecoveryTargetState = "published"
	// RecoveryTargetFailed means local creation reached a terminal failure.
	RecoveryTargetFailed RecoveryTargetState = "failed"
	// RecoveryTargetAbandoned means the operator stopped this recovery before publication.
	RecoveryTargetAbandoned RecoveryTargetState = "abandoned"
)

// RecoveryFailure classifies one target's failed manual replacement attempt.
type RecoveryFailure string

const (
	// RecoveryFailureNone means the target has no failure classification.
	RecoveryFailureNone RecoveryFailure = ""
	// RecoveryFailureQueueFull means the bounded local queue had no capacity.
	RecoveryFailureQueueFull RecoveryFailure = "queue_full"
	// RecoveryFailureRunnerClosed means the local runner was closed.
	RecoveryFailureRunnerClosed RecoveryFailure = "runner_closed"
	// RecoveryFailureBlueprintUnavailable means BlueprintRegistry did not have the requested blueprint.
	RecoveryFailureBlueprintUnavailable RecoveryFailure = "blueprint_unavailable"
	// RecoveryFailureCreate means Runtime.CreateService returned an error.
	RecoveryFailureCreate RecoveryFailure = "create"
	// RecoveryFailureDirectoryUnavailable means Directory could not be read before an action.
	RecoveryFailureDirectoryUnavailable RecoveryFailure = "directory_unavailable"
	// RecoveryFailureDirectoryChanged means Directory no longer matched the frozen expected set.
	RecoveryFailureDirectoryChanged RecoveryFailure = "directory_changed"
	// RecoveryFailurePublish means Directory publish returned a confirmed failure.
	RecoveryFailurePublish RecoveryFailure = "publish"
)

// RecoveryTarget records the Coordinator-owned replacement fact for one removed ServiceRef.
type RecoveryTarget struct {
	Removed   gsr.ServiceRef      `json:"removed"`
	Agent     gsr.ServiceRef      `json:"agent"`
	Blueprint BlueprintID         `json:"blueprint"`
	Created   gsr.ServiceRef      `json:"created"`
	State     RecoveryTargetState `json:"state"`
	Failure   RecoveryFailure     `json:"failure"`
}

// RecoveryTargetRequest pairs one removed Service with its local NodeAgent and blueprint.
type RecoveryTargetRequest struct {
	Removed   gsr.ServiceRef `json:"removed"`
	Agent     gsr.ServiceRef `json:"agent"`
	Blueprint BlueprintID    `json:"blueprint"`
}

// RecoveryPhase describes the durable conclusion of one manual recovery operation.
type RecoveryPhase string

const (
	// RecoveryCreating means targets are being submitted or awaiting local creation receipts.
	RecoveryCreating RecoveryPhase = "creating"
	// RecoveryAwaitingConfirmation means all replacements exist but have not been published.
	RecoveryAwaitingConfirmation RecoveryPhase = "awaiting_confirmation"
	// RecoveryPublishing means a Directory CAS may have committed and needs Resolve.
	RecoveryPublishing RecoveryPhase = "publishing"
	// RecoveryCompleted means all replacements were published.
	RecoveryCompleted RecoveryPhase = "completed"
	// RecoveryFailed means creation or confirmed publication failed.
	RecoveryFailed RecoveryPhase = "failed"
	// RecoveryAbandoned means an operator abandoned an unpublished recovery.
	RecoveryAbandoned RecoveryPhase = "abandoned"
)

// RecoveryOperation is one independent Coordinator-owned snapshot of a manual recovery.
type RecoveryOperation struct {
	RequestID RequestID               `json:"request_id"`
	Principal Principal               `json:"principal"`
	Group     servicegroup.GroupName  `json:"group"`
	Expected  servicegroup.ServiceSet `json:"expected"`
	Published servicegroup.ServiceSet `json:"published"`
	Targets   []RecoveryTarget        `json:"targets"`
	Phase     RecoveryPhase           `json:"phase"`
	CreatedAt time.Time               `json:"created_at"`
	UpdatedAt time.Time               `json:"updated_at"`
}

// BeginRecoveryRequest specifies the expected published set and all replacements to create.
type BeginRecoveryRequest struct {
	RequestID RequestID               `json:"request_id"`
	Principal Principal               `json:"principal"`
	Group     servicegroup.GroupName  `json:"group"`
	Expected  servicegroup.ServiceSet `json:"expected"`
	Targets   []RecoveryTargetRequest `json:"targets"`
}

// RecoveryCreateTask identifies one NodeAgent-owned local replacement creation request.
type RecoveryCreateTask struct {
	Agent     gsr.ServiceRef `json:"agent"`
	RequestID RequestID      `json:"request_id"`
	Removed   gsr.ServiceRef `json:"removed"`
	Blueprint BlueprintID    `json:"blueprint"`
}

// RecoveryExecutor accepts one bounded local replacement creation task.
type RecoveryExecutor interface {
	Submit(RecoveryCreateTask) error
}

// RecoveryRuntime is the narrow Runtime capability required by RecoveryRunner.
type RecoveryRuntime interface {
	Send(gsr.ServiceRef, gsr.CommandID, any) error
	CreateService(gsr.ServiceSpec) (gsr.ServiceRef, error)
}

// BlueprintRegistry builds a fresh ServiceSpec for one stable BlueprintID.
type BlueprintRegistry interface {
	Build(BlueprintID) (gsr.ServiceSpec, error)
}

// RecoveryRunnerConfig configures one composition-root-owned Recovery worker pool.
type RecoveryRunnerConfig struct {
	Registry  BlueprintRegistry
	Workers   int
	QueueSize int
}

// RecoveryReceipt is the NodeAgent-owned local creation fact for one target.
type RecoveryReceipt struct {
	RequestID RequestID           `json:"request_id"`
	Removed   gsr.ServiceRef      `json:"removed"`
	Blueprint BlueprintID         `json:"blueprint"`
	Created   gsr.ServiceRef      `json:"created"`
	State     RecoveryTargetState `json:"state"`
	Failure   RecoveryFailure     `json:"failure"`
	UpdatedAt time.Time           `json:"updated_at"`
}

// recoveryCreateResult is the private Runner-to-NodeAgent result payload.
type recoveryCreateResult struct {
	RequestID RequestID
	Removed   gsr.ServiceRef
	Blueprint BlueprintID
	Created   gsr.ServiceRef
	State     RecoveryTargetState
	Failure   RecoveryFailure
}
