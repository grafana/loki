package drain

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNodeChildAccessors(t *testing.T) {
	t.Parallel()

	t.Run("get on empty node misses", func(t *testing.T) {
		t.Parallel()

		n := createNode()
		child, ok := n.getChild("missing")
		require.False(t, ok)
		require.Nil(t, child)
		require.Equal(t, -1, n.childIndex("missing"))
		require.Equal(t, 0, n.childCount())
	})

	t.Run("set then get hits", func(t *testing.T) {
		t.Parallel()

		n := createNode()
		a, b := createNode(), createNode()
		n.setChild("a", a)
		n.setChild("b", b)

		require.Equal(t, 2, n.childCount())
		require.Equal(t, 0, n.childIndex("a"))
		require.Equal(t, 1, n.childIndex("b"))

		got, ok := n.getChild("a")
		require.True(t, ok)
		require.Same(t, a, got)

		got, ok = n.getChild("b")
		require.True(t, ok)
		require.Same(t, b, got)

		_, ok = n.getChild("c")
		require.False(t, ok)
	})

	t.Run("set replaces without duplicating the key", func(t *testing.T) {
		t.Parallel()

		n := createNode()
		first, second := createNode(), createNode()
		n.setChild("a", first)
		n.setChild("a", second)

		require.Equal(t, 1, n.childCount())
		got, ok := n.getChild("a")
		require.True(t, ok)
		require.Same(t, second, got)
	})

	t.Run("delete keeps insertion order and clears the vacated slot", func(t *testing.T) {
		t.Parallel()

		n := createNode()
		keys := []string{"a", "b", "c", "d"}
		for _, key := range keys {
			n.setChild(key, createNode())
		}

		n.deleteChildAt(n.childIndex("b"))

		require.Equal(t, 3, n.childCount())
		require.Equal(t, []string{"a", "c", "d"}, childKeys(n))
		_, ok := n.getChild("b")
		require.False(t, ok)

		// The removed subtree must not stay reachable through the backing array,
		// otherwise pruning would never release memory.
		tail := n.children[:len(n.children)+1]
		require.Equal(t, nodeChild{}, tail[len(tail)-1])
	})

	t.Run("delete of the only child empties the node", func(t *testing.T) {
		t.Parallel()

		n := createNode()
		n.setChild("a", createNode())
		n.deleteChildAt(0)

		require.Equal(t, 0, n.childCount())
		require.Empty(t, childKeys(n))
	})
}

// TestPrefixTreeMaxChildren pins the fan-out contract: a node accepts distinct
// tokens until one slot is left, spends that slot on the wildcard child, and
// funnels every later token through it.
func TestPrefixTreeMaxChildren(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	d := New(testTenant, cfg, &fakeLimits{}, FormatUnknown, nil)

	tokensFor := func(i int) []string {
		// Only the first token varies, so every cluster lands under the same
		// token-count node and competes for its children.
		return []string{fmt.Sprintf("tok%c", rune('a'+i)), "beta", "gamma", "delta", "epsilon"}
	}

	const inserted = 20
	for i := 0; i < inserted; i++ {
		cluster := &LogCluster{id: i + 1, Tokens: tokensFor(i), Size: 1}
		d.idToCluster.Set(cluster.id, cluster)
		d.addSeqToPrefixTree(d.rootNode, cluster)
	}

	require.Equal(t, 1, d.rootNode.childCount(), "root groups clusters by token count")
	firstLayer, ok := d.rootNode.getChild("5")
	require.True(t, ok, "first layer key is the token count")

	require.Equal(t, cfg.MaxChildren, firstLayer.childCount(), "fan-out must stop at MaxChildren")

	wantKeys := make([]string, 0, cfg.MaxChildren)
	for i := 0; i < cfg.MaxChildren-1; i++ {
		wantKeys = append(wantKeys, tokensFor(i)[0])
	}
	wantKeys = append(wantKeys, cfg.ParamString)
	require.Equal(t, wantKeys, childKeys(firstLayer), "last slot is spent on the wildcard child")

	// Tokens beyond the limit follow the wildcard branch rather than adding children.
	wildcard, ok := firstLayer.getChild(cfg.ParamString)
	require.True(t, ok)
	require.NotEmpty(t, wildcard.children)

	// treeSearch falls back to the wildcard node for a token never seen before.
	unseen := []string{"never-seen-token", "beta", "gamma", "delta", "epsilon"}
	require.NotNil(t, d.treeSearch(d.rootNode, unseen, cfg.SimTh, false),
		"unseen leading token should match through the wildcard branch")

	// An exact token still beats the wildcard.
	exact := tokensFor(0)
	match := d.treeSearch(d.rootNode, exact, cfg.SimTh, false)
	require.NotNil(t, match)
	require.Equal(t, 1, match.id, "exact match must win over the wildcard branch")
}

// TestPrefixTreePruneRemovesEmptyChildren checks that pruning drops branches whose
// clusters are gone and keeps the ones still referenced.
func TestPrefixTreePruneRemovesEmptyChildren(t *testing.T) {
	t.Parallel()

	d := New(testTenant, DefaultConfig(), &fakeLimits{}, FormatUnknown, nil)

	now := time.Now()
	lines := []string{
		"alpha alpha alpha keep",
		"beta beta beta drop",
	}
	for i, line := range lines {
		d.Train(line, now.Add(time.Duration(i)*time.Millisecond).UnixNano())
	}

	require.Len(t, d.Clusters(), 2)
	before := countNodes(d.rootNode)
	require.Equal(t, 1, d.rootNode.childCount(), "both lines have the same token count")

	var dropped *LogCluster
	for _, c := range d.Clusters() {
		if c.String() == "beta beta beta drop" {
			dropped = c
		}
	}
	require.NotNil(t, dropped)
	d.Delete(dropped)

	require.Equal(t, before, countNodes(d.rootNode), "deleting a cluster alone must not touch the tree")

	d.Prune()

	require.Len(t, d.Clusters(), 1)
	require.Less(t, countNodes(d.rootNode), before, "prune must release the dead branch")

	// The surviving cluster is still reachable through the pruned tree.
	survivor := d.Clusters()[0]
	require.Equal(t, survivor, d.treeSearch(d.rootNode, survivor.Tokens, DefaultConfig().SimTh, false))
}
