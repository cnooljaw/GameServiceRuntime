package supervisor

import (
	"reflect"
	"strings"
	"unicode/utf8"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func validateRunnerConfig(config RunnerConfig) error {
	if config.Workers < 1 || config.QueueSize < 1 || config.AttemptTimeout <= 0 || config.ResultTimeout <= 0 || config.ResultRetryInterval <= 0 || (config.Logger != nil && isNil(config.Logger)) {
		return ErrInvalidConfig
	}
	return nil
}

func validateRecoveryTask(task RecoveryTask) error {
	if validateConcreteRef(task.Supervisor) != nil || validateServiceKey(task.Key) != nil || validateConcreteRef(task.FailedRef) != nil || task.FailedRef.Node != task.Supervisor.Node || task.Generation == 0 || task.Attempt == 0 || task.Delay < 0 {
		return ErrInvalidConfig
	}
	return nil
}

func validateLaunchRequest(request LaunchRequest) error {
	if validateConcreteRef(request.Supervisor) != nil || validateServiceKey(request.Key) != nil || validateConcreteRef(request.FailedRef) != nil || request.FailedRef.Node != request.Supervisor.Node || request.Generation == 0 || request.Attempt == 0 {
		return ErrInvalidConfig
	}
	return nil
}

func validateRegistration(registration Registration) error {
	if err := validateServiceKey(registration.Key); err != nil {
		return err
	}
	if validateConcreteRef(registration.Ref) != nil || registration.Generation == 0 {
		return ErrInvalidRegistration
	}
	return validateRestartPolicy(registration.Policy)
}

func validateFailureNotice(notice FailureNotice) error {
	if validateServiceKey(notice.Key) != nil || validateConcreteRef(notice.FailedRef) != nil || notice.Generation == 0 || notice.OccurredAt.IsZero() || notice.Kind != FailureHandlerPanic {
		return ErrInvalidNotice
	}
	return nil
}

func validateRecord(record Record, key ServiceKey) error {
	if record.Registration.Key != key || validateRegistration(record.Registration) != nil || record.Status < ServiceRunning || record.Status > ServiceRestartSuppressed || record.AttemptsInFault < 0 || record.RestartsInWindow < 0 {
		return ErrInvalidResponse
	}
	return nil
}

func validateServiceKey(key ServiceKey) error {
	if !validText(key.Namespace) || !validText(key.ID) {
		return ErrInvalidKey
	}
	return nil
}

func validateConcreteRef(ref gsr.ServiceRef) error {
	if ref.Node == "" || ref.ID == 0 {
		return ErrInvalidRegistration
	}
	return nil
}

func validateRestartPolicy(policy RestartPolicy) error {
	switch policy.Strategy {
	case RestartNever, DestroyOnFailure:
		if policy.MaxAttempts != 0 || policy.MaxRestarts != 0 || policy.Window != 0 || policy.MinBackoff != 0 || policy.MaxBackoff != 0 {
			return ErrInvalidPolicy
		}
		return nil
	case RestartOnFailure:
		if policy.MaxAttempts < 1 || policy.MaxRestarts < 1 || policy.Window <= 0 || policy.MinBackoff <= 0 || policy.MaxBackoff < policy.MinBackoff {
			return ErrInvalidPolicy
		}
		return nil
	default:
		return ErrInvalidPolicy
	}
}

func validText(value string) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) != ""
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
