package encoding

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
)

const samplesPerChunk = 120

var errOutOfBounds = errors.New("out of bounds")

type smallChunk struct {
	chunkenc.XORChunk
	start int64
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

func (b *bigchunk) Add(sample model.SamplePair) (Chunk, error) {
	if b.remainingSamples == 0 {
		if bigchunkSizeCapBytes > 0 && b.Size() > bigchunkSizeCapBytes {
			return addToOverflowChunk(sample)
		}
		if err := b.addNextChunk(sample.Timestamp); err != nil {
			return nil, err
		}
	}

	b.appender.Append(int64(sample.Timestamp), float64(sample.Value))
	b.remainingSamples--
	return nil, nil
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
			b.chunks[l-1].XORChunk = *compacted.(*chunkenc.XORChunk)
		}
	}

	// Explicitly reallocate slice to avoid up to 2x overhead if we let append() do it
	if len(b.chunks)+1 > cap(b.chunks) {
		newChunks := make([]smallChunk, len(b.chunks), len(b.chunks)+1)
		copy(newChunks, b.chunks)
		b.chunks = newChunks
	}
	b.chunks = append(b.chunks, smallChunk{
		XORChunk: *chunkenc.NewXORChunk(),
		start:    int64(start),
	})

	appender, err := b.chunks[len(b.chunks)-1].Appender()
	if err != nil {
		return err
	}
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
	var reuseIter chunkenc.Iterator
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

		var start int64
		start, reuseIter, err = firstTime(chunk, reuseIter)
		if err != nil {
			return err
		}

		b.chunks = append(b.chunks, smallChunk{
			XORChunk: *chunk.(*chunkenc.XORChunk),
			start:    int64(start),
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

func (b *bigchunk) NewIterator(reuseIter Iterator) Iterator {
	if bci, ok := reuseIter.(*bigchunkIterator); ok {
		bci.bigchunk = b
		bci.i = 0
		if len(b.chunks) > 0 {
			bci.curr = b.chunks[0].Iterator(bci.curr)
		} else {
			bci.curr = chunkenc.NewNopIterator()
		}
		return bci
	}
	var it chunkenc.Iterator
	if len(b.chunks) > 0 {
		it = b.chunks[0].Iterator(it)
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
		if b.chunks[k].start <= int64(start) {
			i = k
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

func (b *bigchunk) Rebound(start, end model.Time) (Chunk, error) {
	return reboundChunk(b, start, end)
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
	if int64(target) < it.chunks[it.i].start ||
		(it.i+1 < len(it.chunks) && int64(target) >= it.chunks[it.i+1].start) {
		it.curr = nil
		for it.i = 0; it.i+1 < len(it.chunks) && int64(target) >= it.chunks[it.i+1].start; it.i++ {
		}
	}

	if it.curr == nil {
		it.curr = it.chunks[it.i].Iterator(it.curr)
	} else if t, _ := it.curr.At(); int64(target) <= t {
		it.curr = it.chunks[it.i].Iterator(it.curr)
	}

	for it.curr.Next() {
		t, _ := it.curr.At()
		if t >= int64(target) {
			return true
		}
	}
	// Timestamp is after the end of that chunk - if there is another chunk
	// then the position we need is at the beginning of it.
	if it.i+1 < len(it.chunks) {
		it.i++
		it.curr = it.chunks[it.i].Iterator(it.curr)
		it.curr.Next()
		return true
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
		it.curr = it.chunks[it.i].Iterator(it.curr)
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

func firstTime(c chunkenc.Chunk, iter chunkenc.Iterator) (int64, chunkenc.Iterator, error) {
	var first int64
	iter = c.Iterator(iter)
	if iter.Next() {
		first, _ = iter.At()
	}
	return first, iter, iter.Err()
}
