package decoder

import (
	"reflect"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
)

type sliceDecoder struct {
	elemType reflect.Type
	// slicePtrType is the type descriptor of the pointer to the slice.
	slicePtrType unsafe.Pointer
	valueDecoder Decoder
	size         uintptr
	// bufPool holds the buffers into which the elements of a long array are decoded before its length is known
	// ( see decodeLong ). A buffer in the pool has only zero values, so that an element is decoded into a zero
	// value.
	bufPool sync.Pool
	// lastLen is the length of the array which the decoder decoded last, which an empty slice is allocated for,
	// up to inPlaceSliceLength: the arrays of a kind are often of a length. It is only a hint, for any goroutine.
	lastLen    atomic.Int32
	structName string
	fieldName  string
	// typ is the type of the slice, which the type errors report.
	typ reflect.Type
}

// If use reflect.SliceHeader, data type is uintptr.
// In this case, Go compiler cannot trace reference created by newArray().
// So, define using unsafe.Pointer as data type
type sliceHeader struct {
	data unsafe.Pointer
	len  int
	cap  int
}

// sliceBuf is a buffer of the elements of a slice, allocated and grown by reflect.Value.Grow,
// so that its memory has the type of the elements.
type sliceBuf struct {
	hdr sliceHeader
}

const (
	// inPlaceSliceLength is the number of the elements of an array which are decoded into the slice itself, if it
	// has no more capacity: most arrays are no longer, and the elements of a longer one are decoded into a buffer
	// from there, so that its slice is allocated once, of its length.
	inPlaceSliceLength = 64
	// defaultSliceCapacity is the capacity of a new buffer of the elements.
	defaultSliceCapacity = 4
)

func newSliceDecoder(dec Decoder, elemType reflect.Type, size uintptr, structName, fieldName string) *sliceDecoder {
	return &sliceDecoder{
		typ:          reflect.SliceOf(elemType),
		valueDecoder: dec,
		elemType:     elemType,
		slicePtrType: ptrTypeOf(reflect.SliceOf(elemType)),
		size:         size,
		bufPool: sync.Pool{
			New: func() any {
				return &sliceBuf{}
			},
		},
		structName: structName,
		fieldName:  fieldName,
	}
}

// sliceValue returns the reflect.Value of the slice whose header is at p.
func (d *sliceDecoder) sliceValue(p *sliceHeader) reflect.Value {
	return valueAt(d.slicePtrType, unsafe.Pointer(p))
}

// grow makes the capacity of the slice at dst n or more, keeping its first length elements, as append does.
func (d *sliceDecoder) grow(dst *sliceHeader, length, n int) {
	if n <= dst.cap {
		return
	}
	dst.len = length
	if n < defaultSliceCapacity {
		n = defaultSliceCapacity
	}
	d.sliceValue(dst).Grow(n - length)
}

// decodeLong decodes the elements of the array from cursor, the one at index n, which follows the n elements
// decoded into the slice at dst, into a buffer whose length is not known yet, and stores them all into the slice,
// allocated once, of their length. It is not inlined, so that Decode keeps the size it had: most arrays are shorter.
//
//go:noinline
func (d *sliceDecoder) decodeLong(ctx *RuntimeContext, cursor, depth int64, dst *sliceHeader, n int) (int64, error) {
	buf := ctx.Buf
	elems := d.bufPool.Get().(*sliceBuf)
	d.growBuf(elems, 0, 2*n)
	elems.hdr.len = n
	head := *dst
	head.len = n
	reflect.Copy(d.sliceValue(&elems.hdr), d.sliceValue(&head))
	idx := n
	for {
		d.growBuf(elems, idx, idx+1)
		ep := unsafe.Add(elems.hdr.data, uintptr(idx)*d.size)
		c, err := d.valueDecoder.Decode(ctx, cursor, depth, ep)
		if err != nil {
			d.store(dst, elems, idx+1)
			d.releaseBuf(elems, idx+1)
			return 0, err
		}
		cursor = skipWhiteSpace(buf, c)
		switch buf[cursor] {
		case ']':
			d.store(dst, elems, idx+1)
			d.releaseBuf(elems, idx+1)
			d.lastLen.Store(int32(min(idx+1, inPlaceSliceLength)))
			return cursor + 1, nil
		case ',':
			idx++
		default:
			d.store(dst, elems, idx+1)
			d.releaseBuf(elems, idx+1)
			return 0, errors.ErrInvalidCharacter(buf[cursor], "slice", cursor)
		}
		cursor++
	}
}

// growBuf makes the capacity of the buffer n or more, keeping its first length elements.
// The elements after them are zero values.
func (d *sliceDecoder) growBuf(buf *sliceBuf, length, n int) {
	d.grow(&buf.hdr, length, n)
}

// releaseBuf clears the first n elements of the buffer, which are all it may have used,
// and puts it back to the pool: it keeps nothing the decoded value refers to.
func (d *sliceDecoder) releaseBuf(buf *sliceBuf, n int) {
	if n > 0 {
		buf.hdr.len = n
		d.sliceValue(&buf.hdr).Clear()
	}
	buf.hdr.len = 0
	d.bufPool.Put(buf)
}

// store copies the n elements of the buffer to the slice at dst: into its array when it has room
// for them, or else into a new array of their length.
func (d *sliceDecoder) store(dst *sliceHeader, buf *sliceBuf, n int) {
	if dst.cap < n {
		*dst = sliceHeader{}
		d.sliceValue(dst).Grow(n)
	}
	dst.len = n
	buf.hdr.len = n
	reflect.Copy(d.sliceValue(dst), d.sliceValue(&buf.hdr))
}

func (d *sliceDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	depth++
	if depth > maxDecodeNestingDepth {
		return 0, errors.ErrExceededMaxDepth(buf[cursor], cursor)
	}

	for {
		switch buf[cursor] {
		case ' ', '\n', '\t', '\r':
			cursor++
			continue
		case 'n':
			if err := validateNull(buf, cursor); err != nil {
				return 0, err
			}
			cursor += 4
			*(*sliceHeader)(p) = sliceHeader{}
			return cursor, nil
		case '[':
			cursor++
			cursor = skipWhiteSpace(buf, cursor)
			dst := (*sliceHeader)(p)
			if buf[cursor] == ']' {
				if dst.data == nil {
					dst.data = unsafe.Pointer(&zeroBase)
				} else {
					dst.len = 0
				}
				cursor++
				return cursor, nil
			}
			// The elements are decoded into the slice, which grows as append grows it: the elements of its array are
			// decoded into, the ones after its length too, as encoding/json does. The elements beyond its capacity
			// and inPlaceSliceLength are decoded into a buffer ( see decodeLong ).
			limit := max(dst.cap, inPlaceSliceLength)
			if dst.cap == 0 {
				// the length of the last array, which an array of the kind is likely to have
				if n := int(d.lastLen.Load()); n > 0 {
					d.grow(dst, 0, min(n, inPlaceSliceLength))
				}
			}
			idx := 0
			for {
				if idx == limit {
					return d.decodeLong(ctx, cursor, depth, dst, idx)
				}
				d.grow(dst, idx, idx+1)
				ep := unsafe.Add(dst.data, uintptr(idx)*d.size)
				c, err := d.valueDecoder.Decode(ctx, cursor, depth, ep)
				if err != nil {
					dst.len = idx + 1
					return 0, err
				}
				cursor = skipWhiteSpace(buf, c)
				switch buf[cursor] {
				case ']':
					dst.len = idx + 1
					d.lastLen.Store(int32(idx + 1))
					cursor++
					return cursor, nil
				case ',':
					idx++
				default:
					dst.len = idx + 1
					return 0, errors.ErrInvalidCharacter(buf[cursor], "slice", cursor)
				}
				cursor++
			}
		default:
			return d.decodeOther(ctx, cursor, depth-1)
		}
	}
}

// decodeOther skips the value at cursor, which is not an array: a value of another kind is a type error, and
// anything else a syntax error. It is a function of its own, so that Decode keeps the size it had.
//
//go:noinline
func (d *sliceDecoder) decodeOther(ctx *RuntimeContext, cursor, depth int64) (int64, error) {
	if isOtherValue(ctx.Buf[cursor], arrayValue) {
		return ctx.skipTypeError(cursor, depth, d.typ)
	}
	return 0, errors.ErrUnexpectedEndOfJSON("slice", cursor)
}

func (d *sliceDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	buf := ctx.Buf
	depth++
	if depth > maxDecodeNestingDepth {
		return nil, 0, errors.ErrExceededMaxDepth(buf[cursor], cursor)
	}

	ret := [][]byte{}
	for {
		switch buf[cursor] {
		case ' ', '\n', '\t', '\r':
			cursor++
			continue
		case 'n':
			if err := validateNull(buf, cursor); err != nil {
				return nil, 0, err
			}
			cursor += 4
			return [][]byte{nullbytes}, cursor, nil
		case '[':
			cursor++
			cursor = skipWhiteSpace(buf, cursor)
			if buf[cursor] == ']' {
				cursor++
				return ret, cursor, nil
			}
			idx := 0
			for {
				child, found, err := ctx.Option.Path.node.Index(idx)
				if err != nil {
					return nil, 0, err
				}
				if found {
					if child != nil {
						oldPath := ctx.Option.Path.node
						ctx.Option.Path.node = child
						paths, c, err := d.valueDecoder.DecodePath(ctx, cursor, depth)
						if err != nil {
							return nil, 0, err
						}
						ctx.Option.Path.node = oldPath
						ret = append(ret, paths...)
						cursor = c
					} else {
						start := cursor
						end, err := skipValue(buf, cursor, depth)
						if err != nil {
							return nil, 0, err
						}
						ret = append(ret, buf[start:end])
						cursor = end
					}
				} else {
					c, err := skipValue(buf, cursor, depth)
					if err != nil {
						return nil, 0, err
					}
					cursor = c
				}
				cursor = skipWhiteSpace(buf, cursor)
				switch buf[cursor] {
				case ']':
					cursor++
					return ret, cursor, nil
				case ',':
					idx++
				default:
					return nil, 0, errors.ErrInvalidCharacter(buf[cursor], "slice", cursor)
				}
				cursor++
			}
		case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			return nil, 0, &errors.UnmarshalTypeError{Value: "number", Type: d.typ, Offset: cursor}
		default:
			return nil, 0, errors.ErrUnexpectedEndOfJSON("slice", cursor)
		}
	}
}
