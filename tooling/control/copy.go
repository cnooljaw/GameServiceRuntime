package control

import "github.com/lijiawang/GameServiceRuntime/tooling/monitor"

func cloneReport(report monitor.Report) monitor.Report {
	copy := report
	copy.Services = append([]monitor.ServiceReport(nil), report.Services...)
	copy.Tasks = append([]monitor.TaskReport(nil), report.Tasks...)
	copy.Metrics.Counters = copyUint64Map(report.Metrics.Counters)
	copy.Metrics.Gauges = copyInt64Map(report.Metrics.Gauges)
	copy.Metrics.DurationsNanos = copyInt64Map(report.Metrics.DurationsNanos)
	return copy
}

func copyUint64Map(source map[string]uint64) map[string]uint64 {
	copy := make(map[string]uint64, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}

func copyInt64Map(source map[string]int64) map[string]int64 {
	copy := make(map[string]int64, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}
