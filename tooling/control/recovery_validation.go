package control

import (
	"sort"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/servicegroup"
)

func validBlueprintID(id BlueprintID) bool { return validTrimmedUTF8(string(id)) }

func validRecoveryTargetRequest(target RecoveryTargetRequest) bool {
	return validServiceRef(target.Removed) && validServiceRef(target.Agent) && target.Removed != target.Agent && target.Removed.Node == target.Agent.Node && validBlueprintID(target.Blueprint)
}

func normalizeBeginRecoveryRequest(request BeginRecoveryRequest) (BeginRecoveryRequest, error) {
	if !validRequestID(request.RequestID) {
		return BeginRecoveryRequest{}, ErrInvalidRequestID
	}
	if !validPrincipal(request.Principal) {
		return BeginRecoveryRequest{}, ErrInvalidPrincipal
	}
	if !validDrainGroup(request.Group) || !validDrainServiceSet(request.Expected) || request.Expected.Name != request.Group {
		return BeginRecoveryRequest{}, ErrInvalidRecoveryRequest
	}
	targets := append([]RecoveryTargetRequest(nil), request.Targets...)
	if len(targets) == 0 {
		return BeginRecoveryRequest{}, ErrInvalidRecoveryRequest
	}
	for _, target := range targets {
		if !validRecoveryTargetRequest(target) {
			return BeginRecoveryRequest{}, ErrInvalidRecoveryRequest
		}
	}
	sort.Slice(targets, func(left, right int) bool {
		if targets[left].Removed.Node != targets[right].Removed.Node {
			return targets[left].Removed.Node < targets[right].Removed.Node
		}
		return targets[left].Removed.ID < targets[right].Removed.ID
	})
	for index := 1; index < len(targets); index++ {
		if targets[index-1].Removed == targets[index].Removed {
			return BeginRecoveryRequest{}, ErrInvalidRecoveryRequest
		}
	}
	request.Expected = cloneDrainServiceSet(request.Expected)
	request.Targets = targets
	return request, nil
}

func sameBeginRecoveryRequest(left, right BeginRecoveryRequest) bool {
	if left.RequestID != right.RequestID || left.Principal != right.Principal || left.Group != right.Group || !sameDrainServiceSet(left.Expected, right.Expected) || len(left.Targets) != len(right.Targets) {
		return false
	}
	for index := range left.Targets {
		if left.Targets[index] != right.Targets[index] {
			return false
		}
	}
	return true
}

func validRecoveryTargetState(state RecoveryTargetState) bool {
	switch state {
	case RecoveryTargetPending, RecoveryTargetCreating, RecoveryTargetCreated, RecoveryTargetPublished, RecoveryTargetFailed, RecoveryTargetAbandoned:
		return true
	default:
		return false
	}
}

func validRecoveryFailure(failure RecoveryFailure) bool {
	switch failure {
	case RecoveryFailureNone, RecoveryFailureQueueFull, RecoveryFailureRunnerClosed, RecoveryFailureBlueprintUnavailable, RecoveryFailureCreate, RecoveryFailureDirectoryUnavailable, RecoveryFailureDirectoryChanged, RecoveryFailurePublish:
		return true
	default:
		return false
	}
}

func validRecoveryTarget(target RecoveryTarget) bool {
	if !validRecoveryTargetRequest(RecoveryTargetRequest{Removed: target.Removed, Agent: target.Agent, Blueprint: target.Blueprint}) || !validRecoveryTargetState(target.State) || !validRecoveryFailure(target.Failure) {
		return false
	}
	created := validServiceRef(target.Created) && target.Created != target.Removed && target.Created != target.Agent && target.Created.Node == target.Agent.Node
	switch target.State {
	case RecoveryTargetPending, RecoveryTargetCreating:
		return target.Created == (gsr.ServiceRef{}) && target.Failure == RecoveryFailureNone
	case RecoveryTargetCreated, RecoveryTargetPublished:
		return created && target.Failure == RecoveryFailureNone
	case RecoveryTargetFailed:
		return target.Created == (gsr.ServiceRef{}) && target.Failure != RecoveryFailureNone
	case RecoveryTargetAbandoned:
		return target.Failure == RecoveryFailureNone && (target.Created == (gsr.ServiceRef{}) || created)
	default:
		return false
	}
}

func validRecoveryPhase(phase RecoveryPhase) bool {
	switch phase {
	case RecoveryCreating, RecoveryAwaitingConfirmation, RecoveryPublishing, RecoveryCompleted, RecoveryFailed, RecoveryAbandoned:
		return true
	default:
		return false
	}
}

func emptyRecoveryServiceSet(set servicegroup.ServiceSet) bool { return emptyDrainServiceSet(set) }

func validRecoveryOperation(operation RecoveryOperation) bool {
	if !validRequestID(operation.RequestID) || !validPrincipal(operation.Principal) || !validDrainGroup(operation.Group) || !validDrainServiceSet(operation.Expected) || operation.Expected.Name != operation.Group || !validRecoveryPhase(operation.Phase) || operation.Targets == nil || operation.CreatedAt.IsZero() || operation.UpdatedAt.Before(operation.CreatedAt) {
		return false
	}
	for index, target := range operation.Targets {
		if !validRecoveryTarget(target) {
			return false
		}
		if index > 0 {
			previous := operation.Targets[index-1]
			if previous.Removed.Node > target.Removed.Node || (previous.Removed.Node == target.Removed.Node && previous.Removed.ID >= target.Removed.ID) {
				return false
			}
		}
	}
	switch operation.Phase {
	case RecoveryCreating:
		return emptyRecoveryServiceSet(operation.Published)
	case RecoveryAwaitingConfirmation:
		if !emptyRecoveryServiceSet(operation.Published) {
			return false
		}
		for _, target := range operation.Targets {
			if target.State != RecoveryTargetCreated {
				return false
			}
		}
		return true
	case RecoveryPublishing:
		return emptyRecoveryServiceSet(operation.Published) || validRecoveryPublishedSet(operation)
	case RecoveryCompleted:
		if !validRecoveryPublishedSet(operation) {
			return false
		}
		for _, target := range operation.Targets {
			if target.State != RecoveryTargetPublished {
				return false
			}
		}
		return true
	case RecoveryFailed:
		for _, target := range operation.Targets {
			if target.State == RecoveryTargetCreating || target.State == RecoveryTargetPending {
				return false
			}
		}
		return emptyRecoveryServiceSet(operation.Published) || validRecoveryPublishedSet(operation)
	case RecoveryAbandoned:
		if !emptyRecoveryServiceSet(operation.Published) {
			return false
		}
		for _, target := range operation.Targets {
			if target.State != RecoveryTargetAbandoned {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func validRecoveryPublishedSet(operation RecoveryOperation) bool {
	set := operation.Published
	if !validDrainServiceSet(set) || set.Name != operation.Group || set.Version.AuthorityEpoch != operation.Expected.Version.AuthorityEpoch || set.Version.Revision != operation.Expected.Version.Revision+1 {
		return false
	}
	for _, ref := range operation.Expected.Refs {
		if !containsRecoveryRef(set.Refs, ref) {
			return false
		}
	}
	for _, target := range operation.Targets {
		if containsRecoveryRef(set.Refs, target.Removed) || !containsRecoveryRef(set.Refs, target.Created) {
			return false
		}
	}
	return true
}

func containsRecoveryRef(refs []gsr.ServiceRef, target gsr.ServiceRef) bool {
	for _, ref := range refs {
		if ref == target {
			return true
		}
	}
	return false
}

func validRecoveryCreateTask(task RecoveryCreateTask) bool {
	return validServiceRef(task.Agent) && validRequestID(task.RequestID) && validServiceRef(task.Removed) && task.Agent != task.Removed && task.Agent.Node == task.Removed.Node && validBlueprintID(task.Blueprint)
}

func sameRecoveryCreateTask(left, right RecoveryCreateTask) bool { return left == right }

func validRecoveryCreateResult(result recoveryCreateResult) bool {
	if !validRequestID(result.RequestID) || !validServiceRef(result.Removed) || !validBlueprintID(result.Blueprint) || !validRecoveryTargetState(result.State) || !validRecoveryFailure(result.Failure) {
		return false
	}
	switch result.State {
	case RecoveryTargetCreated:
		return validServiceRef(result.Created) && result.Created != result.Removed && result.Failure == RecoveryFailureNone
	case RecoveryTargetFailed:
		return result.Created == (gsr.ServiceRef{}) && (result.Failure == RecoveryFailureQueueFull || result.Failure == RecoveryFailureBlueprintUnavailable || result.Failure == RecoveryFailureCreate || result.Failure == RecoveryFailureRunnerClosed)
	default:
		return false
	}
}

func validRecoveryReceipt(receipt RecoveryReceipt) bool {
	if receipt.UpdatedAt.IsZero() || !validRequestID(receipt.RequestID) || !validServiceRef(receipt.Removed) || !validBlueprintID(receipt.Blueprint) {
		return false
	}
	switch receipt.State {
	case RecoveryTargetCreating:
		return receipt.Created == (gsr.ServiceRef{}) && receipt.Failure == RecoveryFailureNone
	case RecoveryTargetCreated, RecoveryTargetFailed:
		return validRecoveryCreateResult(recoveryCreateResult{RequestID: receipt.RequestID, Removed: receipt.Removed, Blueprint: receipt.Blueprint, Created: receipt.Created, State: receipt.State, Failure: receipt.Failure})
	default:
		return false
	}
}
