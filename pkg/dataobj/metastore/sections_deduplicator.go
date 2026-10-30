package metastore

import (
	"context"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	dslog "github.com/grafana/dskit/log"

	utillog "github.com/grafana/loki/v4/pkg/util/log"
)

const (
	// duplicatesLogsPerSecond and duplicatesLogsBurst set the rate limit of the duplicate
	// sections logger.
	duplicatesLogsPerSecond = 1
	duplicatesLogsBurst     = 5
)

// sectionsDeduplicator keeps the first descriptor of each section, unchanged, and drops the later
// copies.
//
// Two index objects can describe the same section, for example when the builder indexes one log
// object twice after a failed flush. Both index the same log object, so they must agree on the
// section. The deduplicator returns an error when a later copy disagrees with the first one.
//
// A sectionsDeduplicator is not safe for concurrent use.
type sectionsDeduplicator struct {
	logger  log.Logger
	metrics *ObjectMetastoreMetrics

	// duplicatesLogger limits the logs of identical copies. One query can find thousands of them.
	// The limit covers one deduplicator, so one resolution.
	duplicatesLogger log.Logger

	seen map[SectionKey]DataobjSectionDescriptorWithIndex
}

func newSectionsDeduplicator(logger log.Logger, metrics *ObjectMetastoreMetrics) *sectionsDeduplicator {
	return &sectionsDeduplicator{
		logger:           logger,
		duplicatesLogger: dslog.NewRateLimitedLogger(logger, duplicatesLogsPerSecond, duplicatesLogsBurst, nil),
		metrics:          metrics,
		seen:             make(map[SectionKey]DataobjSectionDescriptorWithIndex),
	}
}

// deduplicate returns the sections that the deduplicator has not seen yet, in input order, and
// records them as seen. It returns a new slice that holds the same descriptors.
//
// Each section carries the path of the index object that listed it. deduplicate uses the path in
// log lines.
//
// A diverged copy makes deduplicate return an error and no sections. deduplicate has already
// recorded the sections before that copy, so the caller must not call it again after an error.
func (d *sectionsDeduplicator) deduplicate(ctx context.Context, sections []DataobjSectionDescriptorWithIndex) ([]*DataobjSectionDescriptor, error) {
	kept := make([]*DataobjSectionDescriptor, 0, len(sections))

	for _, section := range sections {
		first, ok := d.seen[section.SectionKey]
		if !ok {
			d.seen[section.SectionKey] = section
			kept = append(kept, section.DataobjSectionDescriptor)
			continue
		}

		if !first.sameStreamAndStatistics(section.DataobjSectionDescriptor) {
			d.metrics.duplicateSectionsTotal.WithLabelValues(divergedTrue).Inc()

			// This log carries the details the error lacks. It does not use duplicatesLogger,
			// because each failed query logs once and the limit could drop it.
			level.Error(utillog.WithContext(ctx, d.logger)).Log(
				"msg", errDifferentSectionStatistics.Error(),
				"object", first.ObjectPath,
				"section", first.SectionIdx,
				"first_index", first.IndexPath,
				"second_index", section.IndexPath,
			)
			return nil, errDifferentSectionStatistics
		}

		level.Info(utillog.WithContext(ctx, d.duplicatesLogger)).Log(
			"msg", "the same section was found in multiple index objects, deduplicating it",
			"object", first.ObjectPath,
			"section", first.SectionIdx,
			"kept_index", first.IndexPath,
			"dropped_index", section.IndexPath,
		)
		d.metrics.duplicateSectionsTotal.WithLabelValues(divergedFalse).Inc()
	}

	return kept, nil
}
