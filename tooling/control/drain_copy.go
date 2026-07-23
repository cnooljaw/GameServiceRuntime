package control

import (
	"sort"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/servicegroup"
)

func cloneDrainOperation(operation DrainOperation) DrainOperation {
	cloned := operation
	cloned.Original = cloneDrainServiceSet(operation.Original)
	cloned.Published = cloneDrainServiceSet(operation.Published)
	if operation.Targets != nil {
		cloned.Targets = make([]DrainTarget, len(operation.Targets))
		copy(cloned.Targets, operation.Targets)
	}
	return cloned
}

func cloneDrainAudits(audits []DrainAudit) []DrainAudit {
	return append([]DrainAudit(nil), audits...)
}

func cloneDrainServiceSet(set servicegroup.ServiceSet) servicegroup.ServiceSet {
	cloned := set
	if set.Refs != nil {
		cloned.Refs = make([]gsr.ServiceRef, len(set.Refs))
		copy(cloned.Refs, set.Refs)
	}
	if set.Tags != nil {
		cloned.Tags = make(map[string]string, len(set.Tags))
		for key, value := range set.Tags {
			cloned.Tags[key] = value
		}
	}
	return cloned
}

func cloneNodeStopTask(task NodeStopTask) NodeStopTask {
	cloned := task
	cloned.Published = cloneDrainServiceSet(task.Published)
	return cloned
}

func cloneStopOperation(operation StopOperation) StopOperation {
	cloned := operation
	cloned.Published = cloneDrainServiceSet(operation.Published)
	if operation.Targets != nil {
		cloned.Targets = append([]StopTarget(nil), operation.Targets...)
	}
	return cloned
}

func drainTargets(original, published servicegroup.ServiceSet) []DrainTarget {
	publishedRefs := make(map[gsr.ServiceRef]struct{}, len(published.Refs))
	for _, ref := range published.Refs {
		publishedRefs[ref] = struct{}{}
	}
	targets := make([]DrainTarget, 0, len(original.Refs))
	for _, ref := range original.Refs {
		if _, retained := publishedRefs[ref]; !retained {
			targets = append(targets, DrainTarget{Ref: ref})
		}
	}
	sort.Slice(targets, func(left, right int) bool {
		if targets[left].Ref.Node != targets[right].Ref.Node {
			return targets[left].Ref.Node < targets[right].Ref.Node
		}
		return targets[left].Ref.ID < targets[right].Ref.ID
	})
	return targets
}
