package slices

func Contains[T comparable](haystack []T, needle T) bool {
	for _, e := range haystack {
		if e == needle {
			return true
		}
	}

	return false
}
