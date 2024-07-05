package iter

import (
	"fmt"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

type categorizeLabelsIterator struct {
	EntryIterator

	currStreamLabels string
	currHash         uint64
	currErr          error
}

func NewCategorizeLabelsIterator(wrap EntryIterator) EntryIterator {
	return &categorizeLabelsIterator{
		EntryIterator: wrap,
	}
}

func (c *categorizeLabelsIterator) Next() bool {
	if !c.EntryIterator.Next() {
		return false
	}

	currEntry := c.At()
	if len(currEntry.StructuredMetadata) == 0 && len(currEntry.Parsed) == 0 {
		c.currStreamLabels = c.EntryIterator.Labels()
		c.currHash = c.EntryIterator.StreamHash()
		return true
	}

	// We need to remove the structured metadata labels and parsed labels from the stream labels.
	streamLabels := c.EntryIterator.Labels()
	lbls, err := syntax.ParseLabels(streamLabels)
	if err != nil {
		c.currErr = fmt.Errorf("failed to parse series labels to categorize labels: %w", err)
		return false
	}

	builder := labels.NewBuilder(lbls)
	for _, label := range currEntry.StructuredMetadata {
		builder.Del(label.Name)
	}
	for _, label := range currEntry.Parsed {
		builder.Del(label.Name)
	}

	newLabels := builder.Labels()
	c.currStreamLabels = newLabels.String()
	c.currHash = newLabels.Hash()

	return true
}

func (c *categorizeLabelsIterator) Err() error {
	return c.currErr
}

func (c *categorizeLabelsIterator) Labels() string {
	return c.currStreamLabels
}

func (c *categorizeLabelsIterator) StreamHash() uint64 {
	return c.currHash
}
