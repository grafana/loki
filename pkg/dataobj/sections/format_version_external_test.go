package sections_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/format"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/watermarks"
)

// TestOpen_SectionVersion checks the section versions that the columnar
// sections accept. The logs section accepts version 3 as well, and its tests
// cover it.
func TestOpen_SectionVersion(t *testing.T) {
	sectionKinds := []struct {
		kind string
		open func(context.Context, *dataobj.Section) error
	}{
		{format.IndexPointersSectionKind, func(ctx context.Context, sec *dataobj.Section) error {
			_, err := indexpointers.Open(ctx, sec)
			return err
		}},
		{format.PostingsSectionKind, func(ctx context.Context, sec *dataobj.Section) error {
			_, err := postings.Open(ctx, sec)
			return err
		}},
		{format.StatsSectionKind, func(ctx context.Context, sec *dataobj.Section) error {
			_, err := stats.Open(ctx, sec)
			return err
		}},
		{format.StreamsSectionKind, func(ctx context.Context, sec *dataobj.Section) error {
			_, err := streams.Open(ctx, sec)
			return err
		}},
		{format.WatermarksSectionKind, func(ctx context.Context, sec *dataobj.Section) error {
			_, err := watermarks.Open(ctx, sec)
			return err
		}},
	}

	for _, tc := range sectionKinds {
		section := func(version uint32) *dataobj.Section {
			return &dataobj.Section{
				Type:   dataobj.SectionType{Namespace: format.SectionNamespace, Kind: tc.kind, Version: version},
				Reader: noExtensionDataReader{},
			}
		}

		for _, version := range []uint32{2, 4} {
			t.Run(fmt.Sprintf("passes the version check for a %s section of version %d", tc.kind, version), func(t *testing.T) {
				err := tc.open(t.Context(), section(version))

				require.ErrorContains(t, err, "section is missing required extension_data")
			})
		}

		for _, version := range []uint32{1, 3, 5} {
			t.Run(fmt.Sprintf("returns an error for a %s section of version %d", tc.kind, version), func(t *testing.T) {
				err := tc.open(t.Context(), section(version))

				require.EqualError(t, err, fmt.Sprintf("unsupported section version: got=%d", version))
			})
		}
	}
}

// noExtensionDataReader is a section reader with no extension data. Open fails
// on it right after the version check, with an error about the extension data.
// Any other method panics.
type noExtensionDataReader struct{ dataobj.SectionReader }

func (noExtensionDataReader) ExtensionData() []byte { return nil }
