package supervisor

import (
	"errors"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const (
	registerCommand gsr.CommandID = 0x02000301 + iota
	getCommand
	failureCommand
	recoveryStartedCommand
	recoveryPreparedCommand
	recoveryCommittedCommand
	recoveryFailedCommand
)

type registerRequest struct{ Registration Registration }
type getRequest struct{ Key ServiceKey }
type recoveryStartedRequest struct{ Task RecoveryTask }
type recoveryPreparedRequest struct {
	Task RecoveryTask
	Ref  gsr.ServiceRef
}
type recoveryCommittedRequest struct {
	Task RecoveryTask
	Ref  gsr.ServiceRef
}
type recoveryFailedRequest struct {
	Task    RecoveryTask
	Failure RecoveryFailure
}

type responseError uint8

const (
	responseOK responseError = iota
	responseInvalidConfig
	responseInvalidKey
	responseInvalidPolicy
	responseInvalidRegistration
	responseAlreadyRegistered
	responseServiceNotRegistered
	responseInvalidNotice
	responseDuplicateNotice
	responseStaleNotice
	responseRestartSuppressed
	responseRecoveryQueueFull
	responseRunnerClosed
	responseSnapshotNotFound
	responseRecoveryFailed
	responseCreateFailed
	responseNamePublishFailed
	responseAbortFailed
	responseStaleRecovery
)

type operationResponse struct{ Error responseError }
type recordResponse struct {
	Record Record
	Error  responseError
}

func responseFromError(err error) responseError {
	switch {
	case err == nil:
		return responseOK
	case errors.Is(err, ErrInvalidConfig):
		return responseInvalidConfig
	case errors.Is(err, ErrInvalidKey):
		return responseInvalidKey
	case errors.Is(err, ErrInvalidPolicy):
		return responseInvalidPolicy
	case errors.Is(err, ErrInvalidRegistration):
		return responseInvalidRegistration
	case errors.Is(err, ErrAlreadyRegistered):
		return responseAlreadyRegistered
	case errors.Is(err, ErrServiceNotRegistered):
		return responseServiceNotRegistered
	case errors.Is(err, ErrInvalidNotice):
		return responseInvalidNotice
	case errors.Is(err, ErrDuplicateNotice):
		return responseDuplicateNotice
	case errors.Is(err, ErrStaleNotice):
		return responseStaleNotice
	case errors.Is(err, ErrRestartSuppressed):
		return responseRestartSuppressed
	case errors.Is(err, ErrRecoveryQueueFull):
		return responseRecoveryQueueFull
	case errors.Is(err, ErrRunnerClosed):
		return responseRunnerClosed
	case errors.Is(err, ErrSnapshotNotFound):
		return responseSnapshotNotFound
	case errors.Is(err, ErrCreateFailed):
		return responseCreateFailed
	case errors.Is(err, ErrNamePublishFailed):
		return responseNamePublishFailed
	case errors.Is(err, ErrAbortFailed):
		return responseAbortFailed
	case errors.Is(err, ErrStaleRecovery):
		return responseStaleRecovery
	default:
		return responseRecoveryFailed
	}
}

func errorFromResponse(code responseError) error {
	switch code {
	case responseOK:
		return nil
	case responseInvalidConfig:
		return ErrInvalidConfig
	case responseInvalidKey:
		return ErrInvalidKey
	case responseInvalidPolicy:
		return ErrInvalidPolicy
	case responseInvalidRegistration:
		return ErrInvalidRegistration
	case responseAlreadyRegistered:
		return ErrAlreadyRegistered
	case responseServiceNotRegistered:
		return ErrServiceNotRegistered
	case responseInvalidNotice:
		return ErrInvalidNotice
	case responseDuplicateNotice:
		return ErrDuplicateNotice
	case responseStaleNotice:
		return ErrStaleNotice
	case responseRestartSuppressed:
		return ErrRestartSuppressed
	case responseRecoveryQueueFull:
		return ErrRecoveryQueueFull
	case responseRunnerClosed:
		return ErrRunnerClosed
	case responseSnapshotNotFound:
		return ErrSnapshotNotFound
	case responseCreateFailed:
		return ErrCreateFailed
	case responseNamePublishFailed:
		return ErrNamePublishFailed
	case responseAbortFailed:
		return ErrAbortFailed
	case responseStaleRecovery:
		return ErrStaleRecovery
	default:
		return ErrRecoveryFailed
	}
}
