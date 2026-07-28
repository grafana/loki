package push

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
	"unsafe"
)

// Stream contains a unique labels set as a string and a set of entries for it.
// We are not using the proto generated version but this custom one so that we
// can improve serialization see benchmark.
type Stream struct {
	Labels  string  `protobuf:"bytes,1,opt,name=labels,proto3" json:"labels"`
	Entries []Entry `protobuf:"bytes,2,rep,name=entries,proto3,customtype=EntryAdapter" json:"entries"`
	Hash    uint64  `protobuf:"varint,3,opt,name=hash,proto3" json:"-"`
	// Field 4 is reserved: it held a single flat list of structured metadata shared by
	// every entry of the stream, which the pool below replaced before it ever shipped.
	// Both codecs skip it, see Stream.Unmarshal.
	//
	// SharedStructuredMetadataSets is the pool of structured metadata sets shared by the
	// entries of this stream. Entries reference a set by its position in the pool through
	// Entry.SharedResourceRef and Entry.SharedScopeRef, so a set is carried once per
	// stream no matter how many entries, or how many resource and scope combinations,
	// point at it. Resolve an entry's sets with Stream.SharedFor.
	//
	// The backing array of a set is immutable. One array can be shared by the pools of
	// several streams: the OTLP handler pools a resource's attributes into every stream its
	// entries were split across, and sharding, time windowing and Kafka record splitting all
	// hand the same pool to several streams. A consumer that has to change a set must
	// therefore replace it, `sets[i].Attrs = fresh`, and never write through the array it
	// found there. Only this slice of sets is stream-local.
	//
	// The pool is internal to Loki's OTLP ingest pipeline: it is stripped from external
	// pushes and hidden from JSON, like Hash.
	SharedStructuredMetadataSets []SharedStructuredMetadataSet `protobuf:"bytes,5,rep,name=sharedStructuredMetadataSets,proto3" json:"-"`
}

// Entry is a log entry with a timestamp.
type Entry struct {
	Timestamp          time.Time     `protobuf:"bytes,1,opt,name=timestamp,proto3,stdtime" json:"ts"`
	Line               string        `protobuf:"bytes,2,opt,name=line,proto3" json:"line"`
	StructuredMetadata LabelsAdapter `protobuf:"bytes,3,opt,name=structuredMetadata,proto3" json:"structuredMetadata,omitempty"`
	Parsed             LabelsAdapter `protobuf:"bytes,4,opt,name=parsed,proto3" json:"parsed,omitempty"`
	// SharedResourceRef and SharedScopeRef are 1-based indexes into the
	// SharedStructuredMetadataSets of the stream carrying this entry, of respectively the
	// OTLP resource attributes and the OTLP scope attributes of the entry. 0 means none.
	//
	// The references are context dependent: they only resolve against the pool of their
	// own stream, so an entry moved to another stream must have them remapped to that
	// stream's pool. Use Stream.SharedFor rather than indexing a pool by hand.
	//
	// They are internal to Loki's OTLP ingest pipeline and are hidden from JSON, like
	// Stream.Hash: query responses never carry them, values sent by external clients are
	// discarded, and they exist to defer expanding shared structured metadata into every
	// entry.
	SharedResourceRef uint32 `protobuf:"varint,5,opt,name=sharedResourceRef,proto3" json:"-"`
	SharedScopeRef    uint32 `protobuf:"varint,6,opt,name=sharedScopeRef,proto3" json:"-"`
}

// MarshalJSON implements json.Marshaler.
// In Loki, this method should only be used by the
// Legacy encoder used when hitting the deprecated /api/promt/query endpoint.
// We will ignore the categorized labels and only return the stream labels.
func (m *Stream) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Labels  string  `json:"labels"`
		Entries []Entry `json:"entries"`
	}{
		Labels:  m.Labels,
		Entries: m.Entries,
	})
}

// MarshalJSON implements json.Marshaler.
// In Loki, this method should only be used by the
// Legacy encoder used when hitting the deprecated /api/promt/query endpoint.
// We will ignore the structured metadata.
func (m *Entry) MarshalJSON() ([]byte, error) {
	type raw Entry
	e := raw(*m)
	e.StructuredMetadata = nil
	return json.Marshal(e)
}

// LabelAdapter should be a copy of the Prometheus labels.Label type.
// We cannot import Prometheus in this package because it would create many dependencies
// in other projects importing this package. Instead, we copy the definition here, which should
// be kept in sync with the original, so it can be cast to the prometheus type.
type LabelAdapter struct {
	Name, Value string
}

// LabelsAdapter is equivalent to the Prometheus labels.Labels type.
type LabelsAdapter []LabelAdapter

// MarshalJSON implements json.Marshaler.
// Should be kept in sync with Prometheus's labels.Labels implementation.
func (ls LabelsAdapter) MarshalJSON() ([]byte, error) {
	labelsMap := make(map[string]string, len(ls))
	for _, l := range ls {
		labelsMap[l.Name] = l.Value
	}
	return json.Marshal(labelsMap)
}

// UnmarshalJSON implements json.Unmarshaler.
// Should be kept in sync with Prometheus's labels.Labels implementation.
func (ls *LabelsAdapter) UnmarshalJSON(b []byte) error {
	var m map[string]string

	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}

	*ls = make([]LabelAdapter, 0, len(m))
	for k, v := range m {
		*ls = append(*ls, LabelAdapter{Name: k, Value: v})
	}
	slices.SortFunc(*ls, func(a, b LabelAdapter) int { return strings.Compare(a.Name, b.Name) })

	return nil
}

func (m *LabelAdapter) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *LabelAdapter) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *LabelAdapter) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if len(m.Value) > 0 {
		i -= len(m.Value)
		copy(dAtA[i:], m.Value)
		i = encodeVarintPush(dAtA, i, uint64(len(m.Value)))
		i--
		dAtA[i] = 0x12
	}
	if len(m.Name) > 0 {
		i -= len(m.Name)
		copy(dAtA[i:], m.Name)
		i = encodeVarintPush(dAtA, i, uint64(len(m.Name)))
		i--
		dAtA[i] = 0xa
	}
	return len(dAtA) - i, nil
}

func (m *Stream) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *Stream) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *Stream) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if len(m.SharedStructuredMetadataSets) > 0 {
		for iNdEx := len(m.SharedStructuredMetadataSets) - 1; iNdEx >= 0; iNdEx-- {
			{
				size, err := m.SharedStructuredMetadataSets[iNdEx].MarshalToSizedBuffer(dAtA[:i])
				if err != nil {
					return 0, err
				}
				i -= size
				i = encodeVarintPush(dAtA, i, uint64(size))
			}
			i--
			dAtA[i] = 0x2a
		}
	}
	if m.Hash != 0 {
		i = encodeVarintPush(dAtA, i, m.Hash)
		i--
		dAtA[i] = 0x18
	}
	if len(m.Entries) > 0 {
		for iNdEx := len(m.Entries) - 1; iNdEx >= 0; iNdEx-- {
			{
				size, err := m.Entries[iNdEx].MarshalToSizedBuffer(dAtA[:i])
				if err != nil {
					return 0, err
				}
				i -= size
				i = encodeVarintPush(dAtA, i, uint64(size))
			}
			i--
			dAtA[i] = 0x12
		}
	}
	if len(m.Labels) > 0 {
		i -= len(m.Labels)
		copy(dAtA[i:], m.Labels)
		i = encodeVarintPush(dAtA, i, uint64(len(m.Labels)))
		i--
		dAtA[i] = 0xa
	}
	return len(dAtA) - i, nil
}

func (m *Entry) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *Entry) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *Entry) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if m.SharedScopeRef != 0 {
		i = encodeVarintPush(dAtA, i, uint64(m.SharedScopeRef))
		i--
		dAtA[i] = 0x30
	}
	if m.SharedResourceRef != 0 {
		i = encodeVarintPush(dAtA, i, uint64(m.SharedResourceRef))
		i--
		dAtA[i] = 0x28
	}
	if len(m.Parsed) > 0 {
		for iNdEx := len(m.Parsed) - 1; iNdEx >= 0; iNdEx-- {
			{
				size, err := m.Parsed[iNdEx].MarshalToSizedBuffer(dAtA[:i])
				if err != nil {
					return 0, err
				}
				i -= size
				i = encodeVarintPush(dAtA, i, uint64(size))
			}
			i--
			dAtA[i] = 0x22
		}
	}
	if len(m.StructuredMetadata) > 0 {
		for iNdEx := len(m.StructuredMetadata) - 1; iNdEx >= 0; iNdEx-- {
			{
				size, err := (*LabelAdapter)(&m.StructuredMetadata[iNdEx]).MarshalToSizedBuffer(dAtA[:i])
				if err != nil {
					return 0, err
				}
				i -= size
				i = encodeVarintPush(dAtA, i, uint64(size))
			}
			i--
			dAtA[i] = 0x1a
		}
	}
	if len(m.Line) > 0 {
		i -= len(m.Line)
		copy(dAtA[i:], m.Line)
		i = encodeVarintPush(dAtA, i, uint64(len(m.Line)))
		i--
		dAtA[i] = 0x12
	}
	n7, err7 := StdTimeMarshalTo(m.Timestamp, dAtA[i-SizeOfStdTime(m.Timestamp):])
	if err7 != nil {
		return 0, err7
	}
	i -= n7
	i = encodeVarintPush(dAtA, i, uint64(n7))
	i--
	dAtA[i] = 0xa
	return len(dAtA) - i, nil
}

func (m *Stream) Unmarshal(dAtA []byte) error {
	l := len(dAtA)
	iNdEx := 0
	for iNdEx < l {
		preIndex := iNdEx
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return ErrIntOverflowPush
			}
			if iNdEx >= l {
				return io.ErrUnexpectedEOF
			}
			b := dAtA[iNdEx]
			iNdEx++
			wire |= uint64(b&0x7F) << shift
			if b < 0x80 {
				break
			}
		}
		fieldNum := int32(wire >> 3)
		wireType := int(wire & 0x7)
		if wireType == 4 {
			return fmt.Errorf("proto: StreamAdapter: wiretype end group for non-group")
		}
		if fieldNum <= 0 {
			return fmt.Errorf("proto: StreamAdapter: illegal tag %d (wire type %d)", fieldNum, wire)
		}
		switch fieldNum {
		case 1:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field StructuredMetadata", wireType)
			}
			var stringLen uint64
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowPush
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				stringLen |= uint64(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			intStringLen := int(stringLen)
			if intStringLen < 0 {
				return ErrInvalidLengthPush
			}
			postIndex := iNdEx + intStringLen
			if postIndex < 0 {
				return ErrInvalidLengthPush
			}
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.Labels = string(dAtA[iNdEx:postIndex])
			iNdEx = postIndex
		case 2:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Entries", wireType)
			}
			var msglen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowPush
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				msglen |= int(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if msglen < 0 {
				return ErrInvalidLengthPush
			}
			postIndex := iNdEx + msglen
			if postIndex < 0 {
				return ErrInvalidLengthPush
			}
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.Entries = append(m.Entries, Entry{})
			if err := m.Entries[len(m.Entries)-1].Unmarshal(dAtA[iNdEx:postIndex]); err != nil {
				return err
			}
			iNdEx = postIndex
		case 3:
			if wireType != 0 {
				return fmt.Errorf("proto: wrong wireType = %d for field Hash", wireType)
			}
			m.Hash = 0
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowPush
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				m.Hash |= uint64(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
		case 5:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field SharedStructuredMetadataSets", wireType)
			}
			var msglen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowPush
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				msglen |= int(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if msglen < 0 {
				return ErrInvalidLengthPush
			}
			postIndex := iNdEx + msglen
			if postIndex < 0 {
				return ErrInvalidLengthPush
			}
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.SharedStructuredMetadataSets = append(m.SharedStructuredMetadataSets, SharedStructuredMetadataSet{})
			if err := m.SharedStructuredMetadataSets[len(m.SharedStructuredMetadataSets)-1].Unmarshal(dAtA[iNdEx:postIndex]); err != nil {
				return err
			}
			iNdEx = postIndex
		// Field 4 is reserved: it carried the retired flat shared structured metadata list.
		// It falls through to the default branch, which skips it, so a payload written by a
		// scratch build that still emitted it decodes without error and drops the field.
		default:
			iNdEx = preIndex
			skippy, err := skipPush(dAtA[iNdEx:])
			if err != nil {
				return err
			}
			if skippy < 0 {
				return ErrInvalidLengthPush
			}
			if (iNdEx + skippy) < 0 {
				return ErrInvalidLengthPush
			}
			if (iNdEx + skippy) > l {
				return io.ErrUnexpectedEOF
			}
			iNdEx += skippy
		}
	}

	if iNdEx > l {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func (m *Entry) Unmarshal(dAtA []byte) error {
	l := len(dAtA)
	iNdEx := 0
	for iNdEx < l {
		preIndex := iNdEx
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return ErrIntOverflowPush
			}
			if iNdEx >= l {
				return io.ErrUnexpectedEOF
			}
			b := dAtA[iNdEx]
			iNdEx++
			wire |= uint64(b&0x7F) << shift
			if b < 0x80 {
				break
			}
		}
		fieldNum := int32(wire >> 3)
		wireType := int(wire & 0x7)
		if wireType == 4 {
			return fmt.Errorf("proto: EntryAdapter: wiretype end group for non-group")
		}
		if fieldNum <= 0 {
			return fmt.Errorf("proto: EntryAdapter: illegal tag %d (wire type %d)", fieldNum, wire)
		}
		switch fieldNum {
		case 1:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Timestamp", wireType)
			}
			var msglen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowPush
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				msglen |= int(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if msglen < 0 {
				return ErrInvalidLengthPush
			}
			postIndex := iNdEx + msglen
			if postIndex < 0 {
				return ErrInvalidLengthPush
			}
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			if err := StdTimeUnmarshal(&m.Timestamp, dAtA[iNdEx:postIndex]); err != nil {
				return err
			}
			iNdEx = postIndex
		case 2:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Line", wireType)
			}
			var stringLen uint64
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowPush
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				stringLen |= uint64(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			intStringLen := int(stringLen)
			if intStringLen < 0 {
				return ErrInvalidLengthPush
			}
			postIndex := iNdEx + intStringLen
			if postIndex < 0 {
				return ErrInvalidLengthPush
			}
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.Line = string(dAtA[iNdEx:postIndex])
			iNdEx = postIndex
		case 3:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field StructuredMetadata", wireType)
			}
			var msglen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowPush
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				msglen |= int(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if msglen < 0 {
				return ErrInvalidLengthPush
			}
			postIndex := iNdEx + msglen
			if postIndex < 0 {
				return ErrInvalidLengthPush
			}
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.StructuredMetadata = append(m.StructuredMetadata, LabelAdapter{})
			if err := m.StructuredMetadata[len(m.StructuredMetadata)-1].Unmarshal(dAtA[iNdEx:postIndex]); err != nil {
				return err
			}
			iNdEx = postIndex
		case 4:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Parsed", wireType)
			}
			var msglen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowPush
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				msglen |= int(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if msglen < 0 {
				return ErrInvalidLengthPush
			}
			postIndex := iNdEx + msglen
			if postIndex < 0 {
				return ErrInvalidLengthPush
			}
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.Parsed = append(m.Parsed, LabelAdapter{})
			if err := m.Parsed[len(m.Parsed)-1].Unmarshal(dAtA[iNdEx:postIndex]); err != nil {
				return err
			}
			iNdEx = postIndex
		case 5:
			if wireType != 0 {
				return fmt.Errorf("proto: wrong wireType = %d for field SharedResourceRef", wireType)
			}
			m.SharedResourceRef = 0
			for shift := uint(0); ; shift += 7 {
				if shift >= 32 {
					return ErrIntOverflowPush
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				m.SharedResourceRef |= uint32(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
		case 6:
			if wireType != 0 {
				return fmt.Errorf("proto: wrong wireType = %d for field SharedScopeRef", wireType)
			}
			m.SharedScopeRef = 0
			for shift := uint(0); ; shift += 7 {
				if shift >= 32 {
					return ErrIntOverflowPush
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				m.SharedScopeRef |= uint32(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
		default:
			iNdEx = preIndex
			skippy, err := skipPush(dAtA[iNdEx:])
			if err != nil {
				return err
			}
			if skippy < 0 {
				return ErrInvalidLengthPush
			}
			if (iNdEx + skippy) < 0 {
				return ErrInvalidLengthPush
			}
			if (iNdEx + skippy) > l {
				return io.ErrUnexpectedEOF
			}
			iNdEx += skippy
		}
	}

	if iNdEx > l {
		return io.ErrUnexpectedEOF
	}
	return nil
}

// Unmarshal a LabelAdapter, implements proto.Unmarshaller.
func (m *LabelAdapter) Unmarshal(dAtA []byte) error {
	l := len(dAtA)
	iNdEx := 0
	for iNdEx < l {
		preIndex := iNdEx
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return ErrIntOverflowPush
			}
			if iNdEx >= l {
				return io.ErrUnexpectedEOF
			}
			b := dAtA[iNdEx]
			iNdEx++
			wire |= uint64(b&0x7F) << shift
			if b < 0x80 {
				break
			}
		}
		fieldNum := int32(wire >> 3)
		wireType := int(wire & 0x7)
		if wireType == 4 {
			return fmt.Errorf("proto: LabelPairAdapter: wiretype end group for non-group")
		}
		if fieldNum <= 0 {
			return fmt.Errorf("proto: LabelPairAdapter: illegal tag %d (wire type %d)", fieldNum, wire)
		}
		switch fieldNum {
		case 1:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Name", wireType)
			}
			var stringLen uint64
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowPush
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				stringLen |= uint64(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			intStringLen := int(stringLen)
			if intStringLen < 0 {
				return ErrInvalidLengthPush
			}
			postIndex := iNdEx + intStringLen
			if postIndex < 0 {
				return ErrInvalidLengthPush
			}
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.Name = string(dAtA[iNdEx:postIndex])
			iNdEx = postIndex
		case 2:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Value", wireType)
			}
			var stringLen uint64
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowPush
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				stringLen |= uint64(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			intStringLen := int(stringLen)
			if intStringLen < 0 {
				return ErrInvalidLengthPush
			}
			postIndex := iNdEx + intStringLen
			if postIndex < 0 {
				return ErrInvalidLengthPush
			}
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.Value = string(dAtA[iNdEx:postIndex])
			iNdEx = postIndex
		default:
			iNdEx = preIndex
			skippy, err := skipPush(dAtA[iNdEx:])
			if err != nil {
				return err
			}
			if skippy < 0 {
				return ErrInvalidLengthPush
			}
			if (iNdEx + skippy) < 0 {
				return ErrInvalidLengthPush
			}
			if (iNdEx + skippy) > l {
				return io.ErrUnexpectedEOF
			}
			iNdEx += skippy
		}
	}

	if iNdEx > l {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func yoloString(buf []byte) string {
	return *((*string)(unsafe.Pointer(&buf)))
}

func (m *Stream) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	l = len(m.Labels)
	if l > 0 {
		n += 1 + l + sovPush(uint64(l))
	}
	if len(m.Entries) > 0 {
		for _, e := range m.Entries {
			l = e.Size()
			n += 1 + l + sovPush(uint64(l))
		}
	}
	if m.Hash != 0 {
		n += 1 + sovPush(m.Hash)
	}
	if len(m.SharedStructuredMetadataSets) > 0 {
		for _, e := range m.SharedStructuredMetadataSets {
			l = e.Size()
			n += 1 + l + sovPush(uint64(l))
		}
	}
	return n
}

func (m *Entry) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	l = SizeOfStdTime(m.Timestamp)
	n += 1 + l + sovPush(uint64(l))
	l = len(m.Line)
	if l > 0 {
		n += 1 + l + sovPush(uint64(l))
	}
	if len(m.StructuredMetadata) > 0 {
		for _, e := range m.StructuredMetadata {
			l = e.Size()
			n += 1 + l + sovPush(uint64(l))
		}
	}
	if len(m.Parsed) > 0 {
		for _, e := range m.Parsed {
			l = e.Size()
			n += 1 + l + sovPush(uint64(l))
		}
	}
	if m.SharedResourceRef != 0 {
		n += 1 + sovPush(uint64(m.SharedResourceRef))
	}
	if m.SharedScopeRef != 0 {
		n += 1 + sovPush(uint64(m.SharedScopeRef))
	}
	return n
}

func (m *LabelAdapter) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	l = len(m.Name)
	if l > 0 {
		n += 1 + l + sovPush(uint64(l))
	}
	l = len(m.Value)
	if l > 0 {
		n += 1 + l + sovPush(uint64(l))
	}
	return n
}

func (m *Stream) Equal(that interface{}) bool {
	if that == nil {
		return m == nil
	}

	that1, ok := that.(*Stream)
	if !ok {
		that2, ok := that.(Stream)
		if ok {
			that1 = &that2
		} else {
			return false
		}
	}
	if that1 == nil {
		return m == nil
	} else if m == nil {
		return false
	}
	if m.Labels != that1.Labels {
		return false
	}
	if len(m.Entries) != len(that1.Entries) {
		return false
	}
	for i := range m.Entries {
		if !m.Entries[i].Equal(that1.Entries[i]) {
			return false
		}
	}
	if m.Hash != that1.Hash {
		return false
	}
	if len(m.SharedStructuredMetadataSets) != len(that1.SharedStructuredMetadataSets) {
		return false
	}
	for i := range m.SharedStructuredMetadataSets {
		if !m.SharedStructuredMetadataSets[i].Equal(&that1.SharedStructuredMetadataSets[i]) {
			return false
		}
	}
	return true
}

func (m *Entry) Equal(that interface{}) bool {
	if that == nil {
		return m == nil
	}

	that1, ok := that.(*Entry)
	if !ok {
		that2, ok := that.(Entry)
		if ok {
			that1 = &that2
		} else {
			return false
		}
	}
	if that1 == nil {
		return m == nil
	} else if m == nil {
		return false
	}
	if !m.Timestamp.Equal(that1.Timestamp) {
		return false
	}
	if m.Line != that1.Line {
		return false
	}
	if len(m.StructuredMetadata) != len(that1.StructuredMetadata) {
		return false
	}
	for i := range m.StructuredMetadata {
		if !m.StructuredMetadata[i].Equal(that1.StructuredMetadata[i]) {
			return false
		}
	}
	if len(m.Parsed) != len(that1.Parsed) {
		return false
	}
	for i := range m.Parsed {
		if !m.Parsed[i].Equal(that1.Parsed[i]) {
			return false
		}
	}
	if m.SharedResourceRef != that1.SharedResourceRef {
		return false
	}
	if m.SharedScopeRef != that1.SharedScopeRef {
		return false
	}
	return true
}

// Equal implements proto.Equaler.
func (m *LabelAdapter) Equal(other LabelAdapter) bool {
	return m.Name == other.Name && m.Value == other.Value
}

// Compare implements proto.Comparer.
func (m *LabelAdapter) Compare(other LabelAdapter) int {
	if c := strings.Compare(m.Name, other.Name); c != 0 {
		return c
	}
	return strings.Compare(m.Value, other.Value)
}
