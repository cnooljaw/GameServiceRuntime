package drain

import (
	"errors"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const (
	commandAcquireVisitorLease gsr.CommandID = 0x02700101
	commandRenewVisitorLease   gsr.CommandID = 0x02700102
	commandReleaseVisitorLease gsr.CommandID = 0x02700103
	commandListVisitors        gsr.CommandID = 0x02700104
	commandSweepVisitors       gsr.CommandID = 0x027001fe
	// BeginDrainCommand starts one decorated Service's irreversible Drain Guard.
	BeginDrainCommand gsr.CommandID = 0x02700201
	// GetDrainStatusCommand reads one decorated Service's Drain Guard state.
	GetDrainStatusCommand gsr.CommandID = 0x02700202
)

type errorCode string

const (
	responseOK                 errorCode = ""
	responseInvalidLease       errorCode = "invalid_lease"
	responseInvalidTarget      errorCode = "invalid_target"
	responseInvalidVisitor     errorCode = "invalid_visitor"
	responseLeaseExpired       errorCode = "lease_expired"
	responseLeaseOwnerMismatch errorCode = "lease_owner_mismatch"
	responseLeaseExhausted     errorCode = "lease_exhausted"
	responseInvalidRequest     errorCode = "invalid_request"
	responseInvalidGuard       errorCode = "invalid_guard"
	responseUnauthorized       errorCode = "unauthorized"
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

type wireVisitorLease struct {
	Target         wireServiceRef `json:"target"`
	Visitor        wireServiceRef `json:"visitor"`
	AuthorityEpoch uint64         `json:"authority_epoch"`
	Generation     uint64         `json:"generation"`
	Weak           bool           `json:"weak"`
	ExpiresAt      time.Time      `json:"expires_at"`
}

func newWireVisitorLease(lease VisitorLease) wireVisitorLease {
	return wireVisitorLease{
		Target:         newWireServiceRef(lease.Target),
		Visitor:        newWireServiceRef(lease.Visitor),
		AuthorityEpoch: lease.AuthorityEpoch,
		Generation:     lease.Generation,
		Weak:           lease.Weak,
		ExpiresAt:      lease.ExpiresAt,
	}
}

func (lease wireVisitorLease) visitorLease() VisitorLease {
	return VisitorLease{
		Target:         lease.Target.serviceRef(),
		Visitor:        lease.Visitor.serviceRef(),
		AuthorityEpoch: lease.AuthorityEpoch,
		Generation:     lease.Generation,
		Weak:           lease.Weak,
		ExpiresAt:      lease.ExpiresAt,
	}
}

type wireVisitorRef struct {
	Visitor    wireServiceRef `json:"visitor"`
	Generation uint64         `json:"generation"`
	Weak       bool           `json:"weak"`
	ExpiresAt  time.Time      `json:"expires_at"`
}

func newWireVisitorRef(visitor VisitorRef) wireVisitorRef {
	return wireVisitorRef{
		Visitor:    newWireServiceRef(visitor.Visitor),
		Generation: visitor.Generation,
		Weak:       visitor.Weak,
		ExpiresAt:  visitor.ExpiresAt,
	}
}

func (visitor wireVisitorRef) visitorRef() VisitorRef {
	return VisitorRef{
		Visitor:    visitor.Visitor.serviceRef(),
		Generation: visitor.Generation,
		Weak:       visitor.Weak,
		ExpiresAt:  visitor.ExpiresAt,
	}
}

type acquireVisitorLeaseRequest struct {
	Target  wireServiceRef `json:"target"`
	Visitor wireServiceRef `json:"visitor"`
	Weak    bool           `json:"weak"`
}

type renewVisitorLeaseRequest struct {
	Lease wireVisitorLease `json:"lease"`
}

type releaseVisitorLeaseRequest struct {
	Lease wireVisitorLease `json:"lease"`
}

type listVisitorsRequest struct {
	Target wireServiceRef `json:"target"`
}

type leaseResponse struct {
	Lease wireVisitorLease `json:"lease"`
	Error errorCode        `json:"error"`
}

type listVisitorsResponse struct {
	Visitors []wireVisitorRef `json:"visitors"`
	Error    errorCode        `json:"error"`
}

type emptyResponse struct {
	Error errorCode `json:"error"`
}

type wireDrainStatus struct {
	Draining  bool      `json:"draining"`
	StartedAt time.Time `json:"started_at"`
}

func newWireDrainStatus(status DrainStatus) wireDrainStatus {
	return wireDrainStatus{Draining: status.Draining, StartedAt: status.StartedAt}
}

func (status wireDrainStatus) drainStatus() DrainStatus {
	return DrainStatus{Draining: status.Draining, StartedAt: status.StartedAt}
}

func validWireDrainStatus(status wireDrainStatus) bool {
	return validDrainStatus(status.drainStatus())
}

type beginDrainRequest struct{}

type getDrainStatusRequest struct{}

type drainStatusResponse struct {
	Status wireDrainStatus `json:"status"`
	Error  errorCode       `json:"error"`
}

type sweepVisitorsRequest struct{}

func errorFromCode(code errorCode) error {
	switch code {
	case responseOK:
		return nil
	case responseInvalidLease:
		return ErrInvalidLease
	case responseInvalidTarget:
		return ErrInvalidTarget
	case responseInvalidVisitor:
		return ErrInvalidVisitor
	case responseLeaseExpired:
		return ErrLeaseExpired
	case responseLeaseOwnerMismatch:
		return ErrLeaseOwnerMismatch
	case responseLeaseExhausted:
		return ErrLeaseExhausted
	default:
		return ErrInvalidResponse
	}
}

func guardErrorFromCode(code errorCode) error {
	switch code {
	case responseOK:
		return nil
	case responseInvalidGuard:
		return ErrInvalidGuard
	case responseUnauthorized:
		return ErrUnauthorized
	default:
		return ErrInvalidResponse
	}
}

func codeFromError(err error) errorCode {
	switch {
	case err == nil:
		return responseOK
	case errors.Is(err, ErrInvalidLease):
		return responseInvalidLease
	case errors.Is(err, ErrInvalidTarget):
		return responseInvalidTarget
	case errors.Is(err, ErrInvalidVisitor):
		return responseInvalidVisitor
	case errors.Is(err, ErrLeaseExpired):
		return responseLeaseExpired
	case errors.Is(err, ErrLeaseOwnerMismatch):
		return responseLeaseOwnerMismatch
	case errors.Is(err, ErrLeaseExhausted):
		return responseLeaseExhausted
	default:
		return responseInvalidRequest
	}
}

func guardCodeFromError(err error) errorCode {
	switch {
	case err == nil:
		return responseOK
	case errors.Is(err, ErrInvalidGuard):
		return responseInvalidGuard
	case errors.Is(err, ErrUnauthorized):
		return responseUnauthorized
	default:
		return responseInvalidGuard
	}
}
