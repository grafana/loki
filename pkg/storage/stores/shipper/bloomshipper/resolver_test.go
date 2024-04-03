package bloomshipper

import (
	"testing"

	"github.com/stretchr/testify/require"

	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
)

func TestResolver_ParseMetaKey(t *testing.T) {
	r := defaultKeyResolver{}
	ref := MetaRef{
		Ref: Ref{
			TenantID:  "tenant",
			TableName: "table_1",
			Bounds:    v1.NewBounds(0x0000, 0xffff),
			Checksum:  43981,
		},
	}

	// encode block ref as string
	loc := r.Meta(ref)
	path := loc.LocalPath()
	require.Equal(t, "bloom/table_1/tenant/metas/0000000000000000-000000000000ffff-abcd.json", path)

	// parse encoded string into block ref
	parsed, err := r.ParseMetaKey(key(path))
	require.NoError(t, err)
	require.Equal(t, ref, parsed)
}

func TestResolver_ParseBlockKey(t *testing.T) {
	r := defaultKeyResolver{}
	ref := BlockRef{
		Ref: Ref{
			TenantID:       "tenant",
			TableName:      "table_1",
			Bounds:         v1.NewBounds(0x0000, 0xffff),
			StartTimestamp: 0,
			EndTimestamp:   3600000,
			Checksum:       43981,
		},
	}

	// encode block ref as string
	loc := r.Block(ref)
	path := loc.LocalPath()
	require.Equal(t, "bloom/table_1/tenant/blocks/0000000000000000-000000000000ffff/0-3600000-abcd.tar.gz", path)

	// parse encoded string into block ref
	parsed, err := r.ParseBlockKey(key(path))
	require.NoError(t, err)
	require.Equal(t, ref, parsed)
}

func TestResolver_ShardedPrefixedResolver(t *testing.T) {

	blockRef := BlockRef{
		Ref: Ref{
			TenantID:       "tenant",
			TableName:      "table_1",
			Bounds:         v1.NewBounds(0x0000, 0xffff),
			StartTimestamp: 0,
			EndTimestamp:   3600000,
			Checksum:       48350,
		},
	}

	metaRef := MetaRef{
		Ref: Ref{
			TenantID:  "tenant",
			TableName: "table_1",
			Bounds:    v1.NewBounds(0x0000, 0xffff),
			Checksum:  43981,
		},
	}

	t.Run("empty prefixes cause error", func(t *testing.T) {
		_, err := NewShardedPrefixedResolver([]string{}, defaultKeyResolver{})
		require.ErrorContains(t, err, "requires at least 1 prefix")
	})

	t.Run("single prefix", func(t *testing.T) {
		r, err := NewShardedPrefixedResolver([]string{"prefix"}, defaultKeyResolver{})
		require.NoError(t, err)
		loc := r.Meta(metaRef)
		require.Equal(t, "prefix/bloom/table_1/tenant/metas/0000000000000000-000000000000ffff-abcd.json", loc.LocalPath())
		loc = r.Block(blockRef)
		require.Equal(t, "prefix/bloom/table_1/tenant/blocks/0000000000000000-000000000000ffff/0-3600000-bcde.tar.gz", loc.LocalPath())
	})

	t.Run("multiple prefixes", func(t *testing.T) {
		r, err := NewShardedPrefixedResolver([]string{"a", "b", "c", "d"}, defaultKeyResolver{})
		require.NoError(t, err)
		loc := r.Meta(metaRef)
		require.Equal(t, "b/bloom/table_1/tenant/metas/0000000000000000-000000000000ffff-abcd.json", loc.LocalPath())
		loc = r.Block(blockRef)
		require.Equal(t, "d/bloom/table_1/tenant/blocks/0000000000000000-000000000000ffff/0-3600000-bcde.tar.gz", loc.LocalPath())
	})
}
