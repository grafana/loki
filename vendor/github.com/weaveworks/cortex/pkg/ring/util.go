package ring

import (
	"math/rand"
	"sort"
	"time"
)

// GenerateTokens make numTokens random tokens, none of which clash
// with takenTokens.  Assumes takenTokens is sorted.
func GenerateTokens(numTokens int, takenTokens []uint32) []uint32 {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	tokens := []uint32{}
	for i := 0; i < numTokens; {
		candidate := r.Uint32()
		j := sort.Search(len(takenTokens), func(i int) bool {
			return takenTokens[i] >= candidate
		})
		if j < len(takenTokens) && takenTokens[j] == candidate {
			continue
		}
		tokens = append(tokens, candidate)
		i++
	}
	return tokens
}
