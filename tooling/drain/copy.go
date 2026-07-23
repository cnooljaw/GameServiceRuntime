package drain

func cloneLease(lease VisitorLease) VisitorLease {
	return lease
}

func cloneVisitorRefs(visitors []VisitorRef) []VisitorRef {
	result := make([]VisitorRef, len(visitors))
	copy(result, visitors)
	return result
}
