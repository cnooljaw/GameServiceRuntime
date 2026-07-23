package control

import (
	"sort"
	"strings"
	"unicode/utf8"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/servicegroup"
)

func validPrincipal(principal Principal) bool { return validTrimmedUTF8(string(principal)) }

func validRequestID(requestID RequestID) bool { return validTrimmedUTF8(string(requestID)) }

func validTrimmedUTF8(value string) bool {
	return utf8.ValidString(value) && value != "" && strings.TrimSpace(value) == value
}

func validDrainGroup(group servicegroup.GroupName) bool { return validTrimmedUTF8(string(group)) }

func validDrainVersion(version servicegroup.ServiceSetVersion) bool {
	return version.AuthorityEpoch != 0 && version.Revision != 0
}

func validDrainTags(tags map[string]string) bool {
	for key, value := range tags {
		if !validTrimmedUTF8(key) || !utf8.ValidString(value) {
			return false
		}
	}
	return true
}

func normalizeStartDrainRequest(request StartDrainRequest) (StartDrainRequest, error) {
	if !validRequestID(request.RequestID) {
		return StartDrainRequest{}, ErrInvalidRequestID
	}
	if !validPrincipal(request.Principal) {
		return StartDrainRequest{}, ErrInvalidPrincipal
	}
	if !validDrainGroup(request.Group) || !validDrainVersion(request.Expected) || !validDrainTags(request.NextTags) {
		return StartDrainRequest{}, ErrInvalidDrainRequest
	}
	refs := append([]gsr.ServiceRef(nil), request.NextRefs...)
	for _, ref := range refs {
		if !validServiceRef(ref) {
			return StartDrainRequest{}, ErrInvalidDrainRequest
		}
	}
	sort.Slice(refs, func(left, right int) bool {
		if refs[left].Node != refs[right].Node {
			return refs[left].Node < refs[right].Node
		}
		return refs[left].ID < refs[right].ID
	})
	unique := make([]gsr.ServiceRef, 0, len(refs))
	for _, ref := range refs {
		if len(unique) == 0 || unique[len(unique)-1] != ref {
			unique = append(unique, ref)
		}
	}
	if unique == nil {
		unique = make([]gsr.ServiceRef, 0)
	}
	request.NextRefs = unique
	tags := request.NextTags
	request.NextTags = make(map[string]string, len(tags))
	for key, value := range tags {
		request.NextTags[key] = value
	}
	return request, nil
}

func normalizeBeginStopRequest(request BeginStopRequest) (BeginStopRequest, error) {
	if !validRequestID(request.RequestID) {
		return BeginStopRequest{}, ErrInvalidRequestID
	}
	if !validPrincipal(request.Principal) {
		return BeginStopRequest{}, ErrInvalidPrincipal
	}
	targets := append([]StopTargetRequest(nil), request.Targets...)
	for _, target := range targets {
		if !validStopTargetRequest(target) {
			return BeginStopRequest{}, ErrInvalidStopRequest
		}
	}
	sort.Slice(targets, func(left, right int) bool {
		if targets[left].Target.Node != targets[right].Target.Node {
			return targets[left].Target.Node < targets[right].Target.Node
		}
		return targets[left].Target.ID < targets[right].Target.ID
	})
	for index := 1; index < len(targets); index++ {
		if targets[index-1].Target == targets[index].Target {
			return BeginStopRequest{}, ErrInvalidStopRequest
		}
	}
	if targets == nil {
		targets = make([]StopTargetRequest, 0)
	}
	request.Targets = targets
	return request, nil
}

func sameStartDrainRequest(left, right StartDrainRequest) bool {
	if left.RequestID != right.RequestID || left.Principal != right.Principal || left.Group != right.Group || left.Expected != right.Expected || len(left.NextRefs) != len(right.NextRefs) || len(left.NextTags) != len(right.NextTags) {
		return false
	}
	for index := range left.NextRefs {
		if left.NextRefs[index] != right.NextRefs[index] {
			return false
		}
	}
	for key, value := range left.NextTags {
		if rightValue, exists := right.NextTags[key]; !exists || rightValue != value {
			return false
		}
	}
	return true
}

func sameBeginStopRequest(left, right BeginStopRequest) bool {
	if left.RequestID != right.RequestID || left.Principal != right.Principal || len(left.Targets) != len(right.Targets) {
		return false
	}
	for index := range left.Targets {
		if left.Targets[index] != right.Targets[index] {
			return false
		}
	}
	return true
}

func validDrainServiceSet(set servicegroup.ServiceSet) bool {
	if !validDrainGroup(set.Name) || !validDrainVersion(set.Version) || set.Refs == nil || set.Tags == nil || !validDrainTags(set.Tags) {
		return false
	}
	for index, ref := range set.Refs {
		if !validServiceRef(ref) {
			return false
		}
		if index > 0 {
			previous := set.Refs[index-1]
			if previous.Node > ref.Node || (previous.Node == ref.Node && previous.ID >= ref.ID) {
				return false
			}
		}
	}
	return true
}

func emptyDrainServiceSet(set servicegroup.ServiceSet) bool {
	return set.Name == "" && set.Version == (servicegroup.ServiceSetVersion{}) && set.Refs == nil && set.Tags == nil
}

func validDrainPhase(phase DrainPhase) bool {
	switch phase {
	case DrainPreparing, DrainPublishUnknown, DrainGuarding, DrainWaitingVisitors, DrainReadyToStop, DrainConflict, DrainSuperseded:
		return true
	default:
		return false
	}
}

func validDrainOperation(operation DrainOperation) bool {
	if !validRequestID(operation.RequestID) || !validPrincipal(operation.Principal) || !validDrainGroup(operation.Group) || !validDrainVersion(operation.Expected) || !validDrainPhase(operation.Phase) || operation.CreatedAt.IsZero() || operation.UpdatedAt.Before(operation.CreatedAt) {
		return false
	}
	if !emptyDrainServiceSet(operation.Original) && (!validDrainServiceSet(operation.Original) || operation.Original.Name != operation.Group || operation.Original.Version != operation.Expected) {
		return false
	}
	if !emptyDrainServiceSet(operation.Published) && (!validDrainServiceSet(operation.Published) || operation.Published.Name != operation.Group || operation.Published.Version.AuthorityEpoch != operation.Expected.AuthorityEpoch || operation.Published.Version.Revision != operation.Expected.Revision+1) {
		return false
	}
	switch operation.Phase {
	case DrainPreparing:
		if !emptyDrainServiceSet(operation.Published) || len(operation.Targets) != 0 {
			return false
		}
	case DrainPublishUnknown:
		if !validDrainServiceSet(operation.Original) || !emptyDrainServiceSet(operation.Published) || len(operation.Targets) != 0 {
			return false
		}
	case DrainConflict:
		if (!emptyDrainServiceSet(operation.Original) && (!validDrainServiceSet(operation.Original) || operation.Original.Name != operation.Group || operation.Original.Version != operation.Expected)) || !emptyDrainServiceSet(operation.Published) || len(operation.Targets) != 0 {
			return false
		}
	case DrainGuarding, DrainWaitingVisitors, DrainReadyToStop:
		if !validDrainServiceSet(operation.Original) || !validDrainServiceSet(operation.Published) || !validDrainTargets(operation.Targets, operation.Original, operation.Published) {
			return false
		}
	case DrainSuperseded:
		if !validDrainServiceSet(operation.Original) {
			return false
		}
		if emptyDrainServiceSet(operation.Published) {
			return len(operation.Targets) == 0
		}
		if !validDrainServiceSet(operation.Published) || !validDrainTargets(operation.Targets, operation.Original, operation.Published) {
			return false
		}
	}
	return true
}

func validDrainTargets(targets []DrainTarget, original, published servicegroup.ServiceSet) bool {
	if targets == nil {
		return false
	}
	want := drainTargets(original, published)
	if len(targets) != len(want) {
		return false
	}
	for index, target := range targets {
		if target.Ref != want[index].Ref || target.StrongVisitorCount < 0 {
			return false
		}
	}
	return true
}

func validDrainAudits(audits []DrainAudit) bool {
	var previous uint64
	for _, audit := range audits {
		if audit.Sequence == 0 || audit.Sequence <= previous || !validRequestID(audit.RequestID) || !validPrincipal(audit.Principal) || !validTrimmedUTF8(audit.Action) || !validTrimmedUTF8(audit.Outcome) || audit.OccurredAt.IsZero() {
			return false
		}
		previous = audit.Sequence
	}
	return true
}

func validNodeStopResult(result nodeStopResult) bool {
	if !validRequestID(result.RequestID) || !validServiceRef(result.Target) {
		return false
	}
	switch result.State {
	case StopTargetStopped, StopTargetSuperseded:
		return result.Failure == StopFailureNone
	case StopTargetPending:
		return result.Failure == StopFailureDirectoryUnavailable
	case StopTargetFailed:
		return result.Failure == StopFailureRuntimeStop || result.Failure == StopFailureRunnerClosed
	default:
		return false
	}
}

func validNodeStopReceipt(receipt NodeStopReceipt) bool {
	if !validRequestID(receipt.RequestID) || !validServiceRef(receipt.Target) || receipt.UpdatedAt.IsZero() {
		return false
	}
	switch receipt.State {
	case StopTargetQueued:
		return receipt.Failure == StopFailureNone
	case StopTargetPending:
		return receipt.Failure == StopFailureQueueFull || receipt.Failure == StopFailureDirectoryUnavailable
	case StopTargetStopped, StopTargetSuperseded:
		return receipt.Failure == StopFailureNone
	case StopTargetFailed:
		return receipt.Failure == StopFailureRuntimeStop || receipt.Failure == StopFailureRunnerClosed
	default:
		return false
	}
}

func validStopTargetRequest(target StopTargetRequest) bool {
	return validServiceRef(target.Target) && validServiceRef(target.Agent) && target.Target != target.Agent && target.Target.Node == target.Agent.Node
}

func validStopTarget(target StopTarget) bool {
	if !validStopTargetRequest(StopTargetRequest{Target: target.Target, Agent: target.Agent}) {
		return false
	}
	switch target.State {
	case StopTargetQueued:
		return target.Failure == StopFailureNone
	case StopTargetPending:
		return target.Failure == StopFailureNone || target.Failure == StopFailureQueueFull || target.Failure == StopFailureDirectoryUnavailable
	case StopTargetStopped, StopTargetSuperseded:
		return target.Failure == StopFailureNone
	case StopTargetFailed:
		return target.Failure == StopFailureRunnerClosed || target.Failure == StopFailureRuntimeStop
	default:
		return false
	}
}

func validStopPhase(phase StopPhase) bool {
	switch phase {
	case StopDispatching, StopWaiting, StopCompleted, StopFailed, StopSuperseded:
		return true
	default:
		return false
	}
}

func validStopOperation(operation StopOperation) bool {
	if !validRequestID(operation.RequestID) || !validPrincipal(operation.Principal) || !validDrainGroup(operation.Group) || !validDrainServiceSet(operation.Published) || operation.Published.Name != operation.Group || !validStopPhase(operation.Phase) || operation.Targets == nil || operation.CreatedAt.IsZero() || operation.UpdatedAt.Before(operation.CreatedAt) {
		return false
	}
	for index, target := range operation.Targets {
		if !validStopTarget(target) {
			return false
		}
		if index > 0 {
			previous := operation.Targets[index-1]
			if previous.Target.Node > target.Target.Node || (previous.Target.Node == target.Target.Node && previous.Target.ID >= target.Target.ID) {
				return false
			}
		}
	}
	switch operation.Phase {
	case StopDispatching, StopWaiting:
		for _, target := range operation.Targets {
			if target.State == StopTargetStopped || target.State == StopTargetFailed || target.State == StopTargetSuperseded {
				continue
			}
		}
	case StopCompleted:
		for _, target := range operation.Targets {
			if target.State != StopTargetStopped {
				return false
			}
		}
	case StopFailed:
		failed := false
		for _, target := range operation.Targets {
			if target.State == StopTargetPending || target.State == StopTargetQueued || target.State == StopTargetSuperseded {
				return false
			}
			failed = failed || target.State == StopTargetFailed
		}
		if !failed {
			return false
		}
	case StopSuperseded:
	}
	return true
}

func sameNodeStopTask(left, right NodeStopTask) bool {
	return left.Agent == right.Agent && left.RequestID == right.RequestID && left.Target == right.Target && left.Group == right.Group && sameDrainServiceSet(left.Published, right.Published)
}
