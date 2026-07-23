package control

import (
	"reflect"
	"strings"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

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

func validNode(node gsr.NodeID) bool { return strings.TrimSpace(string(node)) != "" }

func validAgent(node gsr.NodeID, agent gsr.ServiceRef) bool {
	return agent.Node == node && agent.ID != 0
}

func validDesired(desired NodeDesiredState) bool {
	return validNode(desired.ID) && strings.TrimSpace(desired.Address) != ""
}

func validTarget(target NodeTarget) bool {
	if !validDesired(target.Desired) {
		return false
	}
	return !target.Desired.Enabled || validAgent(target.Desired.ID, target.Agent)
}
