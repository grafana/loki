package thrift

import (
	"fmt"
	"reflect"
)

// UnionMember is implemented by the concrete member types of a thrift union.
//
// FieldID returns the thrift field id that the member is encoded under. The id
// is intrinsic to the member type, which makes it impossible for a union to
// register the same member under two different ids.
//
// Implementations must declare FieldID on a pointer receiver and must not
// dereference it: unions register their members as typed nil pointers, and the
// codec calls FieldID on those values to build its dispatch table.
type UnionMember interface {
	FieldID() int16
}

// Union is implemented by structs that encode as a thrift union.
//
// A union struct has exactly one field, of an interface type, holding at most
// one member. A nil field means the union is not set, and the struct is skipped
// when it appears as an optional field of another struct.
//
// UnionMembers returns the members the union may hold, as typed nil pointers:
//
//	var logicalTypeMembers = []thrift.UnionMember{
//		(*StringType)(nil),
//		(*MapType)(nil),
//	}
//
//	func (*LogicalType) UnionMembers() []thrift.UnionMember { return logicalTypeMembers }
//
// The returned slice is read once, when the codec for the union is built, so
// implementations should return a package-level value rather than construct it
// on each call.
type Union interface {
	UnionMembers() []UnionMember
}

var unionType = reflect.TypeFor[Union]()

// isUnion reports whether t is a union struct.
//
// Unlike Value, which is probed on the value type because its implementations
// (Null, Slice) are value wrappers with value receivers, Union is probed on the
// pointer type. Union structs live in packages that declare their methods on
// pointer receivers, and a union only ever appears as an addressable struct
// field, so requiring the value method set would buy nothing and would force
// those packages into a mixed receiver set.
func isUnion(t reflect.Type) bool {
	return t.Kind() == reflect.Struct && reflect.PointerTo(t).Implements(unionType)
}

// unionMember describes one member of a union after validation.
type unionMember struct {
	id int16
	// typ is the member struct type, i.e. the element of the registered *T.
	typ reflect.Type
	// interned holds a shared *T for zero-sized members, which carry no state
	// and so can be handed out from every decode without allocating.
	interned reflect.Value
}

// unionLayout is the validated shape of a union struct, independent of whether
// it is being encoded or decoded.
type unionLayout struct {
	typ reflect.Type
	// field is the index of the interface field holding the member.
	field   int
	members []unionMember
	minID   int16
	maxID   int16
}

// unionLayoutOf validates t and describes its shape, panicking on any structural
// problem. Union codecs are built once per type, at the first encode or decode,
// so these panics surface at startup rather than on a hot path.
func unionLayoutOf(t reflect.Type) *unionLayout {
	if t.Kind() != reflect.Struct {
		panic(fmt.Errorf("thrift union %s is not a struct", t))
	}
	if t.NumField() != 1 {
		panic(fmt.Errorf("thrift union %s must have exactly one field, found %d", t, t.NumField()))
	}
	field := t.Field(0)
	if field.Type.Kind() != reflect.Interface {
		panic(fmt.Errorf("thrift union %s field %s must be an interface, found %s", t, field.Name, field.Type))
	}

	members := reflect.New(t).Interface().(Union).UnionMembers()
	if len(members) == 0 {
		panic(fmt.Errorf("thrift union %s declares no members", t))
	}

	layout := &unionLayout{typ: t, field: 0}
	seen := make(map[int16]reflect.Type, len(members))

	for _, m := range members {
		if m == nil {
			panic(fmt.Errorf("thrift union %s declares a nil member", t))
		}
		mt := reflect.TypeOf(m)
		if mt.Kind() != reflect.Ptr || mt.Elem().Kind() != reflect.Struct {
			panic(fmt.Errorf("thrift union %s member %s must be a pointer to a struct", t, mt))
		}
		if !mt.AssignableTo(field.Type) {
			panic(fmt.Errorf("thrift union %s member %s is not assignable to %s", t, mt, field.Type))
		}
		id := m.FieldID()
		if id <= 0 {
			panic(fmt.Errorf("thrift union %s member %s has an invalid field id %d", t, mt, id))
		}
		if prev, ok := seen[id]; ok {
			panic(fmt.Errorf("thrift union %s field id %d is present multiple times with types %s and %s", t, id, prev, mt))
		}
		seen[id] = mt

		member := unionMember{id: id, typ: mt.Elem()}
		if member.typ.Size() == 0 {
			member.interned = reflect.New(member.typ)
		}
		layout.members = append(layout.members, member)

		if layout.minID == 0 || id < layout.minID {
			layout.minID = id
		}
		if id > layout.maxID {
			layout.maxID = id
		}
	}

	return layout
}

// nullFunc reports the union as unset when its member field is nil.
func (layout *unionLayout) nullFunc() NullFunc {
	field := layout.field
	return func(v reflect.Value) bool { return v.Field(field).IsNil() }
}

type unionEncoderMember struct {
	id     int16
	encode EncodeFunc
}

type unionEncoder struct {
	typ    reflect.Type
	field  int
	byType map[reflect.Type]unionEncoderMember
}

func (enc *unionEncoder) encode(w Writer, v reflect.Value, flags Flags) error {
	x := v.Field(enc.field)

	if !x.IsNil() {
		p := x.Elem()

		m, ok := enc.byType[p.Type()]
		if !ok {
			return fmt.Errorf("thrift: %s is not a member of union %s", p.Type(), enc.typ)
		}

		field := Field{ID: m.id, Type: STRUCT}
		// A union writes a single field, so the previous field id is always
		// zero and the delta is the id itself. This mirrors structEncoder so
		// that unions and structs produce identical bytes for the same field.
		if flags.Have(useDeltaEncoding) && field.ID <= 15 {
			field.Delta = true
		}
		if err := w.WriteField(field); err != nil {
			return err
		}

		// A typed nil member encodes as an empty struct, matching the way
		// encodeFuncPtrOf substitutes the zero value for a nil pointer.
		if p.IsNil() {
			p = reflect.New(p.Type().Elem())
		}
		if err := m.encode(w, p.Elem(), flags); err != nil {
			return err
		}
	}

	return w.WriteField(Field{Type: STOP})
}

func encodeFuncUnionOf(t reflect.Type, seen EncodeFuncCache) EncodeFunc {
	layout := unionLayoutOf(t)

	enc := &unionEncoder{
		typ:    t,
		field:  layout.field,
		byType: make(map[reflect.Type]unionEncoderMember, len(layout.members)),
	}
	encode := enc.encode
	seen[t] = encode

	for _, m := range layout.members {
		enc.byType[reflect.PointerTo(m.typ)] = unionEncoderMember{
			id:     m.id,
			encode: EncodeFuncOf(m.typ, seen),
		}
	}

	return encode
}

type unionDecoderMember struct {
	// interned is a shared *T for zero-sized members, invalid otherwise.
	interned reflect.Value
	typ      reflect.Type
	// ptrTyp is *typ, kept to test whether a previously decoded member can
	// be reused as the decode target without allocating.
	ptrTyp reflect.Type
	decode DecodeFunc
}

type unionDecoder struct {
	field   int
	minID   int16
	members []unionDecoderMember
}

func (dec *unionDecoder) decode(r Reader, v reflect.Value, flags Flags) error {
	flags = flags.Only(decodeFlags)

	// Reset so that decoding into a reused value cannot retain a stale
	// member, but remember the previous member: when the input holds a
	// member of the same type, its allocation is reused instead of
	// allocating a fresh value.
	x := v.Field(dec.field)
	prev := x.Elem() // invalid when the union was unset
	x.SetZero()

	lastFieldID := int16(0)
	numFields := 0

	for {
		f, err := r.ReadField()
		if err != nil {
			if numFields > 0 {
				err = dontExpectEOF(err)
			}
			return err
		}

		if f.Type == STOP {
			return nil
		}

		if f.Delta {
			f.ID += lastFieldID
			f.Delta = false
		}

		i := int(f.ID) - int(dec.minID)
		known := i >= 0 && i < len(dec.members) && dec.members[i].decode != nil

		switch {
		case !known:
			// An id we do not know: a member added by a newer writer. Skip it
			// and leave the union unset rather than failing the whole decode.
			if err := skip(r, f.Type); err != nil {
				return with(dontExpectEOF(err), &decodeErrorField{cause: f})
			}

		case f.Type != STRUCT:
			if flags.Have(Strict) {
				return &TypeMismatch{item: "union member", Expect: STRUCT, Found: f.Type}
			}
			if err := skip(r, f.Type); err != nil {
				return with(dontExpectEOF(err), &decodeErrorField{cause: f})
			}

		default:
			m := &dec.members[i]

			p := m.interned
			if !p.IsValid() {
				if prev.IsValid() && prev.Type() == m.ptrTyp && !prev.IsNil() {
					p = prev
					p.Elem().SetZero()
				} else {
					p = reflect.New(m.typ)
				}
			}
			if err := m.decode(r, p.Elem(), flags); err != nil {
				return with(dontExpectEOF(err), &decodeErrorField{cause: f})
			}
			x.Set(p)
		}

		lastFieldID = f.ID
		numFields++
	}
}

func decodeFuncUnionOf(t reflect.Type, seen DecodeFuncCache) DecodeFunc {
	layout := unionLayoutOf(t)

	dec := &unionDecoder{
		field:   layout.field,
		minID:   layout.minID,
		members: make([]unionDecoderMember, (layout.maxID-layout.minID)+1),
	}
	decode := dec.decode
	seen[t] = decode

	for _, m := range layout.members {
		dec.members[m.id-layout.minID] = unionDecoderMember{
			interned: m.interned,
			typ:      m.typ,
			ptrTyp:   reflect.PointerTo(m.typ),
			decode:   DecodeFuncOf(m.typ, seen),
		}
	}

	return decode
}
