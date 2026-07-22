package supervisor

import (
	"context"
	"errors"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestClientRegistersAndReturnsIndependentRecord(t *testing.T) {
	caller := &supervisorFakeCaller{reply: operationResponse{}}
	client, err := NewClient(caller, gsr.ServiceRef{Node: "node-a", ID: 1})
	if err != nil {
		t.Fatal(err)
	}
	registration := testRegistration(RestartNever)
	if err := client.Register(context.Background(), registration); err != nil {
		t.Fatal(err)
	}
	if caller.command != registerCommand {
		t.Fatalf("command = %v, want registerCommand", caller.command)
	}
	if got := caller.payload.(registerRequest).Registration; got != registration {
		t.Fatalf("registration = %#v, want %#v", got, registration)
	}

	want := Record{Registration: registration, Status: ServiceRunning}
	caller.reply = recordResponse{Record: want}
	record, err := client.Get(context.Background(), registration.Key)
	if err != nil {
		t.Fatal(err)
	}
	if record != want {
		t.Fatalf("record = %#v, want %#v", record, want)
	}
	record.Registration.Key.ID = "changed"
	record, err = client.Get(context.Background(), registration.Key)
	if err != nil {
		t.Fatal(err)
	}
	if record.Registration.Key != registration.Key {
		t.Fatalf("record leaked mutation: %#v", record)
	}
}

func TestClientRejectsInvalidDependenciesRequestsAndResponses(t *testing.T) {
	var typedNil *supervisorFakeCaller
	for _, caller := range []CommandCaller{nil, typedNil} {
		if _, err := NewClient(caller, gsr.ServiceRef{Node: "node-a", ID: 1}); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("NewClient(%T) error = %v, want ErrInvalidConfig", caller, err)
		}
	}
	if _, err := NewClient(&supervisorFakeCaller{}, gsr.ServiceRef{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewClient zero target error = %v, want ErrInvalidConfig", err)
	}

	caller := &supervisorFakeCaller{reply: "wrong"}
	client, err := NewClient(caller, gsr.ServiceRef{Node: "node-a", ID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Register(context.Background(), testRegistration(RestartNever)); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Register error = %v, want ErrInvalidResponse", err)
	}
	caller.reply = recordResponse{Error: responseServiceNotRegistered}
	if _, err := client.Get(context.Background(), testServiceKey()); !errors.Is(err, ErrServiceNotRegistered) {
		t.Fatalf("Get error = %v, want ErrServiceNotRegistered", err)
	}
	invalidRegistration := testRegistration(RestartNever)
	invalidRegistration.Ref = gsr.ServiceRef{}
	if err := client.Register(context.Background(), invalidRegistration); !errors.Is(err, ErrInvalidRegistration) {
		t.Fatalf("invalid Register error = %v, want ErrInvalidRegistration", err)
	}
	invalidPolicy := testRegistration(RestartOnFailure)
	invalidPolicy.Policy.MaxAttempts = 0
	if err := client.Register(context.Background(), invalidPolicy); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("invalid policy error = %v, want ErrInvalidPolicy", err)
	}
	if _, err := client.Get(context.Background(), ServiceKey{}); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("invalid Get error = %v, want ErrInvalidKey", err)
	}
	var nilContext context.Context
	if err := client.Register(nilContext, testRegistration(RestartNever)); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("nil context Register error = %v, want ErrInvalidContext", err)
	}
	if _, err := client.Get(nilContext, testServiceKey()); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("nil context Get error = %v, want ErrInvalidContext", err)
	}
	caller.reply = recordResponse{Record: Record{Registration: testRegistration(RestartNever), Status: ServiceRunning, LastFailure: RecoveryFailure(99)}}
	if _, err := client.Get(context.Background(), testServiceKey()); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("invalid failure category error = %v, want ErrInvalidResponse", err)
	}
}

type supervisorFakeCaller struct {
	reply   any
	err     error
	target  gsr.ServiceRef
	command gsr.CommandID
	payload any
}

func (c *supervisorFakeCaller) Call(_ context.Context, target gsr.ServiceRef, command gsr.CommandID, payload any) (any, error) {
	c.target = target
	c.command = command
	c.payload = payload
	return c.reply, c.err
}

func testServiceKey() ServiceKey { return ServiceKey{Namespace: "player", ID: "42"} }

func testRestartPolicy() RestartPolicy {
	return RestartPolicy{
		Strategy: RestartOnFailure, MaxAttempts: 3, MaxRestarts: 2,
		Window: time.Minute, MinBackoff: 10 * time.Millisecond, MaxBackoff: 40 * time.Millisecond,
	}
}

func testRegistration(strategy RestartStrategy) Registration {
	policy := RestartPolicy{Strategy: strategy}
	if strategy == RestartOnFailure {
		policy = testRestartPolicy()
	}
	return Registration{
		Key: testServiceKey(), Ref: gsr.ServiceRef{Node: "node-a", ID: 7}, Generation: 1, Policy: policy,
	}
}
