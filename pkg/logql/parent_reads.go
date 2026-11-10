package logql

import (
	"slices"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// ParentReads is what an operator reads from the series of its operand. It has three independent
// axes: the label names it reads, whether distinct series must stay apart, and whether it reads
// sample values or only whether a series is present. The zero value is the conservative one: the
// operator reads every label and value, and needs every series kept apart.
type ParentReads struct {
	labels     LabelReads
	noIdentity bool
	noValues   bool
}

// readsEverything returns the ParentReads of an operator that reads every label and value of its
// operand and needs every series kept apart.
func readsEverything() ParentReads {
	return ParentReads{}
}

// newParentReads returns the ParentReads of an operator that reads labels from its operand, needs
// its series kept apart when identity is true, and reads its sample values when values is true.
func newParentReads(labels LabelReads, identity, values bool) ParentReads {
	return ParentReads{labels: labels, noIdentity: !identity, noValues: !values}
}

// Labels returns the label names the operator reads.
func (r ParentReads) Labels() LabelReads {
	return r.labels
}

// Identity is true when distinct series of the operand must stay distinguishable. They can be told
// apart by hash, so this does not mean that any label has to be kept.
func (r ParentReads) Identity() bool {
	return !r.noIdentity
}

// Values is true when the operator reads sample values, and false when it reads only whether a
// series has a sample.
func (r ParentReads) Values() bool {
	return !r.noValues
}

// withLabels returns r reading labels instead of its own label names.
func (r ParentReads) withLabels(labels LabelReads) ParentReads {
	r.labels = labels
	return r
}

// LabelReads is the set of label names that an operator reads from the series of its
// operand. The zero value is every label.
type LabelReads struct {
	// only is true when names are the only labels read, and false when names are the only labels
	// not read.
	only bool
	// names are sorted and unique, and nil when empty, so that equal LabelReads compare equal.
	names []string
}

// allLabels returns the LabelReads of an operator that reads every label.
func allLabels() LabelReads {
	return LabelReads{}
}

// noLabels returns the LabelReads of an operator that reads no label.
func noLabels() LabelReads {
	return LabelReads{only: true}
}

// onlyLabels returns the LabelReads of an operator that reads names and no other label.
func onlyLabels(names ...string) LabelReads {
	return LabelReads{only: true, names: sortedUniqueNames(names)}
}

// labelsWithout returns the LabelReads of an operator that reads every label but names.
func labelsWithout(names ...string) LabelReads {
	return LabelReads{names: sortedUniqueNames(names)}
}

// groupingLabels returns the labels that an aggregation with grouping g reads: those of a by
// clause, or all but those of a without clause.
func groupingLabels(g *syntax.Grouping) LabelReads {
	if g == nil {
		return allLabels()
	}
	if g.Without {
		return labelsWithout(g.Groups...)
	}
	return onlyLabels(g.Groups...)
}

// sortedUniqueNames returns a sorted copy of names without duplicates, or nil when names is empty.
// It copies because names may be the groups of a Grouping, which must not be changed in place.
func sortedUniqueNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	sorted := slices.Clone(names)
	slices.Sort(sorted)
	return slices.Compact(sorted)
}

// All is true when the operator reads every label.
func (l LabelReads) All() bool {
	return !l.only && len(l.names) == 0
}

// None is true when the operator reads no label.
func (l LabelReads) None() bool {
	return l.only && len(l.names) == 0
}

// Reads is true when the operator reads the label name.
func (l LabelReads) Reads(name string) bool {
	_, found := slices.BinarySearch(l.names, name)
	return found == l.only
}

// union returns the labels read by an operator that reads both l and o.
func (l LabelReads) union(o LabelReads) LabelReads {
	if l.only && o.only {
		return onlyLabels(append(slices.Clone(l.names), o.names...)...)
	}
	// At least one side reads every label but some. The union leaves out only the names that
	// neither side reads.
	without, other := l, o
	if without.only {
		without, other = o, l
	}
	return labelsWithout(slices.DeleteFunc(slices.Clone(without.names), other.Reads)...)
}
