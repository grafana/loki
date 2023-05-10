package queue

import (
	"github.com/pkg/errors"
)

var ErrOutOfBounds = errors.New("queue index out of bounds")

var empty = string([]byte{byte(0)})

// Mapping is a map-like data structure that allows accessing its items not
// only by key but also by index.
// When an item is removed, the internal key array is not resized, but the
// removed place is marked as empty. This allows to remove keys without
// changing the index of the remaining items after the removed key.
// Mapping uses *tenantQueue as concrete value and keys of type string.
// The data structure is not thread-safe.
type Mapping[v Mapable] struct {
	m     map[string]v
	keys  []string
	empty []QueueIndex
}

func (m *Mapping[v]) Init(size int) {
	m.m = make(map[string]v, size)
	m.keys = make([]string, 0, size)
	m.empty = make([]QueueIndex, 0, size)
}

func (m *Mapping[v]) Put(key string, value v) bool {
	// do not allow empty string or 0 byte string as key
	if key == "" || key == empty {
		return false
	}
	if len(m.empty) == 0 {
		value.SetPos(QueueIndex(len(m.keys)))
		m.keys = append(m.keys, key)
	} else {
		idx := m.empty[0]
		m.empty = m.empty[1:]
		m.keys[idx] = key
		value.SetPos(idx)
	}
	m.m[key] = value
	return true
}

func (m *Mapping[v]) Get(idx QueueIndex) v {
	if len(m.keys) == 0 {
		return nil
	}
	k := m.keys[idx]
	return m.GetByKey(k)
}

func (m *Mapping[v]) GetNext(idx QueueIndex) (v, error) {
	if m.Len() == 0 {
		return nil, ErrOutOfBounds
	}

	i := int(idx)
	i++

	for i < len(m.keys) {
		k := m.keys[i]
		if k != empty {
			return m.GetByKey(k), nil
		}
		i++
	}
	return nil, ErrOutOfBounds
}

func (m *Mapping[v]) GetByKey(key string) v {
	// do not allow empty string or 0 byte string as key
	if key == "" || key == empty {
		return nil
	}
	return m.m[key]
}

func (m *Mapping[v]) Remove(key string) bool {
	e := m.m[key]
	if e == nil {
		return false
	}
	delete(m.m, key)
	m.keys[e.Pos()] = empty
	m.empty = append(m.empty, e.Pos())
	return true
}

func (m *Mapping[v]) Keys() []string {
	return m.keys
}

func (m *Mapping[v]) Values() []v {
	values := make([]v, 0, len(m.keys))
	for _, k := range m.keys {
		if k == empty {
			continue
		}
		values = append(values, m.m[k])
	}
	return values
}

func (m *Mapping[v]) Len() int {
	return len(m.keys) - len(m.empty)
}
