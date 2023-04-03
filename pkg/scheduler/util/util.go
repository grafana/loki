package util

type IntPointerMap map[string]*int

func (m IntPointerMap) Inc(key string) int {
	ptr, ok := m[key]
	if !ok {
		size := 1
		m[key] = &size
		return size
	}
	(*ptr)++
	return *ptr
}

func (m IntPointerMap) Dec(key string) int {
	ptr, ok := m[key]
	if !ok {
		return 0
	}
	(*ptr)--
	if *ptr == 0 {
		delete(m, key)
	}
	return *ptr
}
