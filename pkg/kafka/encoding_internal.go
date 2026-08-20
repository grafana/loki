package kafka

import (
	"errors"
	"fmt"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// EncodeInternal converts a stream in the internal nested form into one or more Kafka
// records, each within maxSize.
//
// The records carry the nested message, so a consumer must be able to read it. That is what
// the distributor's defer_otlp_attribute_expansion config gates.
func EncodeInternal(partitionID int32, tenantID string, stream logproto.InternalStreamAdapter, maxSize int) ([]*kgo.Record, error) {
	return EncodeInternalWithTopic("", partitionID, tenantID, stream, maxSize)
}

// EncodeInternalWithTopic is EncodeInternal for a caller that names the topic. topic can be
// empty in the case the client injects a default.
func EncodeInternalWithTopic(topic string, partitionID int32, tenantID string, stream logproto.InternalStreamAdapter, maxSize int) ([]*kgo.Record, error) {
	var records []*kgo.Record

	err := splitInternalBySize(stream, maxSize, func(part logproto.InternalStreamAdapter) error {
		data, err := part.Marshal()
		if err != nil {
			return fmt.Errorf("failed to marshal stream: %w", err)
		}
		records = append(records, &kgo.Record{
			Topic:     topic,
			Key:       []byte(tenantID),
			Value:     data,
			Partition: partitionID,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(records) == 0 {
		return nil, errors.New("no valid records created")
	}
	return records, nil
}

// splitInternalBySize divides a stream until every part serialises within maxSize, calling
// emit for each part.
//
// It halves by entry count and measures the real serialised size, rather than accumulating a
// per-entry estimate the way the flat encoder does. A nested part pays for its group and
// scope headers as well as its entries, and which groups a part carries depends on how its
// entries fell, so an estimate would have to model the framing. Measuring cannot drift from
// what is actually written.
func splitInternalBySize(stream logproto.InternalStreamAdapter, maxSize int, emit func(logproto.InternalStreamAdapter) error) error {
	if stream.Size() <= maxSize {
		return emit(stream)
	}

	count := stream.EntryCount()
	if count <= 1 {
		return fmt.Errorf("single entry size (%d) exceeds maximum allowed size (%d)", stream.Size(), maxSize)
	}

	half := count / 2
	parts := stream.Divide(2, func(idx int, _ *push.Entry) int {
		if idx < half {
			return 0
		}
		return 1
	})
	for i := range parts {
		if err := splitInternalBySize(parts[i], maxSize, emit); err != nil {
			return err
		}
	}
	return nil
}
