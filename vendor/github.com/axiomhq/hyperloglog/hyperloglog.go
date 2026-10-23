// Package hyperloglog implements the HyperLogLog cardinality estimator with
// sparse and dense representations.
package hyperloglog

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"slices"
)

const (
	pp      = uint8(25)
	mp      = uint32(1) << pp
	version = 2
)

// Sketch is a HyperLogLog estimator. Do not copy one after first use; use
// Clone. Its zero value is empty and initializes on insertion or merge: a zero
// value first used with Insert or InsertHash takes New's configuration
// (precision 14, sparse), while a zero value first used with Merge adopts the
// other sketch's precision and representation.
type Sketch struct {
	p          uint8
	m          uint32
	alpha      float64
	tmpSet     set
	sparseList *compressedList
	regs       []uint8
}

// New returns a HyperLogLog Sketch with 2^14 registers (precision 14)
func New() *Sketch { return New14() }

// New14 returns a HyperLogLog Sketch with 2^14 registers (precision 14)
func New14() *Sketch { return newSketchNoError(14, true) }

// New16 returns a HyperLogLog Sketch with 2^16 registers (precision 16)
func New16() *Sketch { return newSketchNoError(16, true) }

// NewNoSparse returns a HyperLogLog Sketch with 2^14 registers (precision 14) that will not use a sparse representation
func NewNoSparse() *Sketch { return newSketchNoError(14, false) }

// New16NoSparse returns a HyperLogLog Sketch with 2^16 registers (precision 16) that will not use a sparse representation
func New16NoSparse() *Sketch { return newSketchNoError(16, false) }

func newSketchNoError(precision uint8, sparse bool) *Sketch {
	sk, _ := NewSketch(precision, sparse)
	return sk
}

func checkPrecision(p uint8) error {
	if p < 4 || p > 18 {
		return ErrorInvalidPrecision
	}
	return nil
}

func exactLen(what string, have, want uint64) error {
	switch {
	case have == want:
		return nil
	case have < want:
		return fmt.Errorf("hyperloglog: %s need %d bytes, have %d: %w", what, want, have, ErrorTooShort)
	default:
		return fmt.Errorf("hyperloglog: %s need %d bytes, %d bytes follow them: %w", what, want, have-want, ErrorInvalidData)
	}
}

func maxRho(p uint8) uint8 { return 64 - p + 1 }

// NewSketch returns a HyperLogLog Sketch with 2^precision registers. The
// precision has to be >= 4 and <= 18, otherwise ErrorInvalidPrecision is
// returned. When sparse is true the Sketch starts out in the sparse
// representation.
func NewSketch(precision uint8, sparse bool) (*Sketch, error) {
	if err := checkPrecision(precision); err != nil {
		return nil, err
	}
	m := uint32(1) << precision
	s := &Sketch{
		m:     m,
		p:     precision,
		alpha: alpha(float64(m)),
	}
	if sparse {
		s.tmpSet = makeSet(0)
		s.sparseList = newCompressedList(0)
	} else {
		s.regs = make([]uint8, m)
	}
	return s, nil
}

func (sk *Sketch) sparse() bool { return sk.sparseList != nil }

// Clone returns a deep copy of sk.
func (sk *Sketch) Clone() *Sketch {
	clone := *sk
	clone.regs = append([]uint8(nil), sk.regs...)
	clone.tmpSet = sk.tmpSet.Clone()
	clone.sparseList = sk.sparseList.Clone()
	return &clone
}

func (sk *Sketch) maybeToNormal() {
	if uint32(sk.tmpSet.Len())*100 > sk.m {
		sk.mergeSparse()
		if uint32(sk.sparseList.Len()) > sk.m {
			sk.toNormal()
		}
	}
}

// Merge adds other to sk. Nil and zero-value sketches are treated as empty.
func (sk *Sketch) Merge(other *Sketch) error {
	if other == nil || other.p == 0 {
		return nil
	}
	if sk.p == 0 {
		*sk = *other.Clone()
		return nil
	}
	if sk.p != other.p {
		return fmt.Errorf("hyperloglog: cannot merge precision %d with precision %d: %w", sk.p, other.p, ErrorPrecisionMismatch)
	}

	if sk.sparse() && other.sparse() {
		sk.mergeSparseSketch(other)
	} else {
		sk.mergeDenseSketch(other)
	}
	return nil
}

func (sk *Sketch) mergeSparseSketch(other *Sketch) {
	sk.tmpSet.Merge(other.tmpSet)
	for iter := other.sparseList.Iter(); iter.HasNext(); {
		sk.tmpSet.add(iter.Next())
	}
	sk.maybeToNormal()
}

func (sk *Sketch) mergeDenseSketch(other *Sketch) {
	if sk.sparse() {
		sk.toNormal()
	}

	if other.sparse() {
		other.tmpSet.ForEach(func(k uint32) {
			i, r := decodeHash(k, other.p, pp)
			sk.insert(i, r)
		})
		for iter := other.sparseList.Iter(); iter.HasNext(); {
			i, r := decodeHash(iter.Next(), other.p, pp)
			sk.insert(i, r)
		}
	} else {
		for i, v := range other.regs {
			if v > sk.regs[i] {
				sk.regs[i] = v
			}
		}
	}
}

func (sk *Sketch) toNormal() {
	if sk.tmpSet.Len() > 0 {
		sk.mergeSparse()
	}

	sk.regs = make([]uint8, sk.m)
	for iter := sk.sparseList.Iter(); iter.HasNext(); {
		i, r := decodeHash(iter.Next(), sk.p, pp)
		sk.insert(i, r)
	}

	sk.tmpSet = nilSet
	sk.sparseList = nil
}

func (sk *Sketch) insert(i uint32, r uint8) { sk.regs[i] = max(r, sk.regs[i]) }

// Insert hashes e with the package's MetroHash64 seed and adds it to sk.
func (sk *Sketch) Insert(e []byte) { sk.InsertHash(hash(e)) }

// InsertHash adds a uniformly distributed 64-bit hash to sk.
func (sk *Sketch) InsertHash(x uint64) {
	if sk.p == 0 {
		*sk = *New()
	}
	if sk.sparse() {
		if sk.tmpSet.add(encodeHash(x, sk.p, pp)) {
			sk.maybeToNormal()
		}
		return
	}
	i, r := getPosVal(x, sk.p)
	sk.insert(uint32(i), r)
}

// Estimate returns the cardinality estimate and may compact sparse state.
func (sk *Sketch) Estimate() uint64 {
	if sk.p == 0 {
		return 0
	}
	if sk.sparse() {
		sk.mergeSparse()
		// mergeSparse drains tmpSet into sparseList without consulting
		// maybeToNormal, whose threshold only ever fires on insertion. Without
		// this check a caller alternating small batches of Insert with
		// Estimate keeps the sparse list growing past m forever.
		if uint32(sk.sparseList.Len()) > sk.m {
			sk.toNormal()
		} else {
			return uint64(linearCount(mp, mp-min(sk.sparseList.count, mp-1)))
		}
	}

	sum, ez := sumAndZeros(sk.regs)
	m := float64(sk.m)

	est := sk.alpha * m * (m - ez) / (sum + beta(sk.p, ez))
	return uint64(est + 0.5)
}

func (sk *Sketch) mergeSparse() {
	if sk.tmpSet.Len() == 0 {
		return
	}

	keys := make([]uint32, 0, sk.tmpSet.Len())
	sk.tmpSet.ForEach(func(k uint32) {
		keys = append(keys, k)
	})
	slices.Sort(keys)

	newList := newCompressedList(4*sk.tmpSet.Len() + sk.sparseList.Len())
	for iter, i := sk.sparseList.Iter(), 0; iter.HasNext() || i < len(keys); {
		if !iter.HasNext() {
			newList.Append(keys[i])
			i++
			continue
		}

		if i >= len(keys) {
			newList.Append(iter.Next())
			continue
		}

		x1, adv := iter.Peek()
		x2 := keys[i]
		if x1 == x2 {
			newList.Append(x1)
			iter.Advance(x1, adv)
			i++
		} else if x1 > x2 {
			newList.Append(x2)
			i++
		} else {
			newList.Append(x1)
			iter.Advance(x1, adv)
		}
	}

	sk.sparseList = newList
	sk.tmpSet.clear()
}

// MarshalBinary implements the encoding.BinaryMarshaler interface.
//
// An uninitialized (zero value) Sketch has no encoding: its precision is 0, so
// MarshalBinary returns an error wrapping ErrorInvalidPrecision.
//
// When the result will be appended to another buffer, consider using
// AppendBinary to avoid additional allocations and copying.
func (sk *Sketch) MarshalBinary() (data []byte, err error) {
	return sk.AppendBinary(nil)
}

// AppendBinary implements the encoding.BinaryAppender interface.
//
// An uninitialized (zero value) Sketch has no encoding: its precision is 0, so
// AppendBinary returns an error wrapping ErrorInvalidPrecision and leaves the
// caller's buffer unmodified.
//
// UnmarshalBinary requires the encoding to be the entire buffer it is handed
// and rejects trailing bytes with ErrorInvalidData. A caller appending a Sketch
// into a larger buffer must frame the record itself; the number of bytes
// appended is len(result) - len(data).
//
// The encoding is not canonical: two sketches holding the same values may
// encode to different bytes, depending on insertion order and on whether
// Estimate has compacted the sparse representation. An encoding must not be
// hashed, deduplicated, or compared for equality to decide whether two
// sketches hold the same values.
func (sk *Sketch) AppendBinary(data []byte) ([]byte, error) {
	// Refuse to write a header no UnmarshalBinary would accept, and leave the
	// caller's buffer untouched when we do.
	if err := checkPrecision(sk.p); err != nil {
		return data, fmt.Errorf("hyperloglog: precision %d: %w", sk.p, err)
	}
	data = slices.Grow(data, 8+len(sk.regs))
	// Marshal a version marker.
	data = append(data, version)
	// Marshal p.
	data = append(data, sk.p)
	// Marshal b
	data = append(data, 0)

	if sk.sparse() {
		// It's using the sparse Sketch.
		data = append(data, byte(1))

		// Add the tmp_set
		data, err := sk.tmpSet.AppendBinary(data)
		if err != nil {
			return nil, err
		}

		// Add the sparse Sketch
		return sk.sparseList.AppendBinary(data)
	}

	// It's using the dense Sketch.
	data = append(data, byte(0))

	// Add the dense sketch Sketch.
	sz := len(sk.regs)
	data = append(data,
		byte(sz>>24),
		byte(sz>>16),
		byte(sz>>8),
		byte(sz),
	)

	// Marshal each element in the list.
	data = append(data, sk.regs...)

	return data, nil
}

// ErrorTooShort is returned, wrapped, when a buffer ends before the format
// requires. UnmarshalBinary used to return it unwrapped, so err ==
// ErrorTooShort is now always false and callers have to use errors.Is.
var ErrorTooShort = errors.New("too short binary")

// ErrorInvalidVersion is returned by UnmarshalBinary when the version byte is
// neither 1 nor 2.
var ErrorInvalidVersion = errors.New("unknown serialization version")

// ErrorInvalidPrecision is returned unwrapped by NewSketch, and wrapped by
// UnmarshalBinary, MarshalBinary and AppendBinary, when the precision is
// outside the supported range.
var ErrorInvalidPrecision = errors.New("p has to be >= 4 and <= 18")

// ErrorInvalidData is returned by UnmarshalBinary when the binary is long
// enough but describes a state that cannot be decoded.
var ErrorInvalidData = errors.New("invalid binary data")

// ErrorPrecisionMismatch is wrapped by Merge when the sketches have different
// precisions.
var ErrorPrecisionMismatch = errors.New("precisions must be equal")

// UnmarshalBinary implements the encoding.BinaryUnmarshaler interface.
//
// The binary format starts with a 4 byte header:
//
//	byte 0: version. 2 is written; 1 and 2 are accepted, anything else
//	        returns ErrorInvalidVersion.
//	byte 1: precision p, which must be in [4, 18], otherwise
//	        ErrorInvalidPrecision is returned.
//	byte 2: b, the register bias of the version 1 dense payload. It is
//	        ignored for sparse payloads of either version. Version 2 writes
//	        0 and requires 0.
//	byte 3: 1 if the payload is sparse, 0 if it is dense.
//
// The sparse payload is identical for version 1 and 2: a uint32 big endian
// count N of tmp set keys, followed by N uint32 big endian keys, followed by
// the compressed list: a uint32 big endian count, a uint32 big endian last
// value, a uint32 big endian size sz, and sz bytes of a delta varint stream
// whose final byte must have its high bit clear. The tmp set keys may appear in
// any order.
//
// Version 2 pins the hash function to MetroHash64 with seed 1337 and the sparse
// precision pp to 25. Sparse keys and dense registers are only meaningful under
// those two constants, so changing either requires a new version byte.
//
// The version 2 dense payload is a uint32 big endian register count, which
// must equal m = 1<<p, followed by m register bytes. In the version 1 dense
// payload bytes 4:8 are ignored and bytes 8: hold m/2 bytes of two 4 bit
// registers each, both biased by b.
//
// Byte 2 must be 0 when the version is 2, and byte 3 must be 0 or 1; any other
// value returns ErrorInvalidData. The compressed list's count must equal the
// number of varints in its stream and must be less than 2^25, and its last
// value must equal the sum of the deltas, otherwise ErrorInvalidData is
// returned. Each delta varint must be at most 5 bytes long, minimally encoded,
// and must fit in 32 bits. The running sum of the deltas must strictly
// increase after the first delta and must not wrap; a first delta of 0 is
// legal. A violation of either rule returns ErrorInvalidData. Every decoded
// sparse key and every dense register must decode to a value no greater than
// 64-p+1, otherwise ErrorInvalidData is returned.
//
// The payload must end exactly where its declared lengths say it does: bytes
// following the sparse compressed list, the version 2 registers, or the
// version 1 packed registers return ErrorInvalidData.
//
// Version, precision and every length prefix are validated before the receiver
// is mutated, so the receiver is never left with registers or a sparse list
// shorter than the layout implies. If an error is returned sk is left
// unchanged. Every error returned by UnmarshalBinary wraps one of the
// exported sentinels above and has to be matched with errors.Is rather than
// with ==.
func (sk *Sketch) UnmarshalBinary(data []byte) error {
	if len(data) < 8 {
		return fmt.Errorf("hyperloglog: header needs 8 bytes, have %d: %w", len(data), ErrorTooShort)
	}

	// Unmarshal version. We may need this in the future if we make
	// non-compatible changes.
	v := data[0]
	if v != 1 && v != 2 {
		return fmt.Errorf("hyperloglog: version %d: %w", v, ErrorInvalidVersion)
	}

	// Unmarshal p.
	p := data[1]

	// Determine if we need a sparse Sketch
	if data[3] > 1 {
		return fmt.Errorf("hyperloglog: header byte 3 = %d: %w", data[3], ErrorInvalidData)
	}
	sparse := data[3] == 1

	// Unmarshal b. Only the version 1 dense encoding has a register bias.
	b := data[2]
	if v == 2 && b != 0 {
		return fmt.Errorf("hyperloglog: header byte 2 = %d for version 2: %w", b, ErrorInvalidData)
	}

	// Validate the precision before anything is sized from it, so that a
	// header declaring a large p cannot allocate registers the payload does
	// not have. The scratch Sketch below is only built once the declared
	// lengths check out, and the receiver is only replaced once the whole
	// payload has parsed.
	if err := checkPrecision(p); err != nil {
		return fmt.Errorf("hyperloglog: precision %d: %w", p, err)
	}
	m := uint32(1) << p

	// The parsed sketch is only committed to the receiver at the single exit
	// below, after the whole payload has been validated.
	var tmp *Sketch

	switch {
	case sparse:
		// Using the sparse Sketch.

		// Unmarshal the tmp_set.
		tssz := binary.BigEndian.Uint32(data[4:8])

		// We need to unmarshal tssz values in total, and each value requires us
		// to read 4 bytes.
		need := 8 + 4*uint64(tssz)
		if need > uint64(len(data)) {
			return fmt.Errorf("hyperloglog: tmp set of %d keys needs %d bytes, have %d: %w", tssz, need, len(data), ErrorTooShort)
		}
		if tssz > m {
			return fmt.Errorf("hyperloglog: tmp set count %d exceeds register count %d: %w", tssz, m, ErrorInvalidData)
		}
		tmp = newSketchNoError(p, true)
		tmp.tmpSet = makeSet(int(tssz))

		tsLastByte := int(need)
		for i := 8; i < tsLastByte; i += 4 {
			k := binary.BigEndian.Uint32(data[i : i+4])
			if err := checkSparseKey(k, p); err != nil {
				return fmt.Errorf("hyperloglog: tmp set key at offset %d: %w", i, err)
			}
			tmp.tmpSet.add(k)
		}

		// Unmarshal the sparse Sketch.
		if err := tmp.sparseList.unmarshal(data[tsLastByte:], p); err != nil {
			return fmt.Errorf("hyperloglog: compressed list at offset %d: %w", tsLastByte, err)
		}

	case v == 1:
		// Using the version 1 dense Sketch, where two 4 bit registers are
		// packed into each byte.
		payload := data[8:]
		nb := int(m) / 2
		if err := exactLen("v1 dense registers at offset 8", uint64(len(payload)), uint64(nb)); err != nil {
			return err
		}
		tmp = newSketchNoError(p, false)
		if err := tmp.unmarshalBinaryV1(payload, b); err != nil {
			return err
		}

	default:
		// Using the version 2 dense Sketch.
		payload := data[8:]
		sz := binary.BigEndian.Uint32(data[4:8])
		if uint64(sz) != uint64(m) {
			return fmt.Errorf("hyperloglog: dense register count %d, want m = %d: %w", sz, m, ErrorInvalidData)
		}
		if err := exactLen("dense registers at offset 8", uint64(len(payload)), uint64(sz)); err != nil {
			return err
		}
		tmp = newSketchNoError(p, false)
		if err := tmp.unmarshalBinaryV2(payload); err != nil {
			return err
		}
	}

	*sk = *tmp
	return nil
}

func sumAndZeros(regs []uint8) (res, ez float64) {
	for _, v := range regs {
		if v == 0 {
			ez++
		}
		res += 1.0 / math.Pow(2.0, float64(v))
	}
	return res, ez
}

// unmarshalBinaryV1 requires len(sk.regs) == int(sk.m) and
// len(data) == int(sk.m)/2.
func (sk *Sketch) unmarshalBinaryV1(data []byte, b uint8) error {
	maxRho := maxRho(sk.p)
	for i, v := range data {
		// Widen before adding the bias so that it cannot wrap.
		hi := uint16(v>>4) + uint16(b)
		lo := uint16(v&0x0f) + uint16(b)
		if hi > uint16(maxRho) {
			return fmt.Errorf("hyperloglog: v1 register %d = %d at offset %d, max %d: %w", i*2, hi, 8+i, maxRho, ErrorInvalidData)
		}
		if lo > uint16(maxRho) {
			return fmt.Errorf("hyperloglog: v1 register %d = %d at offset %d, max %d: %w", i*2+1, lo, 8+i, maxRho, ErrorInvalidData)
		}
		sk.regs[i*2] = uint8(hi)
		sk.regs[i*2+1] = uint8(lo)
	}
	return nil
}

// unmarshalBinaryV2 requires len(sk.regs) == int(sk.m) and
// len(data) == int(sk.m).
func (sk *Sketch) unmarshalBinaryV2(data []byte) error {
	maxRho := maxRho(sk.p)
	copy(sk.regs, data)
	for i, r := range sk.regs {
		if r > maxRho {
			return fmt.Errorf("hyperloglog: register %d = %d, max %d: %w", i, r, maxRho, ErrorInvalidData)
		}
	}
	return nil
}

// Reset clears the sketch while preserving its current representation and
// allocated backing storage.
func (sk *Sketch) Reset() {
	if sk.sparse() {
		sk.tmpSet.clear()
		sk.sparseList.clear()
		return
	}
	clear(sk.regs)
}
