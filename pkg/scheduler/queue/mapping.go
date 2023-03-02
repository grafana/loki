package queue

var empty = string([]byte{byte(0)})

// Mapping is a map-like data structure that allows accessing its items not
// only by key but also by index.
// When an item is removed, the iinternal key array is not resized, but the
// removed place is marked as empty. This allows to remove keys without
// changing the index of the remaining items after the removed key.
// Mapping uses *tenantQueue as concrete value and keys of type string.
// The data structure is not thread-safe.
// TODO(chaudum): Implement QueueMapping using generics
type Mapping struct {
	m     map[string]*tenantQueue
	keys  []string
	empty []int
}

func (om *Mapping) Init(size int) {
	om.m = make(map[string]*tenantQueue, size)
	om.keys = make([]string, 0, size)
	om.empty = make([]int, 0, size)
}

func (om *Mapping) Put(key string, value *tenantQueue) bool {
	// do not allow empty string or 0 byte string as key
	if key == "" || key == empty {
		return false
	}
	if len(om.empty) == 0 {
		value.index = len(om.keys)
		om.keys = append(om.keys, key)
	} else {
		idx := om.empty[0]
		om.empty = om.empty[1:]
		om.keys[idx] = key
		value.index = idx
	}
	om.m[key] = value
	return true
}

func (om *Mapping) Get(idx QueueIndex) *tenantQueue { //nolint:revive
	if len(om.keys) == 0 {
		return nil
	}
	k := om.keys[idx]
	return om.GetByKey(k)
}

func (om *Mapping) GetNext(idx QueueIndex) *tenantQueue { //nolint:revive
	if len(om.keys) == 0 {
		return nil
	}

	// convert to int
	i := int(idx)
	// proceed to the next index
	i = i + 1
	// start from beginning if next index exceeds slice length
	if i >= len(om.keys) {
		i = 0
	}

	for i < len(om.keys) {
		k := om.keys[i]
		if k != empty {
			return om.GetByKey(k)
		}
		i++
	}
	return nil
}

func (om *Mapping) GetByKey(key string) *tenantQueue { //nolint:revive
	// do not allow empty string or 0 byte string as key
	if key == "" || key == empty {
		return nil
	}
	return om.m[key]
}

func (om *Mapping) Remove(key string) bool {
	e := om.m[key]
	if e == nil {
		return false
	}
	delete(om.m, key)
	om.keys[e.index] = empty
	om.empty = append(om.empty, e.index)
	return true
}

func (om *Mapping) Keys() []string {
	return om.keys
}

func (om *Mapping) Values() []*tenantQueue { //nolint:revive
	values := make([]*tenantQueue, 0, len(om.keys))
	for _, k := range om.keys {
		if k == empty {
			continue
		}
		values = append(values, om.m[k])
	}
	return values
}

func (om *Mapping) Len() int {
	return len(om.keys) - len(om.empty)
}
