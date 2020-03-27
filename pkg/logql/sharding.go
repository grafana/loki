package logql

import (
	"fmt"

	"github.com/cortexproject/cortex/pkg/querier/astmapper"
)

// DownstreamSampleExpr is a SampleExpr which signals downstream computation
type DownstreamSampleExpr struct {
	shard *astmapper.ShardAnnotation
	SampleExpr
}

func (d DownstreamSampleExpr) String() string {
	return fmt.Sprintf("downstream<%s, shard=%s>", d.SampleExpr.String(), d.shard)
}

// DownstreamLogSelectorExpr is a LogSelectorExpr which signals downstream computation
type DownstreamLogSelectorExpr struct {
	shard *astmapper.ShardAnnotation
	LogSelectorExpr
}

func (d DownstreamLogSelectorExpr) String() string {
	return fmt.Sprintf("downstream<%s, shard=%s>", d.LogSelectorExpr.String(), d.shard)
}

// ConcatSampleExpr is an expr for concatenating multiple SampleExpr
// Contract: The embedded SampleExprs within a linked list of ConcatSampleExprs must be of the
// same structure. This makes special implementations of SampleExpr.Associative() unnecessary.
type ConcatSampleExpr struct {
	SampleExpr
	next *ConcatSampleExpr
}

func (c ConcatSampleExpr) String() string {
	if c.next == nil {
		return c.SampleExpr.String()
	}

	return fmt.Sprintf("%s ++ %s", c.SampleExpr.String(), c.next.String())
}

// ConcatLogSelectorExpr is an expr for concatenating multiple LogSelectorExpr
type ConcatLogSelectorExpr struct {
	LogSelectorExpr
	next *ConcatLogSelectorExpr
}

func (c ConcatLogSelectorExpr) String() string {
	if c.next == nil {
		return c.LogSelectorExpr.String()
	}

	return fmt.Sprintf("%s ++ %s", c.LogSelectorExpr.String(), c.next.String())
}
