package sarama

import (
	"fmt"

	"github.com/rcrowley/go-metrics"
)

// Encoder is the interface that wraps the basic Encode method.
// Anything implementing Encoder can be turned into bytes using Kafka's encoding rules.
type encoder interface {
	encode(pe packetEncoder) error
}

type encoderWithHeader interface {
	encoder
	headerVersion() int16
}

// Encode takes an Encoder and turns it into bytes while potentially recording metrics.
func encode(e encoder, metricRegistry metrics.Registry) ([]byte, error) {
	if e == nil {
		return nil, nil
	}

	var prepEnc prepEncoder
	var realEnc realEncoder

	err := e.encode(prepareFlexibleEncoder(&prepEnc, e))
	if err != nil {
		return nil, err
	}

	if prepEnc.length < 0 || prepEnc.length > int(MaxRequestSize) {
		return nil, PacketEncodingError{fmt.Sprintf("invalid request size (%d)", prepEnc.length)}
	}

	realEnc.raw = make([]byte, prepEnc.length)
	realEnc.registry = metricRegistry
	err = e.encode(prepareFlexibleEncoder(&realEnc, e))
	if err != nil {
		return nil, err
	}

	return realEnc.raw, nil
}

// decoder is the interface that wraps the basic Decode method.
// Anything implementing Decoder can be extracted from bytes using Kafka's encoding rules.
type decoder interface {
	decode(pd packetDecoder) error
}

type versionedDecoder interface {
	decode(pd packetDecoder, version int16) error
}

type flexibleVersion interface {
	isFlexibleVersion(version int16) bool
	isFlexible() bool
}

// decode takes bytes and a decoder and fills the fields of the decoder from the bytes,
// interpreted using Kafka's encoding rules.
func decode(buf []byte, in decoder, metricRegistry metrics.Registry) error {
	if buf == nil {
		return nil
	}
	helper := realDecoder{
		raw:      buf,
		registry: metricRegistry,
	}
	err := in.decode(&helper)
	if err != nil {
		return err
	}

	if helper.off != len(buf) {
		return PacketDecodingError{fmt.Sprintf("invalid length: buf=%d decoded=%d %#v", len(buf), helper.off, in)}
	}

	return nil
}

func versionedDecode(buf []byte, in versionedDecoder, version int16, metricRegistry metrics.Registry) error {
	if buf == nil {
		return nil
	}

	helper := prepareFlexibleDecoder(&realDecoder{
		raw:      buf,
		registry: metricRegistry,
	}, in, version)
	err := in.decode(helper, version)
	if err != nil {
		return err
	}

	if remaining := helper.remaining(); remaining != 0 {
		return PacketDecodingError{
			Info: fmt.Sprintf("invalid length len=%d remaining=%d", len(buf), remaining),
		}
	}

	return nil
}

func prepareFlexibleDecoder(pd *realDecoder, in versionedDecoder, version int16) packetDecoder {
	if flexibleDecoder, ok := in.(flexibleVersion); ok && flexibleDecoder.isFlexibleVersion(version) {
		return &realFlexibleDecoder{pd}
	}
	return pd
}

func prepareFlexibleEncoder(pe packetEncoder, req encoder) packetEncoder {
	if flexibleEncoder, ok := req.(flexibleVersion); ok && flexibleEncoder.isFlexible() {
		switch e := pe.(type) {
		case *prepEncoder:
			return &prepFlexibleEncoder{e}
		case *realEncoder:
			return &realFlexibleEncoder{e}
		default:
			return pe
		}
	}
	return pe
}

func downgradeFlexibleDecoder(pd packetDecoder) packetDecoder {
	if f, ok := pd.(*realFlexibleDecoder); ok {
		return f.realDecoder
	}
	return pd
}
