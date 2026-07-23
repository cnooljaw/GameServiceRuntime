package servicegroup

import (
	"reflect"
	"sort"
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

func validNode(node gsr.NodeID) bool {
	return strings.TrimSpace(string(node)) != ""
}

func validGroup(name GroupName) bool {
	value := string(name)
	return value != "" && strings.TrimSpace(value) == value
}

func validServiceRef(ref gsr.ServiceRef) bool {
	return validNode(ref.Node) && ref.ID != 0
}

func validExpectedVersion(version ServiceSetVersion) bool {
	return version == (ServiceSetVersion{}) || validVersion(version)
}

func validVersion(version ServiceSetVersion) bool {
	return version.AuthorityEpoch != 0 && version.Revision != 0
}

func validTags(tags map[string]string) bool {
	for key := range tags {
		if key == "" || strings.TrimSpace(key) != key {
			return false
		}
	}
	return true
}

func normalizeServiceSet(name GroupName, refs []gsr.ServiceRef, tags map[string]string) (ServiceSet, error) {
	if !validGroup(name) {
		return ServiceSet{}, ErrInvalidGroup
	}
	canonicalRefs := append([]gsr.ServiceRef(nil), refs...)
	for _, ref := range canonicalRefs {
		if !validServiceRef(ref) {
			return ServiceSet{}, ErrInvalidServiceSet
		}
	}
	if !validTags(tags) {
		return ServiceSet{}, ErrInvalidServiceSet
	}
	sort.Slice(canonicalRefs, func(left, right int) bool {
		if canonicalRefs[left].Node != canonicalRefs[right].Node {
			return canonicalRefs[left].Node < canonicalRefs[right].Node
		}
		return canonicalRefs[left].ID < canonicalRefs[right].ID
	})
	unique := make([]gsr.ServiceRef, 0, len(canonicalRefs))
	for _, ref := range canonicalRefs {
		if len(unique) == 0 || unique[len(unique)-1] != ref {
			unique = append(unique, ref)
		}
	}
	if unique == nil {
		unique = make([]gsr.ServiceRef, 0)
	}
	return ServiceSet{Name: name, Refs: unique, Tags: cloneTags(tags)}, nil
}

func validServiceSet(set ServiceSet) bool {
	if !validGroup(set.Name) || !validVersion(set.Version) || set.Refs == nil || set.Tags == nil || !validTags(set.Tags) {
		return false
	}
	for index, ref := range set.Refs {
		if !validServiceRef(ref) {
			return false
		}
		if index > 0 {
			previous := set.Refs[index-1]
			if previous.Node > ref.Node || (previous.Node == ref.Node && previous.ID >= ref.ID) {
				return false
			}
		}
	}
	return true
}

func validWireServiceSet(set wireServiceSet) bool {
	return validServiceSet(set.serviceSet())
}

func sameServiceSetContent(left, right ServiceSet) bool {
	if left.Name != right.Name || len(left.Refs) != len(right.Refs) || len(left.Tags) != len(right.Tags) {
		return false
	}
	for index := range left.Refs {
		if left.Refs[index] != right.Refs[index] {
			return false
		}
	}
	for key, value := range left.Tags {
		if right.Tags[key] != value {
			return false
		}
	}
	return true
}
