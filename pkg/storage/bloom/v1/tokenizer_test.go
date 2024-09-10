package v1

import (
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"
	v2 "github.com/grafana/loki/v3/pkg/iter/v2"
)

const BigFile = "../../../logql/sketch/testdata/war_peace.txt"

func TestNGramIterator(t *testing.T) {
	t.Parallel()
	var (
		three      = NewNGramTokenizer(3, 0)
		threeSkip1 = NewNGramTokenizer(3, 1)
		threeSkip3 = NewNGramTokenizer(3, 3)
	)

	for _, tc := range []struct {
		desc  string
		t     *NGramTokenizer
		input string
		exp   []string
	}{
		{
			t:     three,
			input: "",
			exp:   []string{},
		},
		{
			t:     three,
			input: "ab",
			exp:   []string{},
		},
		{
			t:     three,
			input: "abcdefg",
			exp:   []string{"abc", "bcd", "cde", "def", "efg"},
		},
		{
			t:     threeSkip1,
			input: "abcdefg",
			exp:   []string{"abc", "cde", "efg"},
		},
		{
			t:     threeSkip3,
			input: "abcdefgh",
			exp:   []string{"abc", "efg"},
		},
		{
			t:     three,
			input: "日本語",
			exp:   []string{"日本語"},
		},
		{
			t:     four,
			input: "日本語日本語",
			exp: []string{
				"日本語日",
				"本語日本",
				"語日本語"},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			itr := tc.t.Tokens(tc.input)
			for _, exp := range tc.exp {
				require.True(t, itr.Next())
				require.Equal(t, exp, string(itr.At()))
			}
			require.False(t, itr.Next())
		})
	}
}

// Mainly this ensures we don't panic when a string ends in invalid utf8
func TestInvalidUTF8(t *testing.T) {
	x := NewNGramTokenizer(3, 0)

	input := "abc\x80"
	require.False(t, utf8.ValidString(input))
	itr := x.Tokens(input)
	require.True(t, itr.Next())
	require.Equal(t, []byte("abc"), itr.At())
	require.True(t, itr.Next())
	// we don't really care about the final rune returned and it's probably not worth the perf cost
	// to check for it
	require.Equal(t, []byte{0x62, 0x63, 0xef, 0xbf, 0xbd}, itr.At())
	require.False(t, itr.Next())
}

func TestPrefixedIterator(t *testing.T) {
	t.Parallel()
	var (
		three = NewNGramTokenizer(3, 0)
	)

	for _, tc := range []struct {
		desc  string
		input string
		exp   []string
	}{
		{
			input: "",
			exp:   []string{},
		},
		{
			input: "ab",
			exp:   []string{},
		},
		{
			input: "abcdefg",
			exp:   []string{"0123abc", "0123bcd", "0123cde", "0123def", "0123efg"},
		},

		{
			input: "日本語",
			exp:   []string{"0123日本語"},
		},
	} {
		prefix := []byte("0123")
		t.Run(tc.desc, func(t *testing.T) {
			itr := NewPrefixedTokenIter(prefix, len(prefix), three.Tokens(tc.input))
			for _, exp := range tc.exp {
				require.True(t, itr.Next())
				require.Equal(t, exp, string(itr.At()))
			}
			require.False(t, itr.Next())
		})
	}
}

const lorem = `
lorum ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod tempor incididunt ut labore et dolore magna
aliqua ut enim ad minim veniam quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat
duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur excepteur
sint occaecat cupidatat non proident sunt in culpa qui officia deserunt mollit anim id est
laborum ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod tempor incididunt ut labore et dolore magna
aliqua ut enim ad minim veniam quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat
duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur excepteur
sint occaecat cupidatat non proident sunt in culpa qui officia deserunt mollit anim id est
`

func BenchmarkTokens(b *testing.B) {
	var (
		v2Three      = NewNGramTokenizer(3, 0)
		v2ThreeSkip1 = NewNGramTokenizer(3, 1)
	)

	type impl struct {
		desc string
		f    func()
	}
	type tc struct {
		desc  string
		impls []impl
	}
	for _, tc := range []tc{
		{
			desc: "three",
			impls: []impl{
				{
					desc: "v2",
					f: func() {
						itr := v2Three.Tokens(lorem)
						for itr.Next() {
							_ = itr.At()
						}
					},
				},
			},
		},
		{
			desc: "threeSkip1",
			impls: []impl{
				{
					desc: "v2",
					f: func() {
						itr := v2ThreeSkip1.Tokens(lorem)
						for itr.Next() {
							_ = itr.At()
						}
					},
				},
			},
		},
		{
			desc: "threeChunk",
			impls: []impl{
				{
					desc: "v2",
					f: func() func() {
						buf, prefixLn := prefixedToken(v2Three.N(), ChunkRef{}, nil)
						return func() {
							itr := NewPrefixedTokenIter(buf, prefixLn, v2Three.Tokens(lorem))
							for itr.Next() {
								_ = itr.At()
							}
						}
					}(),
				},
			},
		},
		{
			desc: "threeSkip1Chunk",
			impls: []impl{
				{
					desc: "v2",
					f: func() func() {
						buf, prefixLn := prefixedToken(v2Three.N(), ChunkRef{}, nil)
						return func() {
							itr := NewPrefixedTokenIter(buf, prefixLn, v2ThreeSkip1.Tokens(lorem))
							for itr.Next() {
								_ = itr.At()
							}
						}
					}(),
				},
			},
		},
	} {
		b.Run(tc.desc, func(b *testing.B) {
			for _, impl := range tc.impls {
				b.Run(impl.desc, func(b *testing.B) {
					for i := 0; i < b.N; i++ {
						impl.f()
					}
				})
			}
		})
	}
}

func TestStructuredMetadataTokenizer(t *testing.T) {
	tokenizer := NewStructuredMetadataTokenizer("chunk")

	metadata := push.LabelAdapter{Name: "pod", Value: "loki-1"}
	expected := []string{"pod", "chunkpod", "loki-1", "chunkloki-1", "pod=loki-1", "chunkpod=loki-1"}

	tokenIter := tokenizer.Tokens(metadata)
	got, err := v2.Collect(tokenIter)
	require.NoError(t, err)
	require.Equal(t, expected, got)
}
