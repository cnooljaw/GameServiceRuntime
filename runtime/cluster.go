package gsr

import (
	"context"
	"errors"
	"fmt"
)

// WireEnvelope is the transport-safe representation of an Envelope.
type WireEnvelope struct {
	Source       ServiceRef
	Target       ServiceRef
	Session      SessionID
	Command      CommandID
	Payload      []byte
	Response     bool
	CallPath     []ServiceRef
	ErrorCode    string
	ErrorMessage string
}

// ClusterEvents receives decoded transport events for a Runtime.
type ClusterEvents struct {
	Receive     func(NodeID, WireEnvelope)
	Unavailable func(NodeID)
}

// ClusterTransport moves WireEnvelopes between Runtime nodes.
type ClusterTransport interface {
	Start(NodeID, ClusterEvents) error
	Send(NodeID, WireEnvelope) error
	Close(context.Context) error
}

// ClusterCodec encodes Command and Reply payloads at the cluster boundary.
type ClusterCodec interface {
	Encode(CommandID, bool, any) ([]byte, error)
	Decode(CommandID, bool, []byte) (any, error)
}

// RemoteError is an error returned by a remote Service without a stable Runtime error code.
type RemoteError struct {
	Code    string
	Message string
}

func (e *RemoteError) Error() string {
	if e.Message == "" {
		return "gsr: remote error"
	}
	return "gsr: remote error: " + e.Message
}

const maxRemoteErrorMessageLength = 4096

type remoteErrorDefinition struct {
	code string
	err  error
}

var stableRemoteErrors = []remoteErrorDefinition{
	{"timeout", ErrTimeout},
	{"reply_twice", ErrReplyTwice},
	{"service_not_found", ErrServiceNotFound},
	{"service_closed", ErrServiceClosed},
	{"mailbox_full", ErrMailboxFull},
	{"invalid_service_spec", ErrInvalidServiceSpec},
	{"runtime_closed", ErrRuntimeClosed},
	{"reply_unavailable", ErrReplyUnavailable},
	{"reply_expired", ErrReplyExpired},
	{"command_not_registered", ErrCommandNotRegistered},
	{"command_already_registered", ErrCommandAlreadyRegistered},
	{"service_name_conflict", ErrServiceNameConflict},
	{"call_cycle", ErrCallCycle},
	{"call_not_allowed", ErrCallNotAllowed},
	{"service_failed", ErrServiceFailed},
	{"stop_timeout", ErrStopTimeout},
	{"close_timeout", ErrCloseTimeout},
	{"invalid_cluster_config", ErrInvalidClusterConfig},
	{"cluster_start", ErrClusterStart},
	{"remote_unavailable", ErrRemoteUnavailable},
	{"invalid_envelope", ErrInvalidClusterEnvelope},
	{"payload_encode", ErrPayloadEncode},
	{"payload_decode", ErrPayloadDecode},
}

var stableRemoteErrorsByCode = func() map[string]error {
	result := make(map[string]error, len(stableRemoteErrors))
	for _, definition := range stableRemoteErrors {
		result[definition.code] = definition.err
	}
	return result
}()

type clusterRuntime struct {
	runtime   *Runtime
	transport ClusterTransport
	codec     ClusterCodec
}

// NewClusterRuntime creates a Runtime and starts its ClusterTransport.
func NewClusterRuntime(config Config, transport ClusterTransport, codec ClusterCodec) (*Runtime, error) {
	if config.NodeID == "" || transport == nil || codec == nil {
		return nil, ErrInvalidClusterConfig
	}
	runtime := NewRuntime(config)
	cluster := &clusterRuntime{runtime: runtime, transport: transport, codec: codec}
	runtime.cluster = cluster
	events := ClusterEvents{
		Receive:     cluster.receive,
		Unavailable: cluster.unavailable,
	}
	if err := transport.Start(runtime.node, events); err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), runtime.shutdownTimeout)
		defer cancel()
		transportCloseErr := transport.Close(ctx)
		runtime.cluster = nil
		runtimeCloseErr := runtime.Close(ctx)
		startErr := fmt.Errorf("%w: %v", ErrClusterStart, err)
		return nil, errors.Join(startErr, transportCloseErr, runtimeCloseErr)
	}
	return runtime, nil
}

func (c *clusterRuntime) send(envelope Envelope) error {
	source := envelope.Source
	if source == (ServiceRef{}) {
		source = ServiceRef{Node: c.runtime.node}
	}
	var payload []byte
	if envelope.Target.ID == 0 {
		if envelope.Command != coreResolveNameCommand || envelope.Session == 0 || source.ID != 0 {
			return ErrInvalidClusterEnvelope
		}
		var err error
		payload, err = encodeCoreResolveName(envelope.Payload)
		if err != nil {
			return err
		}
	} else {
		var err error
		payload, err = c.codec.Encode(envelope.Command, false, envelope.Payload)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrPayloadEncode, err)
		}
	}
	wire := WireEnvelope{
		Source:   source,
		Target:   envelope.Target,
		Session:  envelope.Session,
		Command:  envelope.Command,
		Payload:  payload,
		CallPath: append([]ServiceRef(nil), envelope.CallPath...),
	}
	if err := c.transport.Send(envelope.Target.Node, wire); err != nil {
		c.runtime.metrics.Inc("cluster_send_errors_total")
		c.runtime.logger.Error("cluster send failed", "node", envelope.Target.Node, "error", err)
		return ErrRemoteUnavailable
	}
	c.runtime.metrics.Inc("cluster_messages_sent_total")
	return nil
}

func (c *clusterRuntime) reply(responder, caller ServiceRef, command CommandID, session SessionID, value any, replyErr error) error {
	wire := WireEnvelope{
		Source:   responder,
		Target:   caller,
		Session:  session,
		Command:  command,
		Response: true,
	}
	resultErr := replyErr
	if resultErr != nil {
		wire.ErrorCode, wire.ErrorMessage = encodeRemoteError(resultErr)
	} else {
		var payload []byte
		var err error
		if responder.ID == 0 && command == coreResolveNameCommand {
			payload, err = encodeCoreResolveResponse(value, c.runtime.node)
		} else {
			payload, err = c.codec.Encode(command, true, value)
		}
		if err != nil {
			if errors.Is(err, ErrInvalidClusterEnvelope) {
				resultErr = err
			} else {
				resultErr = fmt.Errorf("%w: %v", ErrPayloadEncode, err)
			}
			wire.ErrorCode, wire.ErrorMessage = encodeRemoteError(resultErr)
			c.runtime.metrics.Inc("cluster_encode_errors_total")
		} else {
			wire.Payload = payload
		}
	}
	if err := c.transport.Send(caller.Node, wire); err != nil {
		c.runtime.metrics.Inc("cluster_reply_errors_total")
		c.runtime.logger.Error("cluster reply failed", "node", caller.Node, "error", err)
		return ErrRemoteUnavailable
	}
	c.runtime.metrics.Inc("cluster_messages_sent_total")
	return resultErr
}

func (c *clusterRuntime) receive(peer NodeID, wire WireEnvelope) {
	c.runtime.metrics.Inc("cluster_messages_received_total")
	if err := c.validate(peer, wire); err != nil {
		c.runtime.metrics.Inc("cluster_invalid_envelopes_total")
		c.runtime.logger.Error("invalid cluster envelope", "node", peer, "error", err)
		if errors.Is(err, ErrRuntimeClosed) {
			return
		}
		if !wire.Response && wire.Session != 0 && wire.Source.Node == peer && wire.Target.Node == c.runtime.node && (wire.Target.ID != 0 || wire.Command == coreResolveNameCommand) {
			_ = c.reply(wire.Target, wire.Source, wire.Command, wire.Session, nil, err)
		}
		return
	}
	if wire.Response {
		c.receiveReply(wire)
		return
	}
	c.receiveCommand(wire)
}

func (c *clusterRuntime) validate(peer NodeID, wire WireEnvelope) error {
	if c.runtime.state.Load() != runtimeRunning {
		return ErrRuntimeClosed
	}
	if peer == "" || wire.Source.Node != peer || wire.Target.Node != c.runtime.node {
		return ErrInvalidClusterEnvelope
	}
	if wire.Response {
		if wire.Session == 0 || len(wire.CallPath) != 0 || (wire.ErrorCode == "" && wire.ErrorMessage != "") || (wire.ErrorCode != "" && len(wire.Payload) != 0) {
			return ErrInvalidClusterEnvelope
		}
		if wire.Source.ID == 0 && (wire.Command != coreResolveNameCommand || wire.Target.ID != 0) {
			return ErrInvalidClusterEnvelope
		}
		if wire.Source.ID == 0 && wire.ErrorCode == "" && len(wire.Payload) != 8 {
			return ErrInvalidClusterEnvelope
		}
		return nil
	}
	if wire.ErrorCode != "" || wire.ErrorMessage != "" {
		return ErrInvalidClusterEnvelope
	}
	if wire.Target.ID == 0 {
		if wire.Command != coreResolveNameCommand || wire.Session == 0 || wire.Source.ID != 0 || len(wire.Payload) > maxCoreServiceNameLength {
			return ErrInvalidClusterEnvelope
		}
		if len(wire.CallPath) == 0 || wire.CallPath[len(wire.CallPath)-1] != wire.Target {
			return ErrInvalidClusterEnvelope
		}
		return nil
	}
	if wire.Session == 0 {
		if len(wire.CallPath) != 0 {
			return ErrInvalidClusterEnvelope
		}
		return nil
	}
	if len(wire.CallPath) == 0 || wire.CallPath[len(wire.CallPath)-1] != wire.Target {
		return ErrInvalidClusterEnvelope
	}
	return nil
}

func (c *clusterRuntime) receiveCommand(wire WireEnvelope) {
	if wire.Target.ID == 0 {
		name, err := decodeCoreResolveName(wire.Payload)
		if err != nil {
			_ = c.reply(wire.Target, wire.Source, wire.Command, wire.Session, nil, err)
			return
		}
		ref, err := c.runtime.registry.resolve(name)
		_ = c.reply(wire.Target, wire.Source, wire.Command, wire.Session, ref, err)
		return
	}
	payload, err := c.codec.Decode(wire.Command, false, wire.Payload)
	if err != nil {
		err = fmt.Errorf("%w: %v", ErrPayloadDecode, err)
		c.runtime.metrics.Inc("cluster_decode_errors_total")
		if wire.Session != 0 {
			_ = c.reply(wire.Target, wire.Source, wire.Command, wire.Session, nil, err)
		}
		return
	}
	envelope := Envelope{
		Source:   wire.Source,
		Target:   wire.Target,
		Session:  wire.Session,
		Command:  wire.Command,
		Payload:  payload,
		CallPath: append([]ServiceRef(nil), wire.CallPath...),
	}
	if err := c.runtime.sendLocalEnvelope(envelope); err != nil {
		c.runtime.metrics.Inc("cluster_delivery_errors_total")
		if wire.Session != 0 {
			_ = c.reply(wire.Target, wire.Source, wire.Command, wire.Session, nil, err)
		}
	}
}

func (c *clusterRuntime) receiveReply(wire WireEnvelope) {
	call := c.runtime.pending.take(wire.Target, wire.Source, wire.Command, wire.Session)
	if call == nil {
		c.runtime.metrics.Inc("late_reply_total")
		return
	}
	var result callResult
	if wire.ErrorCode != "" {
		result.err = decodeRemoteError(wire.ErrorCode, wire.ErrorMessage)
	} else if wire.Source.ID == 0 && wire.Command == coreResolveNameCommand {
		value, err := decodeCoreResolveResponse(wire.Payload, wire.Source.Node)
		if err != nil {
			result.err = err
		} else {
			result.value = value
		}
	} else {
		value, err := c.codec.Decode(wire.Command, true, wire.Payload)
		if err != nil {
			result.err = fmt.Errorf("%w: %v", ErrPayloadDecode, err)
			c.runtime.metrics.Inc("cluster_decode_errors_total")
		} else {
			result.value = value
		}
	}
	call.result <- result
}

func (c *clusterRuntime) unavailable(peer NodeID) {
	if peer == "" {
		return
	}
	c.runtime.metrics.Inc("cluster_node_unavailable_total")
	c.runtime.pending.failNode(peer, ErrRemoteUnavailable)
}

func encodeRemoteError(err error) (string, string) {
	for _, candidate := range stableRemoteErrors {
		if errors.Is(err, candidate.err) {
			return candidate.code, ""
		}
	}
	message := err.Error()
	if len(message) > maxRemoteErrorMessageLength {
		message = message[:maxRemoteErrorMessageLength]
	}
	return "remote", message
}

func decodeRemoteError(code, message string) error {
	if err := stableRemoteErrorsByCode[code]; err != nil {
		return err
	}
	return &RemoteError{Code: code, Message: message}
}
