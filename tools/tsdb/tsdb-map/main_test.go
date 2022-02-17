package main

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractChecksum(t *testing.T) {
	x := rand.Uint32()
	s := fmt.Sprintf("a/b/c:d:e:%x", x)
	require.Equal(t, x, extractChecksumFromChunkID([]byte(s)))
}
