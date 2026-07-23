package control

import (
	"context"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/servicegroup"
)

// DefaultDrainCoordinatorName is the stable local name normally assigned to DrainCoordinatorService.
const DefaultDrainCoordinatorName gsr.ServiceName = ".drain-coordinator"

// Principal identifies an authenticated control-plane operator asserted by Gateway.
type Principal string

// RequestID identifies one idempotent Drain operation.
type RequestID string

// DrainPhase describes the durable in-memory conclusion of one Drain operation.
type DrainPhase string

const (
	// DrainPreparing means the Coordinator has recorded the operation but has not confirmed its Directory publish.
	DrainPreparing DrainPhase = "preparing"
	// DrainPublishUnknown means the Directory publish may have committed but its result was not confirmed.
	DrainPublishUnknown DrainPhase = "publish_unknown"
	// DrainGuarding means the replacement ServiceSet is confirmed and removed members are being guarded.
	DrainGuarding DrainPhase = "guarding"
	// DrainWaitingVisitors means every removed member is guarded and strong visitors are being checked.
	DrainWaitingVisitors DrainPhase = "waiting_visitors"
	// DrainReadyToStop means every removed member is guarded and has no strong visitor.
	DrainReadyToStop DrainPhase = "ready_to_stop"
	// DrainConflict means the expected Directory version could not be published.
	DrainConflict DrainPhase = "conflict"
	// DrainSuperseded means a different Directory version superseded this operation.
	DrainSuperseded DrainPhase = "superseded"
)

// DrainTarget is the per-removed-member state recorded by a Drain operation.
type DrainTarget struct {
	Ref                gsr.ServiceRef `json:"ref"`
	Guarded            bool           `json:"guarded"`
	StrongVisitorCount int            `json:"strong_visitor_count"`
}

// DrainOperation is an independent snapshot of one authorized Drain operation.
type DrainOperation struct {
	RequestID RequestID                      `json:"request_id"`
	Principal Principal                      `json:"principal"`
	Group     servicegroup.GroupName         `json:"group"`
	Expected  servicegroup.ServiceSetVersion `json:"expected"`
	Original  servicegroup.ServiceSet        `json:"original"`
	Published servicegroup.ServiceSet        `json:"published"`
	Targets   []DrainTarget                  `json:"targets"`
	Phase     DrainPhase                     `json:"phase"`
	CreatedAt time.Time                      `json:"created_at"`
	UpdatedAt time.Time                      `json:"updated_at"`
}

// DrainAudit is one bounded, ordered audit fact emitted by DrainCoordinatorService.
type DrainAudit struct {
	Sequence   uint64    `json:"sequence"`
	RequestID  RequestID `json:"request_id"`
	Principal  Principal `json:"principal"`
	Action     string    `json:"action"`
	Outcome    string    `json:"outcome"`
	OccurredAt time.Time `json:"occurred_at"`
}

// StartDrainRequest specifies the replacement content for one versioned ServiceGroup Drain.
type StartDrainRequest struct {
	RequestID RequestID                      `json:"request_id"`
	Principal Principal                      `json:"principal"`
	Group     servicegroup.GroupName         `json:"group"`
	Expected  servicegroup.ServiceSetVersion `json:"expected"`
	NextRefs  []gsr.ServiceRef               `json:"next_refs"`
	NextTags  map[string]string              `json:"next_tags"`
}

// DrainCoordinatorConfig configures the trusted Gateway and fact services used by DrainCoordinatorService.
type DrainCoordinatorConfig struct {
	Gateway           gsr.ServiceRef
	AllowedPrincipals []Principal
	Directory         gsr.ServiceRef
	VisitorRegistry   gsr.ServiceRef
	CallTimeout       time.Duration
	AuditLimit        int
}

// DrainClient provides typed Calls to one Gateway-facing DrainCoordinatorService.
type DrainClient struct {
	caller CommandCaller
	target gsr.ServiceRef
}

// NewDrainClient binds a typed Drain client to a Gateway-facing Drain Coordinator target.
func NewDrainClient(caller CommandCaller, target gsr.ServiceRef) (*DrainClient, error) {
	if isNil(caller) {
		return nil, ErrInvalidCaller
	}
	if !validServiceRef(target) {
		return nil, ErrInvalidConfig
	}
	return &DrainClient{caller: caller, target: target}, nil
}

// Start creates or retrieves one RequestID-idempotent Drain operation.
func (c *DrainClient) Start(ctx context.Context, request StartDrainRequest) (DrainOperation, error) {
	request, err := normalizeStartDrainRequest(request)
	if err != nil {
		return DrainOperation{}, err
	}
	value, err := c.caller.Call(ctx, c.target, commandStartDrainOperation, startDrainOperationRequest{Request: request})
	if err != nil {
		return DrainOperation{}, err
	}
	return drainOperationFromResponse(value)
}

// Resolve explicitly advances one non-terminal Drain operation without background retries.
func (c *DrainClient) Resolve(ctx context.Context, requestID RequestID, principal Principal) (DrainOperation, error) {
	if !validRequestID(requestID) {
		return DrainOperation{}, ErrInvalidRequestID
	}
	if !validPrincipal(principal) {
		return DrainOperation{}, ErrInvalidPrincipal
	}
	value, err := c.caller.Call(ctx, c.target, commandResolveDrainOperation, resolveDrainOperationRequest{RequestID: requestID, Principal: principal})
	if err != nil {
		return DrainOperation{}, err
	}
	return drainOperationFromResponse(value)
}

// Get returns one independent Drain operation snapshot owned by principal.
func (c *DrainClient) Get(ctx context.Context, requestID RequestID, principal Principal) (DrainOperation, error) {
	if !validRequestID(requestID) {
		return DrainOperation{}, ErrInvalidRequestID
	}
	if !validPrincipal(principal) {
		return DrainOperation{}, ErrInvalidPrincipal
	}
	value, err := c.caller.Call(ctx, c.target, commandGetDrainOperation, getDrainOperationRequest{RequestID: requestID, Principal: principal})
	if err != nil {
		return DrainOperation{}, err
	}
	return drainOperationFromResponse(value)
}

// ListAudit returns ordered independent audit facts visible to an authorized principal.
func (c *DrainClient) ListAudit(ctx context.Context, principal Principal) ([]DrainAudit, error) {
	if !validPrincipal(principal) {
		return nil, ErrInvalidPrincipal
	}
	value, err := c.caller.Call(ctx, c.target, commandListDrainAudit, listDrainAuditRequest{Principal: principal})
	if err != nil {
		return nil, err
	}
	response, ok := value.(drainAuditsResponse)
	if !ok {
		return nil, ErrInvalidResponse
	}
	if err := errorFromCode(response.Error); err != nil {
		return nil, err
	}
	if !validDrainAudits(response.Audits) {
		return nil, ErrInvalidResponse
	}
	return cloneDrainAudits(response.Audits), nil
}

func drainOperationFromResponse(value any) (DrainOperation, error) {
	response, ok := value.(drainOperationResponse)
	if !ok {
		return DrainOperation{}, ErrInvalidResponse
	}
	if err := errorFromCode(response.Error); err != nil {
		return DrainOperation{}, err
	}
	if !validDrainOperation(response.Operation) {
		return DrainOperation{}, ErrInvalidResponse
	}
	return cloneDrainOperation(response.Operation), nil
}
