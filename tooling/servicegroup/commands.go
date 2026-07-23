package servicegroup

import (
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const (
	commandPublishServiceSet      gsr.CommandID = 0x02600101
	commandGetServiceSet          gsr.CommandID = 0x02600102
	commandWatchServiceGroup      gsr.CommandID = 0x02600103
	commandRenewServiceGroupWatch gsr.CommandID = 0x02600104
	commandUnwatchServiceGroup    gsr.CommandID = 0x02600105
	commandSweepExpiredWatches    gsr.CommandID = 0x026001fe
)

type errorCode string

const (
	responseOK                errorCode = ""
	responseInvalidGroup      errorCode = "invalid_group"
	responseInvalidServiceSet errorCode = "invalid_service_set"
	responseGroupNotFound     errorCode = "group_not_found"
	responseVersionConflict   errorCode = "version_conflict"
	responseVersionExhausted  errorCode = "version_exhausted"
	responseUnauthorized      errorCode = "unauthorized"
	responseInvalidRequest    errorCode = "invalid_request"
)

type wireServiceRef struct {
	Node gsr.NodeID    `json:"node"`
	ID   gsr.ServiceID `json:"id"`
}

func newWireServiceRef(ref gsr.ServiceRef) wireServiceRef {
	return wireServiceRef{Node: ref.Node, ID: ref.ID}
}

func (ref wireServiceRef) serviceRef() gsr.ServiceRef {
	return gsr.ServiceRef{Node: ref.Node, ID: ref.ID}
}

type wireServiceSet struct {
	Name    GroupName         `json:"name"`
	Version ServiceSetVersion `json:"version"`
	Refs    []wireServiceRef  `json:"refs"`
	Tags    map[string]string `json:"tags"`
}

func newWireServiceSet(set ServiceSet) wireServiceSet {
	refs := make([]wireServiceRef, len(set.Refs))
	for index, ref := range set.Refs {
		refs[index] = newWireServiceRef(ref)
	}
	return wireServiceSet{
		Name:    set.Name,
		Version: set.Version,
		Refs:    refs,
		Tags:    cloneTags(set.Tags),
	}
}

func (set wireServiceSet) serviceSet() ServiceSet {
	refs := make([]gsr.ServiceRef, len(set.Refs))
	for index, ref := range set.Refs {
		refs[index] = ref.serviceRef()
	}
	return ServiceSet{
		Name:    set.Name,
		Version: set.Version,
		Refs:    refs,
		Tags:    cloneTags(set.Tags),
	}
}

type publishServiceSetRequest struct {
	Name     GroupName         `json:"name"`
	Expected ServiceSetVersion `json:"expected"`
	Refs     []wireServiceRef  `json:"refs"`
	Tags     map[string]string `json:"tags"`
}

type getServiceSetRequest struct {
	Name GroupName `json:"name"`
}

type serviceSetResponse struct {
	Set   wireServiceSet `json:"set"`
	Error errorCode      `json:"error"`
}
