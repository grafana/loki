package index

import (
	"fmt"
	"slices"
	"testing"

	"github.com/RoaringBitmap/roaring/v2"
	"github.com/grafana/loki/v3/pkg/dataobj/index/ngrams"
	"github.com/stretchr/testify/require"
)

func TestPostingsSerialization(t *testing.T) {
	postings := make([]*roaring.Bitmap, 10)
	for i := 0; i < 10; i++ {
		postings[i] = roaring.NewBitmap()
	}
	postings[0].Add(1)
	postings[1].Add(2)
	postings[2].Add(3)

	postingBytes := make([][]byte, 0, 10)
	for i, posting := range postings {
		postingBuffer, err := posting.MarshalBinary()
		if err != nil {
			t.Fatalf("failed to marshal posting: %v", err)
		}
		postingBytes = append(postingBytes, postingBuffer)
		fmt.Printf("Posting %d: %d bytes\n", i, len(postingBuffer))
	}

	for i, byts := range postingBytes {
		fmt.Printf("Posting %d: %d bytes\n", i, len(byts))
		bm := roaring.NewBitmap()
		err := bm.UnmarshalBinary(byts)
		if err != nil {
			t.Fatalf("failed to unmarshal posting: %v", err)
		}
		bm.GetCardinality()
	}
}

func TestTrigramIndex(t *testing.T) {
	sizeOfAlphabet := len(alphabetRunes)
	trigrams := make([]string, 0, sizeOfAlphabet*sizeOfAlphabet*sizeOfAlphabet)
	for _, run := range alphabetRunes {
		for _, run2 := range alphabetRunes {
			for _, run3 := range alphabetRunes {
				trigrams = append(trigrams, fmt.Sprintf("%c%c%c", run, run2, run3))
			}
		}
	}
	slices.Sort(trigrams)

	for _, run := range alphabetRunes {
		for _, run2 := range alphabetRunes {
			for _, run3 := range alphabetRunes {
				gram := fmt.Sprintf("%c%c%c", run, run2, run3)
				expected, _ := slices.BinarySearch(trigrams, gram)
				actual := ngrams.NgramIndex(gram)
				require.Equal(t, expected, actual)
			}
		}
	}
}

func BenchmarkTrigramIndex(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ngrams.NgramIndex("abc")
	}
}

func BenchmarkTrigramIndex3(b *testing.B) {
	trigrams := make([]string, 0, len(alphabetRunes)*len(alphabetRunes)*len(alphabetRunes))
	for _, run := range alphabetRunes {
		for _, run2 := range alphabetRunes {
			for _, run3 := range alphabetRunes {
				trigrams = append(trigrams, fmt.Sprintf("%c%c%c", run, run2, run3))
			}
		}
	}
	slices.Sort(trigrams)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = slices.BinarySearch(trigrams, "abc")
	}
}
