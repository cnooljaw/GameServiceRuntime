package drain

import (
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"

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

func validNode(node gsr.NodeID) bool {
	value := string(node)
	return utf8.ValidString(value) && strings.TrimSpace(value) != ""
}

func validServiceRef(ref gsr.ServiceRef) bool {
	return validNode(ref.Node) && ref.ID != 0
}

func validLease(lease VisitorLease) bool {
	return validServiceRef(lease.Target) &&
		validServiceRef(lease.Visitor) &&
		lease.AuthorityEpoch != 0 &&
		lease.Generation != 0 &&
		!lease.ExpiresAt.IsZero()
}

func validWireLease(lease wireVisitorLease) bool {
	return validLease(lease.visitorLease())
}

func validVisitorRef(visitor VisitorRef) bool {
	return validServiceRef(visitor.Visitor) &&
		visitor.Generation != 0 &&
		!visitor.ExpiresAt.IsZero()
}

func validWireVisitorRef(visitor wireVisitorRef) bool {
	return validVisitorRef(visitor.visitorRef())
}

func validVisitorRefs(visitors []VisitorRef) bool {
	if visitors == nil {
		return false
	}
	for index, visitor := range visitors {
		if !validVisitorRef(visitor) {
			return false
		}
		if index > 0 {
			previous := visitors[index-1].Visitor
			current := visitor.Visitor
			if previous.Node > current.Node || (previous.Node == current.Node && previous.ID >= current.ID) {
				return false
			}
		}
	}
	return true
}

func validWireVisitorRefs(visitors []wireVisitorRef) bool {
	if visitors == nil {
		return false
	}
	decoded := make([]VisitorRef, len(visitors))
	for index, visitor := range visitors {
		decoded[index] = visitor.visitorRef()
	}
	return validVisitorRefs(decoded)
}

func sortVisitorRefs(visitors []VisitorRef) {
	sort.Slice(visitors, func(left, right int) bool {
		if visitors[left].Visitor.Node != visitors[right].Visitor.Node {
			return visitors[left].Visitor.Node < visitors[right].Visitor.Node
		}
		return visitors[left].Visitor.ID < visitors[right].Visitor.ID
	})
}

func sameLease(left, right VisitorLease) bool {
	return left.Target == right.Target &&
		left.Visitor == right.Visitor &&
		left.AuthorityEpoch == right.AuthorityEpoch &&
		left.Generation == right.Generation &&
		left.Weak == right.Weak &&
		left.ExpiresAt.Equal(right.ExpiresAt)
}
