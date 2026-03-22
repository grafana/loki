package policyutil

// GetReferenceCount gets reference count from cache value.
func GetReferenceCount(v any) int {
	if getter, ok := v.(interface{ GetReferenceCount() int }); ok {
		return getter.GetReferenceCount()
	}
	return 1
}
