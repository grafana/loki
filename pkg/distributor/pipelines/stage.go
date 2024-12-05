package pipelines

import (
	"context"
	"fmt"
	"unsafe"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/distributor/model"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
)

func (s Stage) Compile() (Transformer, error) {
	switch s.Action {
	case "parse_logfmt":
		return &Parser{
			Stage:   s,
			builder: log.NewBaseLabelsBuilder(),
			decoder: log.NewLogfmtParser(true, false),
		}, nil
	case "parse_json":
		return &Parser{
			Stage:   s,
			builder: log.NewBaseLabelsBuilder(),
			decoder: log.NewJSONParser(),
		}, nil
	case "parse_pattern":
		pattern, ok := s.Config["pattern"]
		if !ok {
			return nil, fmt.Errorf("missing configuration option 'pattern'")
		}
		dec, err := log.NewPatternParser(pattern)
		if err != nil {
			return nil, err
		}
		return &Parser{
			Stage:   s,
			builder: log.NewBaseLabelsBuilder(),
			decoder: dec,
		}, nil
	case "promote_metadata_to_label":
		return &PromoteMetadataToLabel{Stage: s}, nil
	case "promote_field_to_label":
		return &PromoteFieldToLabel{Stage: s}, nil
	case "promote_field_to_metadata":
		return &PromoteFieldToLabel{Stage: s}, nil
	case "degrade_label_to_metadata":
		return &DegradeLabelToMetadata{Stage: s}, nil
	case "drop_label":
		return &DropLabel{Stage: s}, nil
	case "drop_metadata":
		return &DropMetadata{Stage: s}, nil
	default:
		return nil, fmt.Errorf("invalid action: %v", s.Action)
	}
}

type DropLabel struct {
	Stage
}

// Apply removes specified labels from incoming streams according to the stage configuration.
func (s *DropLabel) Apply(ctx context.Context, streams *model.StreamsBuilder) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	for _, stream := range streams.Build() {
		labels := stream.ParsedLabels
		for l, label := range labels {
			for key := range s.Config {
				if key == label.Name {
					labels = append(labels[:l], labels[l+1:]...)
				}
			}
		}
		streams.AddStreamWithLabels(labels, stream)
	}

	return nil
}

type DropMetadata struct {
	Stage
}

// Apply removes specified metadata fields from incoming stream entries according to the stage configuration.
// It removes any metadata whose name matches the keys in the stage configuration.
func (s DropMetadata) Apply(ctx context.Context, streams *model.StreamsBuilder) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	for _, stream := range streams.Build() {
		for e, entry := range stream.Entries {
			for m, metadata := range entry.StructuredMetadata {
				for key := range s.Config {
					if key == metadata.Name {
						stream.Entries[e].StructuredMetadata = append(stream.Entries[e].StructuredMetadata[:m], stream.Entries[e].StructuredMetadata[m+1:]...)
					}
				}
			}
		}
		streams.AddStream(stream)
	}

	return nil
}

type Parser struct {
	Stage
	builder *log.BaseLabelsBuilder
	decoder log.Stage
}

func (s *Parser) Apply(ctx context.Context, streams *model.StreamsBuilder) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	for _, stream := range streams.Build() {
		for e, entry := range stream.Entries {
			lbs := s.builder.ForLabels(labels.EmptyLabels(), 0)
			s.decoder.Process(entry.Timestamp.UnixNano(), unsafe.Slice(unsafe.StringData(entry.Line), len(entry.Line)), lbs)
			stream.Entries[e].Parsed = append(stream.Entries[e].Parsed, logproto.FromLabelsToLabelAdapters(lbs.LabelsResult().Parsed())...)
		}
		streams.AddStream(stream)
	}

	return nil
}

type PromoteFieldToLabel struct {
	Stage
}

func (s *PromoteFieldToLabel) Apply(ctx context.Context, streams *model.StreamsBuilder) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

type PromoteFieldToMetadata struct {
	Stage
}

func (s *PromoteFieldToMetadata) Apply(ctx context.Context, streams *model.StreamsBuilder) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

type PromoteMetadataToLabel struct {
	Stage
}

func (s *PromoteMetadataToLabel) Apply(ctx context.Context, streams *model.StreamsBuilder) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	for _, stream := range streams.Build() {
		for e := range stream.Entries {
			found := make([]logproto.LabelAdapter, 0, len(s.Config))
			metadata := stream.Entries[e].StructuredMetadata
			for src, dst := range s.Config {
				if dst == "" {
					dst = src
				}
				for idx := 0; idx < len(metadata); idx++ {
					if src == metadata[idx].Name {
						found = append(found, logproto.LabelAdapter{Name: dst, Value: metadata[idx].Value})
						metadata = append(metadata[:idx], metadata[idx+1:]...)
						idx--
					}
				}
			}
			stream.Entries[e].StructuredMetadata = metadata

			streams.AddEntry(append(stream.ParsedLabels, found...), stream.Entries[e])
		}
	}

	return nil
}

type DegradeLabelToMetadata struct {
	Stage
}

func (s *DegradeLabelToMetadata) Apply(ctx context.Context, streams *model.StreamsBuilder) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	for _, stream := range streams.Build() {
		degraded := make([]logproto.LabelAdapter, 0, len(s.Config))
		labels := stream.ParsedLabels

		// remove from labels
		for src, dst := range s.Config {
			if dst == "" {
				dst = src
			}
		outer:
			for l, label := range labels {
				if src == label.Name {
					degraded = append(degraded, logproto.LabelAdapter{Name: dst, Value: label.Value})
					labels = append(labels[:l], labels[l+1:]...)
					break outer
				}
			}
		}

		// add to structured metadata
		for j := range stream.Entries {
			stream.Entries[j].StructuredMetadata = append(stream.Entries[j].StructuredMetadata, degraded...)
		}

		streams.AddStreamWithLabels(labels, stream)
	}

	return nil
}
