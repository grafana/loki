package encoding

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"

	"github.com/prometheus/common/model"
	"github.com/prometheus/tsdb/chunkenc"
)

const samplesPerChunk = 120

var errOutOfBounds = errors.New("out of bounds")

type smallChunk struct {
	*chunkenc.XORChunk
	start int64
	end   int64
}

// bigchunk is a set of prometheus/tsdb chunks.  It grows over time and has no
// upperbound on number of samples it can contain.
type bigchunk struct {
	chunks []smallChunk

	appender         chunkenc.Appender
	remainingSamples int
}

func newBigchunk() *bigchunk {
	return &bigchunk{}
}

func (b *bigchunk) Add(sample model.SamplePair) ([]Chunk, error) {
	if b.remainingSamples == 0 {
		if bigchunkSizeCapBytes > 0 && b.Size() > bigchunkSizeCapBytes {
			return addToOverflowChunk(b, sample)
		}
		if err := b.addNextChunk(sample.Timestamp); err != nil {
			return nil, err
		}
	}

	b.appender.Append(int64(sample.Timestamp), float64(sample.Value))
	b.remainingSamples--
	b.chunks[len(b.chunks)-1].end = int64(sample.Timestamp)
	return []Chunk{b}, nil
}

// addNextChunk adds a new XOR "subchunk" to the internal list of chunks.
func (b *bigchunk) addNextChunk(start model.Time) error {
	// To save memory, we "compact" the previous chunk - the array backing the slice
	// will be upto 2x too big, and we can save this space.
	const chunkCapacityExcess = 32 // don't bother copying if it's within this range
	if l := len(b.chunks); l > 0 {
		oldBuf := b.chunks[l-1].XORChunk.Bytes()
		if cap(oldBuf) > len(oldBuf)+chunkCapacityExcess {
			buf := make([]byte, len(oldBuf))
			copy(buf, oldBuf)
			compacted, err := chunkenc.FromData(chunkenc.EncXOR, buf)
			if err != nil {
				return err
			}
			b.chunks[l-1].XORChunk = compacted.(*chunkenc.XORChunk)
		}
	}

	chunk := chunkenc.NewXORChunk()
	appender, err := chunk.Appender()
	if err != nil {
		return err
	}

	b.chunks = append(b.chunks, smallChunk{
		XORChunk: chunk,
		start:    int64(start),
		end:      int64(start),
	})

	b.appender = appender
	b.remainingSamples = samplesPerChunk
	return nil
}

func (b *bigchunk) Marshal(wio io.Writer) error {
	w := writer{wio}
	if err := w.WriteVarInt16(uint16(len(b.chunks))); err != nil {
		return err
	}
	for _, chunk := range b.chunks {
		buf := chunk.Bytes()
		if err := w.WriteVarInt16(uint16(len(buf))); err != nil {
			return err
		}
		if _, err := w.Write(buf); err != nil {
			return err
		}
	}
	return nil
}

func (b *bigchunk) MarshalToBuf(buf []byte) error {
	writer := bytes.NewBuffer(buf)
	return b.Marshal(writer)
}

func (b *bigchunk) UnmarshalFromBuf(buf []byte) error {
	r := reader{buf: buf}
	numChunks, err := r.ReadUint16()
	if err != nil {
		return err
	}

	b.chunks = make([]smallChunk, 0, numChunks+1) // allow one extra space in case we want to add new data
	for i := uint16(0); i < numChunks; i++ {
		chunkLen, err := r.ReadUint16()
		if err != nil {
			return err
		}

		chunkBuf, err := r.ReadBytes(int(chunkLen))
		if err != nil {
			return err
		}

		chunk, err := chunkenc.FromData(chunkenc.EncXOR, chunkBuf)
		if err != nil {
			return err
		}

		start, end, err := firstAndLastTimes(chunk)
		if err != nil {
			return err
		}

		b.chunks = append(b.chunks, smallChunk{
			XORChunk: chunk.(*chunkenc.XORChunk),
			start:    int64(start),
			end:      int64(end),
		})
	}
	return nil
}

func (b *bigchunk) Encoding() Encoding {
	return Bigchunk
}

func (b *bigchunk) Utilization() float64 {
	return 1.0
}

func (b *bigchunk) Len() int {
	sum := 0
	for _, c := range b.chunks {
		sum += c.NumSamples()
	}
	return sum
}

func (b *bigchunk) Size() int {
	sum := 2 // For the number of sub chunks.
	for _, c := range b.chunks {
		sum += 2 // For the length of the sub chunk.
		sum += len(c.Bytes())
	}
	return sum
}

func (b *bigchunk) NewIterator() Iterator {
	var it chunkenc.Iterator
	if len(b.chunks) > 0 {
		it = b.chunks[0].Iterator()
	} else {
		it = chunkenc.NewNopIterator()
	}
	return &bigchunkIterator{
		bigchunk: b,
		curr:     it,
	}
}

func (b *bigchunk) Slice(start, end model.Time) Chunk {
	i, j := 0, len(b.chunks)
	for k := 0; k < len(b.chunks); k++ {
		if b.chunks[k].end < int64(start) {
			i = k + 1
		}
		if b.chunks[k].start > int64(end) {
			j = k
			break
		}
	}
	return &bigchunk{
		chunks: b.chunks[i:j],
	}
}

type writer struct {
	io.Writer
}

func (w writer) WriteVarInt16(i uint16) error {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], i)
	_, err := w.Write(b[:])
	return err
}

type reader struct {
	i   int
	buf []byte
}

func (r *reader) ReadUint16() (uint16, error) {
	if r.i+2 > len(r.buf) {
		return 0, errOutOfBounds
	}
	result := binary.LittleEndian.Uint16(r.buf[r.i:])
	r.i += 2
	return result, nil
}

func (r *reader) ReadBytes(count int) ([]byte, error) {
	if r.i+count > len(r.buf) {
		return nil, errOutOfBounds
	}
	result := r.buf[r.i : r.i+count]
	r.i += count
	return result, nil
}

type bigchunkIterator struct {
	*bigchunk

	curr chunkenc.Iterator
	i    int
}

func (it *bigchunkIterator) FindAtOrAfter(target model.Time) bool {
	if it.i >= len(it.chunks) {
		return false
	}

	// If the seek is outside the current chunk, use the index to find the right
	// chunk.
	if int64(target) < it.chunks[it.i].start || int64(target) > it.chunks[it.i].end {
		it.curr = nil
		for it.i = 0; it.i < len(it.chunks) && int64(target) > it.chunks[it.i].end; it.i++ {
		}
	}

	if it.i >= len(it.chunks) {
		return false
	}

	if it.curr == nil {
		it.curr = it.chunks[it.i].Iterator()
	} else if t, _ := it.curr.At(); int64(target) <= t {
		it.curr = it.chunks[it.i].Iterator()
	}

	for it.curr.Next() {
		t, _ := it.curr.At()
		if t >= int64(target) {
			return true
		}
	}
	return false
}

func (it *bigchunkIterator) Scan() bool {
	if it.curr.Next() {
		return true
	}
	if err := it.curr.Err(); err != nil {
		return false
	}

	for it.i < len(it.chunks)-1 {
		it.i++
		it.curr = it.chunks[it.i].Iterator()
		if it.curr.Next() {
			return true
		}
	}
	return false
}

func (it *bigchunkIterator) Value() model.SamplePair {
	t, v := it.curr.At()
	return model.SamplePair{
		Timestamp: model.Time(t),
		Value:     model.SampleValue(v),
	}
}

func (it *bigchunkIterator) Batch(size int) Batch {
	var result Batch
	j := 0
	for j < size {
		t, v := it.curr.At()
		result.Timestamps[j] = t
		result.Values[j] = v
		j++

		if j < size && !it.Scan() {
			break
		}
	}
	result.Length = j
	return result
}

func (it *bigchunkIterator) Err() error {
	if it.curr != nil {
		return it.curr.Err()
	}
	return nil
}

func firstAndLastTimes(c chunkenc.Chunk) (int64, int64, error) {
	var (
		first    int64
		last     int64
		firstSet bool
		iter     = c.Iterator()
	)
	for iter.Next() {
		t, _ := iter.At()
		if !firstSet {
			first = t
			firstSet = true
		}
		last = t
	}
	return first, last, iter.Err()
}
