package ring

import (
	"math/rand"
	"sort"
	"time"
)

type TokenGenerator interface {
	// GenerateTokens generates at most requestedTokensCount unique tokens, none of which clashes with
	// the given allTakenTokens, representing the set of all tokens currently present in the ring.
	// Generated tokens are sorted.
	GenerateTokens(requestedTokensCount int, allTakenTokens []uint32) Tokens

	// CanJoin checks whether the instance owning this TokenGenerator can join the set of the given instances satisfies,
	// and fails if it is not possible.
	CanJoin(instances map[string]InstanceDesc) error

	// CanJoinEnabled returns true if the instance owning this TokenGenerator should perform the CanJoin check before
	// it tries to join the ring.
	CanJoinEnabled() bool
}

type RandomTokenGenerator struct{}

func NewRandomTokenGenerator() *RandomTokenGenerator {
	return &RandomTokenGenerator{}
}

// GenerateTokens generates at most requestedTokensCount unique random tokens, none of which clashes with
// the given allTakenTokens, representing the set of all tokens currently present in the ring.
// Generated tokens are sorted.
func (t *RandomTokenGenerator) GenerateTokens(requestedTokensCount int, allTakenTokens []uint32) Tokens {
	if requestedTokensCount <= 0 {
		return []uint32{}
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	used := make(map[uint32]bool, len(allTakenTokens))
	for _, v := range allTakenTokens {
		used[v] = true
	}

	tokens := make([]uint32, 0, requestedTokensCount)
	for i := 0; i < requestedTokensCount; {
		candidate := r.Uint32()
		if used[candidate] {
			continue
		}
		used[candidate] = true
		tokens = append(tokens, candidate)
		i++
	}

	// Ensure returned tokens are sorted.
	sort.Slice(tokens, func(i, j int) bool {
		return tokens[i] < tokens[j]
	})

	return tokens
}

func (t *RandomTokenGenerator) CanJoin(_ map[string]InstanceDesc) error {
	return nil
}

func (t *RandomTokenGenerator) CanJoinEnabled() bool {
	return false
}
