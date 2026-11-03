package drain

import (
	"fmt"
	"runtime"
	"testing"
)

// mapNode mirrors Node as it looked while children lived in a map. It exists only
// so both layouts can be priced in the same process against the same tree; the
// production tree no longer allocates a map per node.
type mapNode struct {
	keyToChildNode map[string]*mapNode
	clusterIDs     []int
}

// cloneAsMapTree rebuilds node with map-backed children. Keys are shared with the
// source tree, exactly as they are shared with the cluster templates in either
// layout, so the measured difference is child storage and nothing else.
func cloneAsMapTree(node *Node) *mapNode {
	clone := &mapNode{keyToChildNode: make(map[string]*mapNode)}
	if len(node.clusterIDs) > 0 {
		clone.clusterIDs = append(clone.clusterIDs, node.clusterIDs...)
	}
	for _, c := range node.children {
		clone.keyToChildNode[c.key] = cloneAsMapTree(c.child)
	}
	return clone
}

// cloneAsSliceTree rebuilds node with the production slice-backed children,
// including the append growth pattern a real Drain produces.
func cloneAsSliceTree(node *Node) *Node {
	clone := createNode()
	if len(node.clusterIDs) > 0 {
		clone.clusterIDs = append(clone.clusterIDs, node.clusterIDs...)
	}
	for _, c := range node.children {
		clone.setChild(c.key, cloneAsSliceTree(c.child))
	}
	return clone
}

type treeSizes struct {
	nodes     int
	sliceB    int64
	mapB      int64
	fanout    map[int]int
	maxFanout int
}

// measureTreeLayouts trains a Drain over lines and then prices two structurally
// identical copies of its prefix tree: one slice-backed, one map-backed.
func measureTreeLayouts(tb testing.TB, lines []string) treeSizes {
	tb.Helper()

	d := New(testTenant, DefaultConfig(), &fakeLimits{}, DetectLogFormat(lines[0]), nil)
	for _, line := range lines {
		d.Train(line, 0)
	}

	sizes := treeSizes{
		nodes:  countNodes(d.rootNode),
		fanout: map[int]int{},
	}
	collectFanout(d.rootNode, sizes.fanout, &sizes.maxFanout)

	base := liveBytes()
	sliceClone := cloneAsSliceTree(d.rootNode)
	afterSlice := liveBytes()
	mapClone := cloneAsMapTree(d.rootNode)
	afterMap := liveBytes()

	sizes.sliceB = int64(afterSlice) - int64(base)
	sizes.mapB = int64(afterMap) - int64(afterSlice)

	// Keep everything reachable until both measurements are taken.
	runtime.KeepAlive(d)
	runtime.KeepAlive(sliceClone)
	runtime.KeepAlive(mapClone)
	return sizes
}

func collectFanout(node *Node, hist map[int]int, maxFanout *int) {
	n := node.childCount()
	hist[n]++
	if n > *maxFanout {
		*maxFanout = n
	}
	for _, c := range node.children {
		collectFanout(c.child, hist, maxFanout)
	}
}

// TestPrefixTreeRetainedHeap_MapVsSlice is the memory proof for storing children
// in a slice: it builds real Drain trees from the testdata corpora and reports the
// heap each layout needs for the very same tree.
func TestPrefixTreeRetainedHeap_MapVsSlice(t *testing.T) {
	for _, file := range memoryBenchmarkFiles {
		lines := readTestdataLines(t, file)
		sizes := measureTreeLayouts(t, lines)

		saved := sizes.mapB - sizes.sliceB
		t.Logf("%-30s nodes=%-5d slice=%8d B (%6.1f B/node)  map=%8d B (%6.1f B/node)  saved=%8d B (%4.1f%%)  max_fanout=%d",
			file, sizes.nodes,
			sizes.sliceB, float64(sizes.sliceB)/float64(sizes.nodes),
			sizes.mapB, float64(sizes.mapB)/float64(sizes.nodes),
			saved, 100*float64(saved)/float64(sizes.mapB),
			sizes.maxFanout)
		t.Logf("%-30s children per node: %v", file, sizes.fanout)

		if sizes.sliceB >= sizes.mapB {
			t.Errorf("%s: slice-backed children (%d B) should stay below map-backed children (%d B)", file, sizes.sliceB, sizes.mapB)
		}
	}
}

// BenchmarkNodeChildLookup_MapVsSlice prices the lookup that replaced the map
// read. Fan-out 1-15 covers every internal node (Config.MaxChildren caps it),
// while 77 covers the root, whose children are token counts (4..80) and are
// therefore the widest scan Drain can perform: one per Train call.
//
// Each case looks up the last key, the worst case for a linear scan.
func BenchmarkNodeChildLookup_MapVsSlice(b *testing.B) {
	for _, fanout := range []int{1, 2, 5, 15, 77} {
		keys := make([]string, fanout)
		for i := range keys {
			// Root keys are token counts; inner keys are log tokens. Numeric keys are
			// the shorter, cheaper comparison, so they keep the scan honest at 77.
			keys[i] = fmt.Sprintf("%d", i+4)
		}
		want := keys[len(keys)-1]

		sliceNode := createNode()
		mapped := &mapNode{keyToChildNode: make(map[string]*mapNode)}
		for _, key := range keys {
			sliceNode.setChild(key, createNode())
			mapped.keyToChildNode[key] = &mapNode{}
		}

		b.Run(fmt.Sprintf("fanout=%d/slice", fanout), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, ok := sliceNode.getChild(want); !ok {
					b.Fatal("miss")
				}
			}
		})

		b.Run(fmt.Sprintf("fanout=%d/map", fanout), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, ok := mapped.keyToChildNode[want]; !ok {
					b.Fatal("miss")
				}
			}
		})
	}
}

// BenchmarkPrefixTreeChildren_MapVsSlice reports the same comparison as
// b.ReportMetric values so the delta shows up in benchstat-friendly output.
func BenchmarkPrefixTreeChildren_MapVsSlice(b *testing.B) {
	for _, file := range memoryBenchmarkFiles {
		lines := readTestdataLines(b, file)

		for _, layout := range []string{"slice", "map"} {
			b.Run(fmt.Sprintf("%s/%s", file, layout), func(b *testing.B) {
				var totalBytes, totalNodes int64
				for i := 0; i < b.N; i++ {
					sizes := measureTreeLayouts(b, lines)
					if layout == "slice" {
						totalBytes += sizes.sliceB
					} else {
						totalBytes += sizes.mapB
					}
					totalNodes += int64(sizes.nodes)
				}

				n := float64(b.N)
				b.ReportMetric(float64(totalBytes)/n, "tree_B/drain")
				b.ReportMetric(float64(totalBytes)/float64(totalNodes), "tree_B/node")
				b.ReportMetric(float64(totalNodes)/n, "nodes/drain")
			})
		}
	}
}
