package queue

// OrderedMap is a map-like data structure that allows accessing its items not
// only by key but also by index.
// It preserves the ingest order of times for iterating over the values.
// Keys are only of type string, values are of any type.
// The data structure is not thread-safe.
// TODO(chaudum): Implement QueueMapping using generics
type OrderedMap struct {
	// m is the loopup table from key to item
	m map[string]any
	// keys is a slice of keys which conserves ingest order
	keys []string
}

func (om *OrderedMap) Init(size int) {
	om.m = make(map[string]any, size)
	om.keys = make([]string, 0, size)
}

func (om *OrderedMap) Put(key string, value any) int {
	if key == "" {
		return len(om.keys)
	}
	om.keys = append(om.keys, key)
	om.m[key] = value
	return len(om.keys)
}

func (om *OrderedMap) Get(idx int) any {
	if len(om.keys) == 0 {
		return nil
	}
	k := om.keys[idx]
	return om.GetByKey(k)
}

func (om *OrderedMap) GetByKey(key string) any {
	if key == "" {
		return nil
	}
	return om.m[key]
}

func (om *OrderedMap) Remove(key string) any {
	for idx, k := range om.keys {
		if k == key {
			om.keys = append(om.keys[:idx], om.keys[idx+1:]...)
			delete(om.m, key)
			break
		}
	}
	return nil
}

func (om *OrderedMap) Keys() []string {
	return om.keys
}

func (om *OrderedMap) Values() []any {
	values := make([]any, 0, len(om.keys))
	for _, k := range om.keys {
		values = append(values, om.m[k])
	}
	return values
}

func (om *OrderedMap) Len() int {
	return len(om.keys)
}

// QueueMapping is a concrete implementation of the OrderedMap
// to avoid casting the return values.
type QueueMapping struct {
	m    map[string]*tenantQueue
	keys []string
}

func (om *QueueMapping) Init(size int) {
	om.m = make(map[string]*tenantQueue, size)
	om.keys = make([]string, 0, size)
}

func (om *QueueMapping) Put(key string, value *tenantQueue) int {
	if key == "" {
		return len(om.keys)
	}
	om.keys = append(om.keys, key)
	om.m[key] = value
	return len(om.keys)
}

func (om *QueueMapping) Get(idx QueueIndex) *tenantQueue {
	if len(om.keys) == 0 {
		return nil
	}
	k := om.keys[idx]
	return om.GetByKey(k)
}

func (om *QueueMapping) GetByKey(key string) *tenantQueue {
	if key == "" {
		return nil
	}
	return om.m[key]
}

func (om *QueueMapping) Remove(key string) *tenantQueue {
	for idx, k := range om.keys {
		if k == key {
			om.keys = append(om.keys[:idx], om.keys[idx+1:]...)
			delete(om.m, key)
			break
		}
	}
	return nil
}

func (om *QueueMapping) Keys() []string {
	return om.keys
}

func (om *QueueMapping) Values() []*tenantQueue {
	values := make([]*tenantQueue, 0, len(om.keys))
	for _, k := range om.keys {
		values = append(values, om.m[k])
	}
	return values
}

func (om *QueueMapping) Len() int {
	return len(om.keys)
}
