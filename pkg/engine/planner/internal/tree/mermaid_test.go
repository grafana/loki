package tree

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMermaidDiagram_Write(t *testing.T) {
	root := dummyPlan()

	var sb strings.Builder
	m := NewMermaid(&sb)
	err := m.Write(root)
	require.NoError(t, err)

	t.Logf("\n%s\n", sb.String())
}
