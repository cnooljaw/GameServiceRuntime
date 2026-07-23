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

func validNodeConfig(config NodeConfig) bool {
	return validNode(config.ID) && strings.TrimSpace(config.Address) != ""
}

func validTarget(target NodeTarget) bool {
	if !validNodeConfig(target.Config) {
		return false
	}
	return !target.Config.Enabled || validAgent(target.Config.ID, target.Agent)
}
