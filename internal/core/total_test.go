package core

// deref reads a total of a result in a test: -1 when the collector did not
// count it.
func deref(total *int) int {
	if total == nil {
		return -1
	}
	return *total
}
