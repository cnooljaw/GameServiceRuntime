package main

import (
	"context"
	"errors"
	"fmt"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const deliverGameOutputBatchCommand gsr.CommandID = 0x0410f00b

var (
	errInvalidOutputServiceConfig = errors.New("nhsk: invalid GameOutputService config")
	errInvalidGameOutputBatch     = errors.New("nhsk: invalid GameOutputBatch")
	errOutputGenerationMismatch   = errors.New("nhsk: output connection generation mismatch")
)

type gameOutputSink interface {
	Submit(GameOutputBatch) error
}

type gameOutputService struct {
	generation ConnectionGeneration
	sink       gameOutputSink
	reporter   ConnectionFailureReporter
}

func newGameOutputServiceSpec(
	generation ConnectionGeneration,
	sink gameOutputSink,
	reporter ConnectionFailureReporter,
) (gsr.ServiceSpec, error) {
	if generation == 0 || sink == nil || reporter == nil {
		return gsr.ServiceSpec{}, errInvalidOutputServiceConfig
	}
	return gsr.ServiceSpec{
		Service: &gameOutputService{generation: generation, sink: sink, reporter: reporter},
		Policy:  gsr.ServicePolicy{Mailbox: gsr.DiscardMailbox},
	}, nil
}

func (*gameOutputService) Init(serviceContext gsr.ServiceContext) error {
	if serviceContext == nil {
		return errInvalidOutputServiceConfig
	}
	return nil
}

func (service *gameOutputService) Handle(_ gsr.CommandContext, command gsr.Command) error {
	if command.ID != deliverGameOutputBatchCommand {
		return gsr.ErrUnknownCommand
	}
	batch, ok := command.Payload.(GameOutputBatch)
	if !ok || !validGameOutputBatch(batch) {
		return errInvalidGameOutputBatch
	}
	if batch.ConnectionGeneration != service.generation {
		return errOutputGenerationMismatch
	}
	if err := service.sink.Submit(batch); err != nil {
		service.reporter.FailConnection(service.generation, ConnectionFailureOutputSinkRejected)
		return fmt.Errorf("nhsk: submit GameOutputBatch: %w", err)
	}
	return nil
}

func (*gameOutputService) Stop(context.Context) error { return nil }

func (service *gameOutputService) Close() error {
	service.sink = nil
	service.reporter = nil
	return nil
}

func validGameOutputBatch(batch GameOutputBatch) bool {
	if batch.BattleID == 0 || batch.MatchID == 0 || batch.ProductID == 0 || batch.Ref.Node == "" || batch.Ref.ID == 0 || batch.ConnectionGeneration == 0 || len(batch.Outputs) == 0 {
		return false
	}
	for _, output := range batch.Outputs {
		if output == nil {
			return false
		}
	}
	return true
}
