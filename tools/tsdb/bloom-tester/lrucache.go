package main

import "container/list"

type LRUCache struct {
	capacity int
	cache    map[string]*list.Element
	list     *list.List
}

type Entry struct {
	key string
}

func NewLRUCache(capacity int) *LRUCache {
	return &LRUCache{
		capacity: capacity,
		cache:    make(map[string]*list.Element),
		list:     list.New(),
	}
}

func (c *LRUCache) Get(key string) bool {
	if elem, ok := c.cache[key]; ok {
		// Move the accessed element to the front of the list
		c.list.MoveToFront(elem)
		return true
	}
	return false
}

func (c *LRUCache) Put(key string) {
	if elem, ok := c.cache[key]; ok {
		// If the key already exists, move it to the front
		c.list.MoveToFront(elem)
	} else {
		// If the cache is full, remove the least recently used element
		if len(c.cache) >= c.capacity {
			// Get the least recently used element from the back of the list
			tailElem := c.list.Back()
			if tailElem != nil {
				deletedEntry := c.list.Remove(tailElem).(*Entry)
				delete(c.cache, deletedEntry.key)
			}
		}

		// Add the new key to the cache and the front of the list
		newEntry := &Entry{key}
		newElem := c.list.PushFront(newEntry)
		c.cache[key] = newElem
	}
}

func (c *LRUCache) Clear() {
	// Iterate through the list and remove all elements
	for elem := c.list.Front(); elem != nil; elem = elem.Next() {
		delete(c.cache, elem.Value.(*Entry).key)
	}

	// Clear the list
	c.list.Init()
}

type LRUCache2 struct {
	capacity int
	cache    map[string]*LRUNode2
	head     *LRUNode2
	tail     *LRUNode2
}

type LRUNode2 struct {
	key string
	//value interface{}
	prev *LRUNode2
	next *LRUNode2
}

func NewLRUCache2(capacity int) *LRUCache2 {
	return &LRUCache2{
		capacity: capacity,
		cache:    make(map[string]*LRUNode2),
	}
}

func (c *LRUCache2) Get(key string) bool {
	if node, ok := c.cache[key]; ok {
		// Move the accessed element to the front
		c.moveToFront(node)
		return true
	}
	return false
}

func (c *LRUCache2) Put(key string) {
	if node, ok := c.cache[key]; ok {
		// If the key already exists, update the value and move it to the front
		c.moveToFront(node)
	} else {
		// If the cache is full, remove the least recently used element
		if len(c.cache) >= c.capacity {
			c.removeTail()
		}

		// Add the new key to the cache and the front
		newNode := &LRUNode2{key: key}
		c.cache[key] = newNode
		c.addToFront(newNode)
	}
}

func (c *LRUCache2) moveToFront(node *LRUNode2) {
	if node == c.head {
		return
	}
	if node == c.tail {
		c.tail = node.prev
		c.tail.next = nil
	} else {
		node.prev.next = node.next
		node.next.prev = node.prev
	}
	c.addToFront(node)
}

func (c *LRUCache2) addToFront(node *LRUNode2) {
	node.prev = nil
	node.next = c.head
	if c.head != nil {
		c.head.prev = node
	}
	c.head = node
	if c.tail == nil {
		c.tail = node
	}
}

func (c *LRUCache2) removeTail() {
	if c.tail == nil {
		return
	}
	delete(c.cache, c.tail.key)
	if c.tail == c.head {
		c.head = nil
		c.tail = nil
	} else {
		c.tail = c.tail.prev
		c.tail.next = nil
	}
}

type LRUCache4 struct {
	capacity int
	cache    map[string]*list.Element
	list     *list.List
}

type Entry4 struct {
	key   string
	value []byte
}

func NewLRUCache4(capacity int) *LRUCache4 {
	return &LRUCache4{
		capacity: capacity,
		cache:    make(map[string]*list.Element),
		list:     list.New(),
	}
}

func (c *LRUCache4) Get(value []byte) bool {
	key := string(value)
	if elem, ok := c.cache[key]; ok {
		// Move the accessed element to the front of the list
		c.list.MoveToFront(elem)
		return true
	}
	return false
}

func (c *LRUCache4) GetString(key string) (bool, []byte) {
	if elem, ok := c.cache[key]; ok {
		// Move the accessed element to the front of the list
		c.list.MoveToFront(elem)
		return true, elem.Value.(*Entry4).value
	}
	return false, nil
}

func (c *LRUCache4) Put(value []byte) {
	key := string(value)

	if elem, ok := c.cache[key]; ok {
		// If the key already exists, move it to the front
		c.list.MoveToFront(elem)
	} else {
		// If the cache is full, remove the least recently used element
		if len(c.cache) >= c.capacity {
			// Get the least recently used element from the back of the list
			tailElem := c.list.Back()
			if tailElem != nil {
				deletedEntry := c.list.Remove(tailElem).(*Entry4)
				delete(c.cache, deletedEntry.key)
			}
		}

		// Add the new key to the cache and the front of the list
		newEntry := &Entry4{key, value}
		newElem := c.list.PushFront(newEntry)
		c.cache[key] = newElem
	}
}

func (c *LRUCache4) Clear() {
	// Iterate through the list and remove all elements
	for elem := c.list.Front(); elem != nil; elem = elem.Next() {
		delete(c.cache, elem.Value.(*Entry4).key)
	}

	// Clear the list
	c.list.Init()
}

type HashSet struct {
	capacity int
	cache    map[string][]byte
}

func NewHashSet(capacity int) *HashSet {
	return &HashSet{
		capacity: capacity,
		cache:    make(map[string][]byte),
	}
}

func (c *HashSet) Get(key string) (bool, []byte) {
	if value, ok := c.cache[key]; ok {
		return true, value
	}
	return false, nil
}

func (c *HashSet) Put(key string) {
	c.cache[key] = []byte(key)
}

func (c *HashSet) PutBytes(value []byte) {
	key := string(value)
	c.cache[key] = []byte(key)
}

func (c *HashSet) PutBoth(key string, value []byte) {
	c.cache[key] = value
}

func (c *HashSet) SurfaceMap() map[string][]byte {
	return c.cache
}

func (c *HashSet) Clear() {
	for k := range c.cache {
		delete(c.cache, k)
	}
}
