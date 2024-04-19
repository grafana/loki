package iter

import (
	"fmt"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

type categorizeLabelsIterator struct {
	EntryIterator
	currEntry        logproto.Entry
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

	c.currEntry = c.Entry()
	if len(c.currEntry.StructuredMetadata) == 0 && len(c.currEntry.Parsed) == 0 {
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
	for _, label := range c.currEntry.StructuredMetadata {
		builder.Del(label.Name)
	}
	for _, label := range c.currEntry.Parsed {
		builder.Del(label.Name)
	}

	newLabels := builder.Labels()
	c.currStreamLabels = newLabels.String()
	c.currHash = newLabels.Hash()

	return true
}

func (c *categorizeLabelsIterator) Error() error {
	return c.currErr
}

func (c *categorizeLabelsIterator) Labels() string {
	return c.currStreamLabels
}

func (c *categorizeLabelsIterator) StreamHash() uint64 {
	return c.currHash
}
