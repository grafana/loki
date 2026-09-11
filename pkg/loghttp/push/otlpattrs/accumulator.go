// Package otlpattrs observes how much of an OTLP push request's ingested
// volume comes from resource and scope attributes being expanded onto every
// log record, and reports it as sampled structured logs.
//
// Only attribute names and byte counts are ever recorded. Attribute values are
// never retained or logged, since they routinely contain user data.
package otlpattrs

import (
	"cmp"
	"slices"
	"strings"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/util"
)

// Kind identifies which part of the OTLP payload an attribute came from.
type Kind string

const (
	// KindResource is an attribute defined on a ResourceLogs block.
	KindResource Kind = "resource"
	// KindScope is an attribute defined on a ScopeLogs block.
	KindScope Kind = "scope"
)

type attrStat struct {
	// records is the number of log records this attribute was copied onto.
	records int64
	// expandedBytes is the total bytes the attribute contributed across all
	// of those records, including the repeated attribute name.
	expandedBytes int64
}

// Accumulator collects per-attribute expansion counters for a single push
// request. It is not safe for concurrent use; a request is parsed on one
// goroutine, so it deliberately avoids any synchronisation.
type Accumulator struct {
	records       int64
	resourceAttrs map[string]attrStat
	scopeAttrs    map[string]attrStat
}

// NewAccumulator returns an Accumulator ready to observe a single push request.
func NewAccumulator() *Accumulator {
	return &Accumulator{
		resourceAttrs: make(map[string]attrStat),
		scopeAttrs:    make(map[string]attrStat),
	}
}

// IncRecords adds to the request's total log record count.
// Callers must add each record exactly once, so it is separate from Observe,
// which is called once per resource and once per scope over the same records.
func (a *Accumulator) IncRecords(records int) {
	a.records += int64(records)
}

// IsEmpty reports whether the request expanded no attributes at all.
func (a *Accumulator) IsEmpty() bool {
	return len(a.resourceAttrs) == 0 && len(a.scopeAttrs) == 0
}

// Observe records that every attribute in attrs was copied onto records log
// records.
func (a *Accumulator) Observe(kind Kind, attrs push.LabelsAdapter, records int) {
	if records <= 0 {
		return
	}

	for _, attr := range attrs {
		if slices.Contains(util.ExcludedStructuredMetadataLabels, attr.Name) {
			continue
		}

		// Note that even though expandedBytes counts the attribute name and value
		// once per record, these slices are not copied per record, so the actual
		// memory cost in distributor is lower in practice.
		//
		// Where this cost does show up is in the kafka producer and on the ingester
		// when we unmarshal the denormalised payload into memory.
		expandedBytes := int64(records) * int64(len(attr.Name)+len(attr.Value))

		switch kind {
		case KindResource:
			stat := a.resourceAttrs[attr.Name]
			stat.records += int64(records)
			stat.expandedBytes += expandedBytes
			a.resourceAttrs[attr.Name] = stat
		case KindScope:
			stat := a.scopeAttrs[attr.Name]
			stat.records += int64(records)
			stat.expandedBytes += expandedBytes
			a.scopeAttrs[attr.Name] = stat
		default:
			// audit only supports resource and scope attributes.
			// silently ignore unknown kind.
		}
	}
}

// Attribute is a single attribute's contribution to a push request.
type Attribute struct {
	Kind Kind
	Name string
	// Records is the number of log records the attribute was copied onto.
	Records int64
	// ExpandedBytes is the total bytes the attribute contributed to the request.
	ExpandedBytes int64
}

// Report is a ranked, truncated view of an Accumulator, safe to log.
type Report struct {
	Records int64

	// Attributes is the total number of distinct attributes observed, whether
	// or not they made the cut.
	Attributes int
	// AttributeExpandedBytes is how much of ExpandedBytes is attributable to
	// resource and scope attributes being copied onto every record.
	AttributeExpandedBytes int64

	// Top holds the attributes that individually contributed the most bytes,
	// ordered by descending contribution.
	Top []Attribute

	// Overflow summarises the attributes that did not make Top.
	OverflowAttributes    int
	OverflowExpandedBytes int64

	// OverflowNames lists the names of the overflow attributes.
	OverflowNames []string
}

// Report ranks the observed attributes by the bytes they contributed and keeps
// the top limit of them, folding the rest into the overflow totals. A limit of
// zero or less keeps everything.
func (a *Accumulator) Report(limit int) Report {
	report := Report{
		Records:    a.records,
		Attributes: len(a.resourceAttrs) + len(a.scopeAttrs),
	}

	ranked := make([]Attribute, 0, len(a.resourceAttrs)+len(a.scopeAttrs))
	for name, stat := range a.resourceAttrs {
		report.AttributeExpandedBytes += stat.expandedBytes
		ranked = append(ranked, Attribute{
			Kind:          KindResource,
			Name:          name,
			Records:       stat.records,
			ExpandedBytes: stat.expandedBytes,
		})
	}

	for name, stat := range a.scopeAttrs {
		report.AttributeExpandedBytes += stat.expandedBytes
		ranked = append(ranked, Attribute{
			Kind:          KindScope,
			Name:          name,
			Records:       stat.records,
			ExpandedBytes: stat.expandedBytes,
		})
	}

	slices.SortFunc(ranked, func(x, y Attribute) int {
		if c := cmp.Compare(y.ExpandedBytes, x.ExpandedBytes); c != 0 {
			return c
		}
		if x.Kind != y.Kind {
			return strings.Compare(string(x.Kind), string(y.Kind))
		}
		return strings.Compare(x.Name, y.Name)
	})

	if limit <= 0 || limit >= len(ranked) {
		report.Top = ranked
		return report
	}

	report.Top = ranked[:limit]

	overflow := ranked[limit:]
	report.OverflowAttributes = len(overflow)
	report.OverflowNames = make([]string, 0, len(overflow))
	for _, attr := range overflow {
		report.OverflowExpandedBytes += attr.ExpandedBytes
		report.OverflowNames = append(report.OverflowNames, attr.Name)
	}

	return report
}
