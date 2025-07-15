package windowing_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/windowing"
)

func Test_windowPages(t *testing.T) {
	tt := []struct {
		name       string
		pages      []*fakePageDesc
		windowSize int64
		expect     []windowing.Window[*fakePageDesc]
	}{
		{
			name:       "empty pages",
			pages:      nil,
			windowSize: 1_000_000,
			expect:     nil,
		},
		{
			name:       "single page smaller than window",
			pages:      []*fakePageDesc{newFakePage(0, 100)},
			windowSize: 1_000_000,
			expect: []windowing.Window[*fakePageDesc]{
				{{Data: newFakePage(0, 100), Index: 0}},
			},
		},
		{
			name:       "single page larger than window",
			pages:      []*fakePageDesc{newFakePage(0, 5_000_000)},
			windowSize: 5_000_000,
			expect: []windowing.Window[*fakePageDesc]{
				{{Data: newFakePage(0, 5_000_000), Index: 0}},
			},
		},
		{
			name: "basic grouping",
			pages: []*fakePageDesc{
				newFakePage(0, 100),
				newFakePage(100, 100),
				newFakePage(200, 100),

				newFakePage(1500, 100),
				newFakePage(1600, 100),
			},
			windowSize: 1000,
			expect: []windowing.Window[*fakePageDesc]{
				{
					{Data: newFakePage(0, 100), Index: 0},
					{Data: newFakePage(100, 100), Index: 1},
					{Data: newFakePage(200, 100), Index: 2},
				},
				{
					{Data: newFakePage(1500, 100), Index: 3},
					{Data: newFakePage(1600, 100), Index: 4},
				},
			},
		},
		{
			name: "basic grouping (unordered)",
			pages: []*fakePageDesc{
				newFakePage(1500, 100),
				newFakePage(200, 100),
				newFakePage(100, 100),

				newFakePage(1600, 100),
				newFakePage(0, 100),
			},
			windowSize: 1000,
			expect: []windowing.Window[*fakePageDesc]{
				{
					{Data: newFakePage(0, 100), Index: 4},
					{Data: newFakePage(100, 100), Index: 2},
					{Data: newFakePage(200, 100), Index: 1},
				},
				{
					{Data: newFakePage(1500, 100), Index: 0},
					{Data: newFakePage(1600, 100), Index: 3},
				},
			},
		},
		{
			name: "grouping with large page",
			pages: []*fakePageDesc{
				newFakePage(0, 100),
				newFakePage(100, 100),
				newFakePage(200, 1000),
				newFakePage(300, 100),
				newFakePage(400, 100),
			},
			windowSize: 500,
			expect: []windowing.Window[*fakePageDesc]{
				{
					{Data: newFakePage(0, 100), Index: 0},
					{Data: newFakePage(100, 100), Index: 1},
				},
				{
					{Data: newFakePage(200, 1000), Index: 2},
				},
				{
					{Data: newFakePage(300, 100), Index: 3},
					{Data: newFakePage(400, 100), Index: 4},
				},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			getInfo := func(p *fakePageDesc) (uint64, uint64) {
				return p.Info.DataOffset, p.Info.DataSize
			}
			actual := slices.Collect(windowing.Iter(tc.pages, getInfo, tc.windowSize))

			for wi, w := range actual {
				for pi, p := range w {
					t.Logf("window %d page %d: %#v\n", wi, pi, p.Data)
				}
			}

			require.Equal(t, tc.expect, actual)
		})
	}
}

type fakePageDesc struct{ Info *datasetmd.PageInfo }

func (f *fakePageDesc) GetInfo() *datasetmd.PageInfo { return f.Info }

func (f *fakePageDesc) GoString() string {
	return fmt.Sprintf("(start: %d, size: %d)", f.Info.DataOffset, f.Info.DataSize)
}

func newFakePage(offset, size uint64) *fakePageDesc {
	return &fakePageDesc{
		Info: &datasetmd.PageInfo{
			DataOffset: offset,
			DataSize:   size,
		},
	}
}
