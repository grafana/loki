// Package hashprobe provides implementations of probing tables for various
// data types.
//
// Probing tables are specialized hash tables supporting only a single
// "probing" operation which behave like a "lookup or insert". When a key
// is probed, either its value is retrieved if it already existed in the table,
// or it is inserted and assigned its index in the insert sequence as value.
//
// Values are represented as signed 32 bits integers, which means that probing
// tables defined in this package may contain at most 2^31-1 entries.
//
// Probing tables have a method named Probe with the following signature:
//
//	func (t *Int64Table) Probe(keys []int64, values []int32) int {
//		...
//	}
//
// The method takes an array of keys to probe as first argument, an array of
// values where the indexes of each key will be written as second argument, and
// returns the number of keys that were inserted during the call.
//
// Applications that need to determine which keys were inserted can capture the
// length of the probing table prior to the call, and scan the list of values
// looking for indexes greater or equal to the length of the table before the
// call.
package hashprobe

import (
	cryptoRand "crypto/rand"
	"encoding/binary"
	"math"
	"math/bits"
	"math/rand"
	"sync"

	"github.com/parquet-go/parquet-go/hashprobe/aeshash"
	"github.com/parquet-go/parquet-go/hashprobe/wyhash"
	"github.com/parquet-go/parquet-go/internal/unsafecast"
	"github.com/parquet-go/parquet-go/sparse"
)

const (
	// Number of probes tested per iteration. This parameter balances between
	// the amount of memory allocated on the stack to hold the computed hashes
	// of the keys being probed, and amortizing the baseline cost of the probing
	// algorithm.
	//
	// The larger the value, the more memory is required, but lower the baseline
	// cost will be.
	//
	// We chose a value that is somewhat large, resulting in reserving 2KiB of
	// stack but mostly erasing the baseline cost.
	probesPerLoop = 256
)

var (
	prngSeed   [8]byte
	prngMutex  sync.Mutex
	prngSource rand.Source64
)

func init() {
	_, err := cryptoRand.Read(prngSeed[:])
	if err != nil {
		panic("cannot seed random number generator from system source: " + err.Error())
	}
	seed := int64(binary.LittleEndian.Uint64(prngSeed[:]))
	prngSource = rand.NewSource(seed).(rand.Source64)
}

func tableSizeAndMaxLen(groupSize, numValues int, maxLoad float64) (size, maxLen int) {
	n := int(math.Ceil((1 / maxLoad) * float64(numValues)))
	size = nextPowerOf2((n + (groupSize - 1)) / groupSize)
	maxLen = int(math.Ceil(maxLoad * float64(groupSize*size)))
	return
}

func nextPowerOf2(n int) int {
	return 1 << (64 - bits.LeadingZeros64(uint64(n-1)))
}

func randSeed() uintptr {
	prngMutex.Lock()
	defer prngMutex.Unlock()
	return uintptr(prngSource.Uint64())
}

type Int32Table struct{ table32 }

func NewInt32Table(cap int, maxLoad float64) *Int32Table {
	return &Int32Table{makeTable32(cap, maxLoad)}
}

func (t *Int32Table) Reset() { t.reset() }

func (t *Int32Table) Len() int { return t.len }

func (t *Int32Table) Cap() int { return t.size() }

func (t *Int32Table) Probe(keys, values []int32) int {
	return t.probe(unsafecast.Slice[uint32](keys), values)
}

func (t *Int32Table) ProbeArray(keys sparse.Int32Array, values []int32) int {
	return t.probeArray(keys.Uint32Array(), values)
}

type Float32Table struct{ table32 }

func NewFloat32Table(cap int, maxLoad float64) *Float32Table {
	return &Float32Table{makeTable32(cap, maxLoad)}
}

func (t *Float32Table) Reset() { t.reset() }

func (t *Float32Table) Len() int { return t.len }

func (t *Float32Table) Cap() int { return t.size() }

func (t *Float32Table) Probe(keys []float32, values []int32) int {
	return t.probe(unsafecast.Slice[uint32](keys), values)
}

func (t *Float32Table) ProbeArray(keys sparse.Float32Array, values []int32) int {
	return t.probeArray(keys.Uint32Array(), values)
}

type Uint32Table struct{ table32 }

func NewUint32Table(cap int, maxLoad float64) *Uint32Table {
	return &Uint32Table{makeTable32(cap, maxLoad)}
}

func (t *Uint32Table) Reset() { t.reset() }

func (t *Uint32Table) Len() int { return t.len }

func (t *Uint32Table) Cap() int { return t.size() }

func (t *Uint32Table) Probe(keys []uint32, values []int32) int {
	return t.probe(keys, values)
}

func (t *Uint32Table) ProbeArray(keys sparse.Uint32Array, values []int32) int {
	return t.probeArray(keys, values)
}

// table32 is the generic implementation of probing tables for 32 bit types.
//
// The table uses the following memory layout:
//
//	[group 0][group 1][...][group N]
//
// Each group contains up to 7 key/value pairs, and is exactly 64 bytes in size,
// which allows it to fit within a single cache line, and ensures that probes
// can be performed with a single memory load per key.
//
// Groups fill up by appending new entries to the keys and values arrays. When a
// group is full, the probe checks the next group.
//
// https://en.wikipedia.org/wiki/Linear_probing
type table32 struct {
	len     int
	maxLen  int
	maxLoad float64
	seed    uintptr
	table   []table32Group
}

const table32GroupSize = 7

type table32Group struct {
	keys   [table32GroupSize]uint32
	values [table32GroupSize]uint32
	bits   uint32
	_      uint32
}

func makeTable32(cap int, maxLoad float64) (t table32) {
	if maxLoad < 0 || maxLoad > 1 {
		panic("max load of probing table must be a value between 0 and 1")
	}
	if cap < table32GroupSize {
		cap = table32GroupSize
	}
	t.init(cap, maxLoad)
	return t
}

func (t *table32) size() int {
	return table32GroupSize * len(t.table)
}

func (t *table32) init(cap int, maxLoad float64) {
	size, maxLen := tableSizeAndMaxLen(table32GroupSize, cap, maxLoad)
	*t = table32{
		maxLen:  maxLen,
		maxLoad: maxLoad,
		seed:    randSeed(),
		table:   make([]table32Group, size),
	}
}

func (t *table32) grow(totalValues int) {
	tmp := table32{}
	tmp.init(totalValues, t.maxLoad)
	tmp.len = t.len

	hashes := make([]uintptr, table32GroupSize)
	modulo := uintptr(len(tmp.table)) - 1

	for i := range t.table {
		g := &t.table[i]
		n := bits.OnesCount32(g.bits)

		if aeshash.Enabled() {
			aeshash.MultiHash32(hashes[:n], g.keys[:n], tmp.seed)
		} else {
			wyhash.MultiHash32(hashes[:n], g.keys[:n], tmp.seed)
		}

		for j, hash := range hashes[:n] {
			for {
				group := &tmp.table[hash&modulo]

				if n := bits.OnesCount32(group.bits); n < table32GroupSize {
					group.bits = (group.bits << 1) | 1
					group.keys[n] = g.keys[j]
					group.values[n] = g.values[j]
					break
				}

				hash++
			}
		}
	}

	*t = tmp
}

func (t *table32) reset() {
	t.len = 0

	for i := range t.table {
		t.table[i] = table32Group{}
	}
}

func (t *table32) probe(keys []uint32, values []int32) int {
	return t.probeArray(sparse.MakeUint32Array(keys), values)
}

func (t *table32) probeArray(keys sparse.Uint32Array, values []int32) int {
	numKeys := keys.Len()

	if totalValues := t.len + numKeys; totalValues > t.maxLen {
		t.grow(totalValues)
	}

	var hashes [probesPerLoop]uintptr
	var baseLength = t.len
	var useAesHash = aeshash.Enabled()

	_ = values[:numKeys]

	for i := 0; i < numKeys; {
		j := len(hashes) + i
		n := len(hashes)

		if j > numKeys {
			j = numKeys
			n = numKeys - i
		}

		k := keys.Slice(i, j)
		v := values[i:j:j]
		h := hashes[:n:n]

		if useAesHash {
			aeshash.MultiHashUint32Array(h, k, t.seed)
		} else {
			wyhash.MultiHashUint32Array(h, k, t.seed)
		}

		t.len = multiProbe32(t.table, t.len, h, k, v)
		i = j
	}

	return t.len - baseLength
}

func multiProbe32Default(table []table32Group, numKeys int, hashes []uintptr, keys sparse.Uint32Array, values []int32) int {
	modulo := uintptr(len(table)) - 1

	for i, hash := range hashes {
		key := keys.Index(i)
		for {
			group := &table[hash&modulo]
			index := table32GroupSize
			value := int32(0)

			for j, k := range group.keys {
				if k == key {
					index = j
					break
				}
			}

			if n := bits.OnesCount32(group.bits); index < n {
				value = int32(group.values[index])
			} else {
				if n == table32GroupSize {
					hash++
					continue
				}

				value = int32(numKeys)
				group.bits = (group.bits << 1) | 1
				group.keys[n] = key
				group.values[n] = uint32(value)
				numKeys++
			}

			values[i] = value
			break
		}
	}

	return numKeys
}

type Int64Table struct{ table64 }

func NewInt64Table(cap int, maxLoad float64) *Int64Table {
	return &Int64Table{makeTable64(cap, maxLoad)}
}

func (t *Int64Table) Reset() { t.reset() }

func (t *Int64Table) Len() int { return t.len }

func (t *Int64Table) Cap() int { return t.size() }

func (t *Int64Table) Probe(keys []int64, values []int32) int {
	return t.probe(unsafecast.Slice[uint64](keys), values)
}

func (t *Int64Table) ProbeArray(keys sparse.Int64Array, values []int32) int {
	return t.probeArray(keys.Uint64Array(), values)
}

type Float64Table struct{ table64 }

func NewFloat64Table(cap int, maxLoad float64) *Float64Table {
	return &Float64Table{makeTable64(cap, maxLoad)}
}

func (t *Float64Table) Reset() { t.reset() }

func (t *Float64Table) Len() int { return t.len }

func (t *Float64Table) Cap() int { return t.size() }

func (t *Float64Table) Probe(keys []float64, values []int32) int {
	return t.probe(unsafecast.Slice[uint64](keys), values)
}

func (t *Float64Table) ProbeArray(keys sparse.Float64Array, values []int32) int {
	return t.probeArray(keys.Uint64Array(), values)
}

type Uint64Table struct{ table64 }

func NewUint64Table(cap int, maxLoad float64) *Uint64Table {
	return &Uint64Table{makeTable64(cap, maxLoad)}
}

func (t *Uint64Table) Reset() { t.reset() }

func (t *Uint64Table) Len() int { return t.len }

func (t *Uint64Table) Cap() int { return t.size() }

func (t *Uint64Table) Probe(keys []uint64, values []int32) int {
	return t.probe(keys, values)
}

func (t *Uint64Table) ProbeArray(keys sparse.Uint64Array, values []int32) int {
	return t.probeArray(keys, values)
}

// table64 is the generic implementation of probing tables for 64 bit types.
//
// The table uses a layout similar to the one documented on the table for 32 bit
// keys (see table32). Each group holds up to 4 key/value pairs (instead of 7
// like table32) so that each group fits in a single CPU cache line. This table
// version has a bit lower memory density, with ~23% of table memory being used
// for padding.
//
// Technically we could hold up to 5 entries per group and still fit within the
// 64 bytes of a CPU cache line; on x86 platforms, AVX2 registers can only hold
// four 64 bit values, we would need twice as many instructions per probe if the
// groups were holding 5 values. The trade off of memory for compute efficiency
// appeared to be the right choice at the time.
type table64 struct {
	len     int
	maxLen  int
	maxLoad float64
	seed    uintptr
	table   []table64Group
}

const table64GroupSize = 4

type table64Group struct {
	keys   [table64GroupSize]uint64
	values [table64GroupSize]uint32
	bits   uint32
	_      uint32
	_      uint32
	_      uint32
}

func makeTable64(cap int, maxLoad float64) (t table64) {
	if maxLoad < 0 || maxLoad > 1 {
		panic("max load of probing table must be a value between 0 and 1")
	}
	if cap < table64GroupSize {
		cap = table64GroupSize
	}
	t.init(cap, maxLoad)
	return t
}

func (t *table64) size() int {
	return table64GroupSize * len(t.table)
}

func (t *table64) init(cap int, maxLoad float64) {
	size, maxLen := tableSizeAndMaxLen(table64GroupSize, cap, maxLoad)
	*t = table64{
		maxLen:  maxLen,
		maxLoad: maxLoad,
		seed:    randSeed(),
		table:   make([]table64Group, size),
	}
}

func (t *table64) grow(totalValues int) {
	tmp := table64{}
	tmp.init(totalValues, t.maxLoad)
	tmp.len = t.len

	hashes := make([]uintptr, table64GroupSize)
	modulo := uintptr(len(tmp.table)) - 1

	for i := range t.table {
		g := &t.table[i]
		n := bits.OnesCount32(g.bits)

		if aeshash.Enabled() {
			aeshash.MultiHash64(hashes[:n], g.keys[:n], tmp.seed)
		} else {
			wyhash.MultiHash64(hashes[:n], g.keys[:n], tmp.seed)
		}

		for j, hash := range hashes[:n] {
			for {
				group := &tmp.table[hash&modulo]

				if n := bits.OnesCount32(group.bits); n < table64GroupSize {
					group.bits = (group.bits << 1) | 1
					group.keys[n] = g.keys[j]
					group.values[n] = g.values[j]
					break
				}

				hash++
			}
		}
	}

	*t = tmp
}

func (t *table64) reset() {
	t.len = 0

	for i := range t.table {
		t.table[i] = table64Group{}
	}
}

func (t *table64) probe(keys []uint64, values []int32) int {
	return t.probeArray(sparse.MakeUint64Array(keys), values)
}

func (t *table64) probeArray(keys sparse.Uint64Array, values []int32) int {
	numKeys := keys.Len()

	if totalValues := t.len + numKeys; totalValues > t.maxLen {
		t.grow(totalValues)
	}

	var hashes [probesPerLoop]uintptr
	var baseLength = t.len
	var useAesHash = aeshash.Enabled()

	_ = values[:numKeys]

	for i := 0; i < numKeys; {
		j := len(hashes) + i
		n := len(hashes)

		if j > numKeys {
			j = numKeys
			n = numKeys - i
		}

		k := keys.Slice(i, j)
		v := values[i:j:j]
		h := hashes[:n:n]

		if useAesHash {
			aeshash.MultiHashUint64Array(h, k, t.seed)
		} else {
			wyhash.MultiHashUint64Array(h, k, t.seed)
		}

		t.len = multiProbe64(t.table, t.len, h, k, v)
		i = j
	}

	return t.len - baseLength
}

func multiProbe64Default(table []table64Group, numKeys int, hashes []uintptr, keys sparse.Uint64Array, values []int32) int {
	modulo := uintptr(len(table)) - 1

	for i, hash := range hashes {
		key := keys.Index(i)
		for {
			group := &table[hash&modulo]
			index := table64GroupSize
			value := int32(0)

			for i, k := range group.keys {
				if k == key {
					index = i
					break
				}
			}

			if n := bits.OnesCount32(group.bits); index < n {
				value = int32(group.values[index])
			} else {
				if n == table64GroupSize {
					hash++
					continue
				}

				value = int32(numKeys)
				group.bits = (group.bits << 1) | 1
				group.keys[n] = key
				group.values[n] = uint32(value)
				numKeys++
			}

			values[i] = value
			break
		}
	}

	return numKeys
}

type Uint128Table struct{ table128 }

func NewUint128Table(cap int, maxLoad float64) *Uint128Table {
	return &Uint128Table{makeTable128(cap, maxLoad)}
}

func (t *Uint128Table) Reset() { t.reset() }

func (t *Uint128Table) Len() int { return t.len }

func (t *Uint128Table) Cap() int { return t.cap }

func (t *Uint128Table) Probe(keys [][16]byte, values []int32) int {
	return t.probe(keys, values)
}

func (t *Uint128Table) ProbeArray(keys sparse.Uint128Array, values []int32) int {
	return t.probeArray(keys, values)
}

// table128 is the generic implementation of probing tables for 128 bit types.
//
// This table uses the following memory layout:
//
//	[key A][key B][...][value A][value B][...]
//
// The table stores values as their actual value plus one, and uses zero as a
// sentinel to determine whether a slot is occupied. A linear probing strategy
// is used to resolve conflicts. This approach results in at most two memory
// loads for every four keys being tested, since the location of a key and its
// corresponding value will not be contiguous on the same CPU cache line, but
// a cache line can hold four 16 byte keys.
type table128 struct {
	len     int
	cap     int
	maxLen  int
	maxLoad float64
	seed    uintptr
	table   []byte
}

func makeTable128(cap int, maxLoad float64) (t table128) {
	if maxLoad < 0 || maxLoad > 1 {
		panic("max load of probing table must be a value between 0 and 1")
	}
	if cap < 8 {
		cap = 8
	}
	t.init(cap, maxLoad)
	return t
}

func (t *table128) init(cap int, maxLoad float64) {
	size, maxLen := tableSizeAndMaxLen(1, cap, maxLoad)
	*t = table128{
		cap:     size,
		maxLen:  maxLen,
		maxLoad: maxLoad,
		seed:    randSeed(),
		table:   make([]byte, 16*size+4*size),
	}
}

func (t *table128) kv() (keys [][16]byte, values []int32) {
	i := t.cap * 16
	return unsafecast.Slice[[16]byte](t.table[:i]), unsafecast.Slice[int32](t.table[i:])
}

func (t *table128) grow(totalValues int) {
	tmp := table128{}
	tmp.init(totalValues, t.maxLoad)
	tmp.len = t.len

	keys, values := t.kv()
	hashes := make([]uintptr, probesPerLoop)
	useAesHash := aeshash.Enabled()

	_ = values[:len(keys)]

	for i := 0; i < len(keys); {
		j := len(hashes) + i
		n := len(hashes)

		if j > len(keys) {
			j = len(keys)
			n = len(keys) - i
		}

		h := hashes[:n:n]
		k := keys[i:j:j]
		v := values[i:j:j]

		if useAesHash {
			aeshash.MultiHash128(h, k, tmp.seed)
		} else {
			wyhash.MultiHash128(h, k, tmp.seed)
		}

		tmp.insert(h, k, v)
		i = j
	}

	*t = tmp
}

func (t *table128) insert(hashes []uintptr, keys [][16]byte, values []int32) {
	tableKeys, tableValues := t.kv()
	modulo := uintptr(t.cap) - 1

	for i, hash := range hashes {
		for {
			j := hash & modulo
			v := tableValues[j]

			if v == 0 {
				tableKeys[j] = keys[i]
				tableValues[j] = values[i]
				break
			}

			hash++
		}
	}
}

func (t *table128) reset() {
	t.len = 0

	for i := range t.table {
		t.table[i] = 0
	}
}

func (t *table128) probe(keys [][16]byte, values []int32) int {
	return t.probeArray(sparse.MakeUint128Array(keys), values)
}

func (t *table128) probeArray(keys sparse.Uint128Array, values []int32) int {
	numKeys := keys.Len()

	if totalValues := t.len + numKeys; totalValues > t.maxLen {
		t.grow(totalValues)
	}

	var hashes [probesPerLoop]uintptr
	var baseLength = t.len
	var useAesHash = aeshash.Enabled()

	_ = values[:numKeys]

	for i := 0; i < numKeys; {
		j := len(hashes) + i
		n := len(hashes)

		if j > numKeys {
			j = numKeys
			n = numKeys - i
		}

		k := keys.Slice(i, j)
		v := values[i:j:j]
		h := hashes[:n:n]

		if useAesHash {
			aeshash.MultiHashUint128Array(h, k, t.seed)
		} else {
			wyhash.MultiHashUint128Array(h, k, t.seed)
		}

		t.len = multiProbe128(t.table, t.cap, t.len, h, k, v)
		i = j
	}

	return t.len - baseLength
}

func multiProbe128Default(table []byte, tableCap, tableLen int, hashes []uintptr, keys sparse.Uint128Array, values []int32) int {
	modulo := uintptr(tableCap) - 1
	offset := uintptr(tableCap) * 16
	tableKeys := unsafecast.Slice[[16]byte](table[:offset])
	tableValues := unsafecast.Slice[int32](table[offset:])

	for i, hash := range hashes {
		key := keys.Index(i)
		for {
			j := hash & modulo
			v := tableValues[j]

			if v == 0 {
				values[i] = int32(tableLen)
				tableLen++
				tableKeys[j] = key
				tableValues[j] = int32(tableLen)
				break
			}

			if key == tableKeys[j] {
				values[i] = v - 1
				break
			}

			hash++
		}
	}

	return tableLen
}
