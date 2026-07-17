package wirecodec

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/dataset/encoding/wirecodec/internal/typemd"
)

func TestTypeCodec(t *testing.T) {
	t.Run("scalars", func(t *testing.T) {
		tests := []struct {
			name string
			typ  types.Type
			want *typemd.Type
		}{
			{
				name: "null",
				typ:  &types.Null{},
				want: &typemd.Type{Kind: &typemd.Type_Null{Null: &typemd.Null{}}},
			},
			{
				name: "uint32",
				typ:  &types.Uint32{Nullable: true},
				want: &typemd.Type{Kind: &typemd.Type_Uint32{Uint32: &typemd.Uint32{Nullable: true}}},
			},
			{
				name: "uint64",
				typ:  &types.Uint64{Nullable: true},
				want: &typemd.Type{Kind: &typemd.Type_Uint64{Uint64: &typemd.Uint64{Nullable: true}}},
			},
			{
				name: "int32",
				typ:  &types.Int32{Nullable: true},
				want: &typemd.Type{Kind: &typemd.Type_Int32{Int32: &typemd.Int32{Nullable: true}}},
			},
			{
				name: "int64",
				typ:  &types.Int64{Nullable: true},
				want: &typemd.Type{Kind: &typemd.Type_Int64{Int64: &typemd.Int64{Nullable: true}}},
			},
			{
				name: "bool",
				typ:  &types.Bool{Nullable: true},
				want: &typemd.Type{Kind: &typemd.Type_Bool{Bool: &typemd.Bool{Nullable: true}}},
			},
			{
				name: "utf8",
				typ:  &types.UTF8{Nullable: true},
				want: &typemd.Type{Kind: &typemd.Type_Utf8{Utf8: &typemd.UTF8{Nullable: true}}},
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				var dict dictionary
				enc := &layoutEncoder{dict: &dict}

				encoded := enc.encodeType(tc.typ)
				decoded, err := (&layoutDecoder{dict: &dict}).decodeType(encoded)
				require.NoError(t, err)

				require.Equal(t, tc.want, encoded)
				require.Equal(t, tc.typ, decoded)
			})
		}
	})

	t.Run("struct", func(t *testing.T) {
		var dict dictionary
		enc := &layoutEncoder{dict: &dict}
		typ := &types.Struct{
			Fields: []types.StructField{
				{Name: "name", Type: &types.UTF8{}},
				{Name: "age", Type: &types.Int32{Nullable: true}},
			},
			Nullable: true,
		}

		encoded := enc.encodeType(typ)
		decoded, err := (&layoutDecoder{dict: &dict}).decodeType(encoded)
		require.NoError(t, err)

		require.Equal(t, &typemd.Type{
			Kind: &typemd.Type_Struct{Struct: &typemd.Struct{
				Fields: []*typemd.Struct_Field{
					{NameRef: 0, Type: &typemd.Type{Kind: &typemd.Type_Utf8{Utf8: &typemd.UTF8{}}}},
					{NameRef: 1, Type: &typemd.Type{Kind: &typemd.Type_Int32{Int32: &typemd.Int32{Nullable: true}}}},
				},
				Nullable: true,
			}},
		}, encoded)
		require.Equal(t, []string{"name", "age"}, dict.keys)
		require.Equal(t, typ, decoded)
	})

	t.Run("list", func(t *testing.T) {
		var dict dictionary
		enc := &layoutEncoder{dict: &dict}
		typ := &types.List{Element: &types.UTF8{}, Nullable: true}

		encoded := enc.encodeType(typ)
		decoded, err := (&layoutDecoder{dict: &dict}).decodeType(encoded)
		require.NoError(t, err)

		require.Equal(t, &typemd.Type{
			Kind: &typemd.Type_List{List: &typemd.List{
				Element:  &typemd.Type{Kind: &typemd.Type_Utf8{Utf8: &typemd.UTF8{}}},
				Nullable: true,
			}},
		}, encoded)
		require.Equal(t, typ, decoded)
	})
}
