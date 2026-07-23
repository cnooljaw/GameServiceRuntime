package servicegroup

import gsr "github.com/lijiawang/GameServiceRuntime/runtime"

func cloneServiceSet(set ServiceSet) ServiceSet {
	refs := make([]gsr.ServiceRef, len(set.Refs))
	copy(refs, set.Refs)
	return ServiceSet{
		Name:    set.Name,
		Version: set.Version,
		Refs:    refs,
		Tags:    cloneTags(set.Tags),
	}
}

func cloneTags(tags map[string]string) map[string]string {
	result := make(map[string]string, len(tags))
	for key, value := range tags {
		result[key] = value
	}
	return result
}

func cloneWatchResult(result WatchResult) WatchResult {
	copy := WatchResult{Lease: result.Lease, Found: result.Found}
	if result.Found {
		copy.Current = cloneServiceSet(result.Current)
	}
	return copy
}
