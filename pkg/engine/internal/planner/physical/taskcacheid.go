package physical

import (
	"fmt"
	"slices"
	"time"

	"github.com/cespare/xxhash/v2"
)

// hashUint64 returns the 16-character hex representation of a uint64 for use as a task cache ID.
func hashUint64(h uint64) string {
	return fmt.Sprintf("%016x", h)
}

// expressionStrings returns sorted string representations of expressions for deterministic hashing.
func expressionStrings(exprs []Expression) []string {
	if len(exprs) == 0 {
		return nil
	}
	out := make([]string, len(exprs))
	for i, e := range exprs {
		if e != nil {
			out[i] = e.String()
		}
	}
	slices.Sort(out)
	return out
}

// hashStrings writes a sorted slice of strings into the digest with a separator.
func hashStrings(d *xxhash.Digest, strs []string) {
	for _, s := range strs {
		_, _ = d.WriteString(s)
		_, _ = d.Write([]byte{0})
	}
}

// hashInt64Slice writes a sorted slice of int64s into the digest.
func hashInt64Slice(d *xxhash.Digest, vals []int64) {
	sorted := slices.Clone(vals)
	slices.Sort(sorted)
	for _, v := range sorted {
		_, _ = d.WriteString(fmt.Sprintf("%d", v))
		_, _ = d.Write([]byte{0})
	}
}

// hashTimeRange writes start and end Unix nano times into the digest.
func hashTimeRange(d *xxhash.Digest, start, end time.Time) {
	_, _ = d.WriteString(fmt.Sprintf("%d", start.UnixNano()))
	_, _ = d.Write([]byte{0})
	_, _ = d.WriteString(fmt.Sprintf("%d", end.UnixNano()))
}

// hashDataObjScan computes a content-based hash of a DataObjScan for task cache identification.
func hashDataObjScan(s *DataObjScan) string {
	d := xxhash.New()
	_, _ = d.WriteString(string(s.Location))
	_, _ = d.Write([]byte{0})
	_, _ = d.WriteString(fmt.Sprintf("%d", s.Section))
	_, _ = d.Write([]byte{0})
	hashInt64Slice(d, s.StreamIDs)
	hashStrings(d, expressionStrings(exprSliceToExpression(s.Projections)))
	hashStrings(d, expressionStrings(s.Predicates))
	hashTimeRange(d, s.MaxTimeRange.Start, s.MaxTimeRange.End)
	return hashUint64(d.Sum64())
}

// exprSliceToExpression converts []ColumnExpression to []Expression for expressionStrings.
func exprSliceToExpression(ce []ColumnExpression) []Expression {
	if len(ce) == 0 {
		return nil
	}
	out := make([]Expression, len(ce))
	for i, e := range ce {
		out[i] = e
	}
	return out
}

// hashPointersScan computes a content-based hash of a PointersScan for task cache identification.
func hashPointersScan(s *PointersScan) string {
	d := xxhash.New()
	_, _ = d.WriteString(string(s.Location))
	_, _ = d.Write([]byte{0})
	selectorStr := ""
	if s.Selector != nil {
		selectorStr = s.Selector.String()
	}
	_, _ = d.WriteString(selectorStr)
	_, _ = d.Write([]byte{0})
	hashStrings(d, expressionStrings(s.Predicates))
	hashTimeRange(d, s.Start, s.End)
	return hashUint64(d.Sum64())
}
