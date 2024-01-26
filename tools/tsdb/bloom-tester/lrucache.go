package main

import (
	"container/list"
	"fmt"
)

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
	if elem, ok := c.cache[string(value)]; ok {
		// Move the accessed element to the front of the list
		c.list.MoveToFront(elem)
		return true
	}
	return false
}

func (c *LRUCache4) GetString(key string) bool {
	if elem, ok := c.cache[key]; ok {
		// Move the accessed element to the front of the list
		c.list.MoveToFront(elem)
		return true
	}
	return false
}

func (c *LRUCache4) PutStringByte(key string, value []byte) {

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

func (c *LRUCache4) Put(value []byte) {
	c.PutStringByte(string(value), value)
}

func (c *LRUCache4) PutString(value string) {
	c.PutStringByte(value, []byte(value))
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

// ByteKey is an interface for types that represent keys of a certain size.
type ByteKey interface {
	Size() int
	Equal(other ByteKey) bool
}

// FourByteKey represents a key of 4 bytes.
type FourByteKey [4]byte

// Size returns the size of the FourByteKey.
func (k FourByteKey) Size() int {
	return 4
}

// Equal checks if two FourByteKeys are equal.
func (k FourByteKey) Equal(other ByteKey) bool {
	if otherFourByteKey, ok := other.(FourByteKey); ok {
		return k == otherFourByteKey
	}
	return false
}

// ThirtyOneByteKey represents a key of 31 bytes.
type ThirtyOneByteKey [31]byte

// Size returns the size of the ThirtyOneByteKey.
func (k ThirtyOneByteKey) Size() int {
	return 31
}

// Equal checks if two ThirtyOneByteKeys are equal.
func (k ThirtyOneByteKey) Equal(other ByteKey) bool {
	if otherThirtyOneByteKey, ok := other.(ThirtyOneByteKey); ok {
		return k == otherThirtyOneByteKey
	}
	return false
}

type ByteKeyLRUCache struct {
	capacity int
	//m        map[ByteKey]struct{}
	m    map[ByteKey]*list.Element
	list *list.List
}

func NewByteKeyLRUCache(capacity int) *ByteKeyLRUCache {
	return &ByteKeyLRUCache{
		capacity: capacity,
		m:        make(map[ByteKey]*list.Element, capacity),
		list:     list.New(),
	}
}

func (c *ByteKeyLRUCache) Get(key ByteKey) bool {
	if value, ok := c.m[key]; ok {
		// Move the accessed element to the front of the list
		c.list.MoveToFront(value)
		return true
	}
	return false
}

func (c *ByteKeyLRUCache) Put(key ByteKey) {
	if value, ok := c.m[key]; ok {
		// If the key already exists, move it to the front
		c.list.MoveToFront(value)
	} else {
		// If the cache is full, remove the least recently used element
		if len(c.m) >= c.capacity {
			// Get the least recently used element from the back of the list
			tailElem := c.list.Back()
			if tailElem != nil {
				deletedEntry := c.list.Remove(tailElem).(ByteKey)
				delete(c.m, deletedEntry)
			}
		}

		// Add the new key to the cache and the front of the list
		elem := c.list.PushFront(key)
		c.m[key] = elem
	}
}

func (c *ByteKeyLRUCache) Clear() {
	// Iterate through the list and remove all elements
	for elem := c.list.Front(); elem != nil; elem = elem.Next() {
		delete(c.m, elem.Value.(ByteKey))
	}

	// Clear the list
	c.list.Init()
}

// ByteKeyMap is a map that uses ByteKey as a key.
type ByteKeyMap struct {
	capacity int
	m        map[ByteKey]struct{}
}

// NewByteKeyMap creates a new ByteKeyMap.
func NewByteKeyMap(capacity int) ByteKeyMap {
	return ByteKeyMap{
		capacity: capacity,
		m:        make(map[ByteKey]struct{}, capacity),
	}
}

// Put adds an entry to the map.
func (bm *ByteKeyMap) Put(key ByteKey) {
	bm.m[key] = struct{}{}
}

// Get retrieves a value from the map based on the key.
func (bm *ByteKeyMap) Get(key ByteKey) bool {
	_, exists := bm.m[key]
	return exists
}

type ByteSet struct {
	capacity int
	cache    map[[4]byte]struct{}
}

func NewByteSet(capacity int) *ByteSet {
	return &ByteSet{
		capacity: capacity,
		cache:    make(map[[4]byte]struct{}),
	}
}

func sliceToByteArray(slice []byte) [4]byte {
	// Define the desired size of the byte array
	// If you want to make it dynamically sized, use len(slice)
	var array [4]byte

	// Copy elements from the slice to the array
	copy(array[:], slice)

	return array
}

// NewFourByteKeyFromSlice converts a byte slice to a FourByteKey.
func NewFourByteKeyFromSlice(slice []byte) FourByteKey {
	var key FourByteKey
	copy(key[:], slice)
	return key
}

// NewThirtyOneByteKeyFromSlice converts a byte slice to a FourByteKey.
func NewThirtyOneByteKeyFromSlice(slice []byte) ThirtyOneByteKey {
	var key ThirtyOneByteKey
	copy(key[:], slice)
	return key
}

func (c ByteSet) Get(key string) bool {
	if _, ok := c.cache[sliceToByteArray([]byte(key))]; ok {
		return true
	}
	return false
}

func (c *ByteSet) Put(key string) {
	c.cache[sliceToByteArray([]byte(key))] = struct{}{}
}

func (c *ByteSet) PutBytes(value []byte) {
	c.cache[sliceToByteArray(value)] = struct{}{}
}

func (c *ByteSet) Clear() {
	for k := range c.cache {
		delete(c.cache, k)
	}
}

type FourByteKeyLRUCache struct {
	capacity int
	m        map[[4]byte]*list.Element
	list     *list.List
}

func NewFourByteKeyLRUCache(capacity int) *FourByteKeyLRUCache {
	return &FourByteKeyLRUCache{
		capacity: capacity,
		m:        make(map[[4]byte]*list.Element, capacity),
		list:     list.New(),
	}
}

func (c *FourByteKeyLRUCache) Get(key [4]byte) bool {
	if value, ok := c.m[key]; ok {
		// Move the accessed element to the front of the list
		c.list.MoveToFront(value)
		return true
	}
	return false
}

func (c *FourByteKeyLRUCache) Put(key [4]byte) {
	if value, ok := c.m[key]; ok {
		// If the key already exists, move it to the front
		c.list.MoveToFront(value)
	} else {
		// If the cache is full, remove the least recently used element
		if len(c.m) >= c.capacity {
			// Get the least recently used element from the back of the list
			tailElem := c.list.Back()
			if tailElem != nil {
				deletedEntry := c.list.Remove(tailElem).([4]byte)
				delete(c.m, deletedEntry)
			}
		}

		// Add the new key to the cache and the front of the list
		elem := c.list.PushFront(key)
		c.m[key] = elem
	}
}

func (c *FourByteKeyLRUCache) Clear() {
	// Iterate through the list and remove all elements
	for elem := c.list.Front(); elem != nil; elem = elem.Next() {
		delete(c.m, elem.Value.([4]byte))
	}

	// Clear the list
	c.list.Init()
}

type LRUCache5 struct {
	capacity int
	cache    map[string]*LRUNode5
	head     *LRUNode5
	tail     *LRUNode5
}

type LRUNode5 struct {
	key  string
	prev *LRUNode5
	next *LRUNode5
}

func NewLRUCache5(capacity int) *LRUCache5 {
	return &LRUCache5{
		capacity: capacity,
	}
}
func (c *LRUCache5) init() {
	c.cache = make(map[string]*LRUNode5, c.capacity)
	c.head = new(LRUNode5)
	c.tail = new(LRUNode5)
	c.head.next = c.tail
	c.tail.prev = c.head
}

func (c *LRUCache5) pop(item *LRUNode5) {
	item.prev.next = item.next
	item.next.prev = item.prev
}

func (c *LRUCache5) push(item *LRUNode5) {
	c.head.next.prev = item
	item.next = c.head.next
	item.prev = c.head
	c.head.next = item
}

func (c *LRUCache5) evict() *LRUNode5 {
	item := c.tail.prev
	c.pop(item)
	delete(c.cache, item.key)
	return item
}

func (c *LRUCache5) Get(key string) bool {
	if c.cache == nil {
		c.init()
	}
	item := c.cache[key]
	if item == nil {
		return false
	}
	if c.head.next != item {
		c.pop(item)
		c.push(item)
	}
	return true
}

func (c *LRUCache5) Put(key string) {
	if c.cache == nil {
		c.init()
	}
	item := c.cache[key]
	if item == nil {
		if len(c.cache) == c.capacity {
			item = c.evict()
		} else {
			item = new(LRUNode5)
		}
		item.key = key
		c.push(item)
		c.cache[key] = item
	} else {
		if c.head.next != item {
			c.pop(item)
			c.push(item)
		}
	}
}

func (c *LRUCache5) Clear() {
	if c.cache != nil {

		for elem := range c.cache {
			delete(c.cache, elem)
		}

		c.head = nil
		c.tail = nil
	}
}

func (c *LRUCache5) Dump() {
	if c.cache != nil {

		for elem := range c.cache {
			fmt.Println(elem)
		}

	}
}
