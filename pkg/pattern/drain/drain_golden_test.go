package drain

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// childKeys and childByKey isolate the test suite from how a Node stores its
// children, so the golden hashes below stay comparable across that change.
func childKeys(node *Node) []string {
	keys := make([]string, 0, node.childCount())
	for _, c := range node.children {
		keys = append(keys, c.key)
	}
	return keys
}

func childByKey(node *Node, key string) (*Node, bool) {
	return node.getChild(key)
}

// goldenFiles pins both the clustering result and the exact prefix tree shape
// produced by Train (and by Train followed by Prune) over every testdata corpus.
// The values were captured from the map-backed prefix tree and must survive any
// change to how a Node stores its children.
//
// Set DRAIN_GOLDEN_PRINT=1 to print the current values when the corpora change.
var goldenFiles = []struct {
	file          string
	clusters      int
	sha256        string
	nodes         int
	tree          string
	prunedNodes   int
	prunedTree    string
	prunedCluster int
}{
	{file: "testdata/agent-logfmt.txt", clusters: 15, sha256: "63fbe63eaff23c1f0cf57da434e9357c83a7854e8399ebb73e91de1e2d315ec9", nodes: 111, tree: "fe2e6ce9cee743f68b826c47444f3af73ba857c457f1ff2f91f2bd608fbe52ff", prunedNodes: 63, prunedTree: "f2b23ea479227344ee50fca8971791cc54f9d7011e995feecccf64f5b2735b55", prunedCluster: 8},
	{file: "testdata/calico.txt", clusters: 81, sha256: "a982388216a9f1e4749307a9947f745fb1318a17a2639f64cda159ae24f7f28d", nodes: 1294, tree: "40de9b39d214d8e73e3d4f29ba9de066a8034cc8c05899ff8b42fd5039cf533d", prunedNodes: 786, prunedTree: "36eb7957d168129d8a652ed7a343c93e3c9099df29c5c777ce9d9a6250e4a851", prunedCluster: 41},
	{file: "testdata/distributor-logfmt.txt", clusters: 1, sha256: "ae7b7411306022a8a6b828dc6d8a6e43cb1592428d336d8fe783f958908d834a", nodes: 13, tree: "a06a998a1bec84e25dfd2ba5f1c63245e9db262d6f0b3ae297354b35fda188ff", prunedNodes: 13, prunedTree: "a06a998a1bec84e25dfd2ba5f1c63245e9db262d6f0b3ae297354b35fda188ff", prunedCluster: 1},
	{file: "testdata/drone-json.txt", clusters: 1, sha256: "ed03e6e22b442584ae577baae0c586581b56cf8dddce9b689f9239afbd1e427d", nodes: 5, tree: "70c881338c76742d04d9dbd67bf37336842aefaf4cb19a5db0c4505ab9248119", prunedNodes: 5, prunedTree: "70c881338c76742d04d9dbd67bf37336842aefaf4cb19a5db0c4505ab9248119", prunedCluster: 1},
	{file: "testdata/grafana-ruler.txt", clusters: 300, sha256: "011fcfbc2d7964366e2788b7a79df1fc29d1092a5d938c6a203f5b03b778f621", nodes: 3347, tree: "7e1f01533b25aa133125bfb6fcb373a8bc0647d52390c8924cfb3dc3804eaa0a", prunedNodes: 1597, prunedTree: "7658efb70a880faf285d29e51c7fa21911a0df3651d0e781de844824933682d2", prunedCluster: 154},
	{file: "testdata/ingester-logfmt.txt", clusters: 3, sha256: "ffa527aba5f4928a4668b31ddc688f7dacbb9be024b8aa35844bbdfda2a00f25", nodes: 28, tree: "3c36d50973ec3ddb352de55228aa96df6ed82c1d8f6f9442daef021fa5c0e96a", prunedNodes: 23, prunedTree: "71b5c55bc51325c42cf2cdf206efed2db5832415df3abb93c43ea9cc00299cdd", prunedCluster: 2},
	{file: "testdata/journald.txt", clusters: 111, sha256: "65f6a1b399af39831fb37b2e6be04847a5e783242f345b60fcd7285777488429", nodes: 1686, tree: "5ca408fc45b7e34036cc1ed725ad850efffa925a4dc05a123943ce8b007d7c7f", prunedNodes: 943, prunedTree: "113d301a161ff28252f9a65ef5c2e369f17521c05fcdb90ffe02c7865ad88d45", prunedCluster: 56},
	{file: "testdata/kafka.txt", clusters: 13, sha256: "a2bd50ab76fadd2ec1b08f3a535da8aa10fa267b56a29f3c004cd262a84df634", nodes: 291, tree: "440d5aaff0d2e25d30eb028d6602a9c94d473910c75e01272f91c6993ebe093e", prunedNodes: 168, prunedTree: "5b7bf1f15097996726b8cf84e3106a9b89ac9ce5be470973b0541e29d222f598", prunedCluster: 7},
	{file: "testdata/kubernetes.txt", clusters: 34, sha256: "981203ad898a7537ef350bb949b6c820873f405c0ee13b774c3bb66ad6ef2eea", nodes: 740, tree: "b1a475d6da03b37bfa13efcc10a757e0a934bea9b68868be77580fb12b56acb5", prunedNodes: 405, prunedTree: "ed26ba7f7ada0e32b06b8623d6197fc95fb4705bda36901ae7be71cd027bb520", prunedCluster: 17},
	{file: "testdata/vault.txt", clusters: 1, sha256: "1de109a53af58d6aaa75f71ffeb91e7fd32d6468f4bd6c6914420fd985bdabef", nodes: 11, tree: "590beef42d2e170ef73f262adb3ad107680eddf3b13c930d09c94b2f126cced1", prunedNodes: 11, prunedTree: "590beef42d2e170ef73f262adb3ad107680eddf3b13c930d09c94b2f126cced1", prunedCluster: 1},
}

func readTestdataLines(tb testing.TB, path string) []string {
	tb.Helper()

	file, err := os.Open(path)
	require.NoError(tb, err)
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	require.NoError(tb, scanner.Err())
	require.NotEmpty(tb, lines)
	return lines
}

// clusterFingerprint hashes the sorted cluster templates held by d.
func clusterFingerprint(d *Drain) (int, string) {
	clusters := d.Clusters()
	templates := make([]string, 0, len(clusters))
	for _, c := range clusters {
		templates = append(templates, c.String())
	}
	slices.Sort(templates)

	sum := sha256.Sum256([]byte(strings.Join(templates, "\n")))
	return len(clusters), hex.EncodeToString(sum[:])
}

// treeFingerprint hashes the full prefix tree: every node's children (sorted by
// key, so the hash is independent of child storage order) and cluster IDs.
func treeFingerprint(root *Node) (int, string) {
	h := sha256.New()
	nodes := writeNode(h, root)
	return nodes, hex.EncodeToString(h.Sum(nil))
}

func writeNode(h io.Writer, node *Node) int {
	fmt.Fprintf(h, "ids=%v(", node.clusterIDs)

	children := childKeys(node)
	slices.Sort(children)

	total := 1
	for _, key := range children {
		child, ok := childByKey(node, key)
		if !ok {
			panic("child disappeared: " + key)
		}
		fmt.Fprintf(h, "%q:", key)
		total += writeNode(h, child)
	}
	fmt.Fprint(h, ")")
	return total
}

// trainAndFingerprint trains a fresh Drain over lines, then reports cluster and
// tree fingerprints before and after dropping every even cluster ID and pruning.
func trainAndFingerprint(tb testing.TB, lines []string) (clusters int, clusterSum string, nodes int, treeSum string, prunedNodes int, prunedTreeSum string, prunedClusters int) {
	tb.Helper()

	d := New(testTenant, DefaultConfig(), &fakeLimits{}, DetectLogFormat(lines[0]), nil)
	for _, line := range lines {
		d.Train(line, 0)
	}

	clusters, clusterSum = clusterFingerprint(d)
	nodes, treeSum = treeFingerprint(d.rootNode)

	// Drop half the clusters so Prune has empty branches to collect.
	for _, c := range d.Clusters() {
		if c.id%2 == 0 {
			d.Delete(c)
		}
	}
	d.Prune()

	prunedClusters = len(d.Clusters())
	prunedNodes, prunedTreeSum = treeFingerprint(d.rootNode)
	return
}

func TestTrainGoldenClusters(t *testing.T) {
	t.Parallel()

	printGolden := os.Getenv("DRAIN_GOLDEN_PRINT") == "1"

	for _, tt := range goldenFiles {
		t.Run(tt.file, func(t *testing.T) {
			t.Parallel()

			lines := readTestdataLines(t, tt.file)
			clusters, clusterSum, nodes, treeSum, prunedNodes, prunedTreeSum, prunedClusters := trainAndFingerprint(t, lines)

			if printGolden {
				t.Logf("{file: %q, clusters: %d, sha256: %q, nodes: %d, tree: %q, prunedNodes: %d, prunedTree: %q, prunedCluster: %d},",
					tt.file, clusters, clusterSum, nodes, treeSum, prunedNodes, prunedTreeSum, prunedClusters)
				return
			}

			require.Equal(t, tt.clusters, clusters, "cluster count changed for %s", tt.file)
			require.Equal(t, tt.sha256, clusterSum, "cluster templates changed for %s", tt.file)
			require.Equal(t, tt.nodes, nodes, "prefix tree node count changed for %s", tt.file)
			require.Equal(t, tt.tree, treeSum, "prefix tree shape changed for %s", tt.file)
			require.Equal(t, tt.prunedCluster, prunedClusters, "surviving cluster count changed for %s", tt.file)
			require.Equal(t, tt.prunedNodes, prunedNodes, "pruned node count changed for %s", tt.file)
			require.Equal(t, tt.prunedTree, prunedTreeSum, "pruned tree shape changed for %s", tt.file)
		})
	}
}
