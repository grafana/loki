package indexobj

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/util"
)

// ErrMixedLayouts reports an index whose sections or rows of one tenant do not
// share one stream layout. Writers never produce such an index.
var ErrMixedLayouts = errors.New("index sections do not share one stream layout")

// ErrInvalidLayout reports an index whose stream layout for one tenant cannot
// be read: a stats section records an invalid layout, or a legacy index has no
// stats rows to take the sort schema from. No retry can fix it.
var ErrInvalidLayout = errors.New("index has no valid stream layout")

// TenantLayout returns the stream layout of the data objects that obj indexes
// for tenant.
//
// Every stats section of tenant must record the same layout. If none records
// a layout, as in indexes written before stats sections recorded one,
// TenantLayout derives it from the sort schema of the stats rows, with the
// default shard layout.
//
// ok is false if obj has no stats section for tenant, so the index describes
// no data object of tenant. TenantLayout returns an error that wraps
// [ErrMixedLayouts] if the sections disagree, and one that wraps
// [ErrInvalidLayout] if the layout cannot be read.
func TenantLayout(ctx context.Context, obj *dataobj.Object, tenant string) (layout streams.Layout, ok bool, err error) {
	layout, recorded, unrecorded, err := recordedTenantLayout(ctx, obj, tenant)
	if err != nil {
		return streams.Layout{}, false, err
	}
	switch {
	case recorded > 0 && unrecorded > 0:
		return streams.Layout{}, false, fmt.Errorf("%w: %d stats sections record a layout and %d do not", ErrMixedLayouts, recorded, unrecorded)
	case recorded > 0:
		return layout, true, nil
	case unrecorded > 0:
		layout, err := legacyTenantLayout(ctx, obj, tenant)
		if err != nil {
			return streams.Layout{}, false, err
		}
		return layout, true, nil
	default:
		return streams.Layout{}, false, nil
	}
}

// recordedTenantLayout returns the layout that the stats sections of tenant
// record, with the count of sections that record one and that do not. It
// returns an error that wraps [ErrMixedLayouts] if two sections record
// different layouts.
func recordedTenantLayout(ctx context.Context, obj *dataobj.Object, tenant string) (layout streams.Layout, recorded, unrecorded int, err error) {
	for _, section := range obj.Sections().Filter(stats.CheckSection) {
		if section.Tenant != tenant {
			continue
		}
		opened, err := stats.Open(ctx, section)
		if err != nil {
			return streams.Layout{}, 0, 0, fmt.Errorf("opening stats section: %w", err)
		}
		got, recordsLayout, err := opened.Layout()
		if err != nil {
			return streams.Layout{}, 0, 0, fmt.Errorf("%w: reading stats layout: %w", ErrInvalidLayout, err)
		}
		if !recordsLayout {
			unrecorded++
			continue
		}
		if recorded > 0 && !got.Equal(layout) {
			return streams.Layout{}, 0, 0, fmt.Errorf("%w: stats sections record %v and %v", ErrMixedLayouts, layout, got)
		}
		layout = got
		recorded++
	}
	return layout, recorded, unrecorded, nil
}

// legacyTenantLayout derives the layout of an index whose stats sections
// record none. Writers of such indexes recorded the sort schema on every stats
// row. They always used the default shard layout, because no other shard
// factor was valid.
func legacyTenantLayout(ctx context.Context, obj *dataobj.Object, tenant string) (streams.Layout, error) {
	sortSchema, err := legacySortSchema(ctx, obj, tenant)
	if err != nil {
		return streams.Layout{}, err
	}
	var schemaLabels []string
	if sortSchema != "" {
		schemaLabels = strings.Split(sortSchema, ",")
	}
	return streams.NewLayout(streams.LegacyShardLayout(), schemaLabels), nil
}

func legacySortSchema(ctx context.Context, obj *dataobj.Object, tenant string) (string, error) {
	var (
		sortSchema string
		found      bool
	)
	for _, section := range obj.Sections().Filter(stats.CheckSection) {
		if section.Tenant != tenant {
			continue
		}
		opened, err := stats.Open(ctx, section)
		if err != nil {
			return "", fmt.Errorf("opening stats section: %w", err)
		}
		err = func() (err error) {
			reader := stats.NewRowReader(ctx, opened)
			defer util.CloseAndHandleError(reader, &err)
			for reader.Next() {
				row := reader.At()
				if found && row.SortSchema != sortSchema {
					return fmt.Errorf("%w: stats rows have sort schemas %q and %q", ErrMixedLayouts, sortSchema, row.SortSchema)
				}
				sortSchema, found = row.SortSchema, true
			}
			return reader.Err()
		}()
		if err != nil {
			return "", err
		}
	}
	if !found {
		return "", fmt.Errorf("%w: legacy index has no stats rows for tenant %s", ErrInvalidLayout, tenant)
	}
	return sortSchema, nil
}
