// MIT License
//
// Copyright (c) 2022 faceair
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package drain

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unsafe"

	"github.com/hashicorp/golang-lru/v2/simplelru"
	"github.com/prometheus/common/model"
	"golang.org/x/exp/maps"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/pattern/tokenization"
)

type Config struct {
	maxNodeDepth    int
	LogClusterDepth int
	SimTh           float64
	MaxChildren     int
	ExtraDelimiters []string
	MaxClusters     int
	ParamString     string
}

func createLogClusterCache(maxSize int) *LogClusterCache {
	if maxSize == 0 {
		maxSize = math.MaxInt
	}
	cache, _ := simplelru.NewLRU[int, *LogCluster](maxSize, nil)
	return &LogClusterCache{
		cache: cache,
	}
}

type LogClusterCache struct {
	cache simplelru.LRUCache[int, *LogCluster]
}

func (c *LogClusterCache) Values() []*LogCluster {
	values := make([]*LogCluster, 0)
	for _, key := range c.cache.Keys() {
		if value, ok := c.cache.Peek(key); ok {
			values = append(values, value)
		}
	}
	return values
}

func (c *LogClusterCache) Set(key int, cluster *LogCluster) {
	c.cache.Add(key, cluster)
}

func (c *LogClusterCache) Iterate(fn func(*LogCluster) bool) {
	for _, key := range c.cache.Keys() {
		if value, ok := c.cache.Peek(key); ok {
			if !fn(value) {
				return
			}
		}
	}
}

func (c *LogClusterCache) Get(key int) *LogCluster {
	cluster, ok := c.cache.Get(key)
	if !ok {
		return nil
	}
	return cluster
}

func createNode() *Node {
	return &Node{
		keyToChildNode: make(map[string]*Node),
		clusterIDs:     make([]int, 0),
	}
}

type Node struct {
	keyToChildNode map[string]*Node
	clusterIDs     []int
}

func DefaultConfig() *Config {
	// TODO(kolesnikovae):
	//
	// This is crucial for Drain to ensure that the first LogClusterDepth tokens
	// are constant (see https://jiemingzhu.github.io/pub/pjhe_icws2017.pdf).
	// We should remove any variables such as timestamps, IDs, IPs, counters, etc.
	// from these tokens.
	//
	// Moreover, Drain is not designed for structured logs. Therefore, we should
	// handle logfmt (and, probably, JSON) logs in a special way:
	//
	// The parse tree should have a fixed length, and the depth should be
	// determined by the number of fields in the logfmt message.
	// A parsing tree should be maintained for each unique field set.
	return &Config{
		// At training, if at the depth of LogClusterDepth there is a cluster with
		// similarity coefficient greater that SimTh, then the log message is added
		// to that cluster. Otherwise, a new cluster is created.
		//
		// LogClusterDepth should be equal to the number of constant tokens from
		// the beginning of the message that likely determine the message contents.
		//
		//  > In this step, Drain traverses from a 1-st layer node, which
		//  > is searched in step 2, to a leaf node. This step is based on
		//  > the assumption that tokens in the beginning positions of a log
		//  > message are more likely to be constants. Specifically, Drain
		//  > selects the next internal node by the tokens in the beginning
		//  > positions of the log message
		LogClusterDepth: 18,
		// SimTh is basically a ratio of matching/total in the cluster.
		// Cluster tokens: "foo <*> bar fred"
		//       Log line: "foo bar baz qux"
		//                  *   *   *   x
		// Similarity of these sequences is 0.75 (the distance)
		// Both SimTh and MaxClusterDepth impact branching factor: the greater
		// MaxClusterDepth and SimTh, the less the chance that there will be
		// "similar" clusters, but the greater the footprint.
		SimTh:       0.5,
		MaxChildren: 8,
		ParamString: `<_>`,
		MaxClusters: 300,
	}
}

func New(config *Config) *Drain {
	return NewWithTokenizer(config, &AdaptiveTokenizer{})
}

func NewWithTokenizer(config *Config, tokenizer PatternTokenizer) *Drain {
	if config.LogClusterDepth < 3 {
		panic("depth argument must be at least 3")
	}
	config.maxNodeDepth = config.LogClusterDepth - 2

	d := &Drain{
		config:      config,
		rootNode:    createNode(),
		idToCluster: createLogClusterCache(config.MaxClusters),
		tokenizer:   tokenizer,
	}
	return d
}

type Drain struct {
	config          *Config
	rootNode        *Node
	idToCluster     *LogClusterCache
	clustersCounter int
	tokenizer       PatternTokenizer

	processedTokenCache simplelru.LRUCache[string, string]
}

func (d *Drain) Clusters() []*LogCluster {
	return d.idToCluster.Values()
}

func (d *Drain) TrainTokens(tokens [][]byte, stringer func([]string) string, ts int64) *LogCluster {
	return d.train(tokens, stringer, ts)
}

func (d *Drain) Train(content string, ts int64) *LogCluster {
	return d.train(d.tokenizer.Marshal([]byte(content)), d.tokenizer.Unmarshal, ts)
}

func byteSlicesToStrings(in [][]byte) []string {
	output := make([]string, len(in))
	for i, byteSlice := range in {
		output[i] = string(byteSlice)
	}
	return output
}

func (d *Drain) train(tokens [][]byte, stringer func([]string) string, ts int64) *LogCluster {
	for i, token := range tokens {
		if len(token) > 50 {
			openIndex := bytes.Index(token[40:50], []byte("<"))
			closeIndex := bytes.Index(token[40:51], []byte(">"))
			if openIndex != -1 && closeIndex == -1 {
				tokens[i] = append(token[:openIndex], []byte(d.config.ParamString)...)
			} else {
				tokens[i] = append(token[:50], []byte(d.config.ParamString)...)
			}
		}
	}
	// We have to repeat this twice or more if the tree is modified during training
	for {
		matchCluster := d.treeSearch(d.rootNode, tokens, d.config.SimTh, false)
		// Match no existing log cluster
		if matchCluster == nil {
			clusterID := d.clustersCounter + 1
			matchCluster = &LogCluster{
				Tokens:   byteSlicesToStrings(tokens), // Copy the bytes to new strings because we are (probably) storing them
				id:       clusterID,
				Size:     1,
				Stringer: stringer,
				Chunks:   Chunks{},
			}
			// This call may modify the prefix tree to replace a static value with a placeholder (e.g. 3214 -> <NUM>)
			treeModified := d.addSeqToPrefixTree(d.rootNode, matchCluster)
			if treeModified {
				continue
			}
			// No modification, we just add the new cluster
			d.clustersCounter++
			d.idToCluster.Set(clusterID, matchCluster)
		} else {
			// An existing cluster was modified instead of inserting a new cluster, so we should be able to find it now.
			newTemplateTokens := d.createTemplate(tokens, matchCluster.Tokens)
			matchCluster.Tokens = newTemplateTokens
			matchCluster.append(model.TimeFromUnixNano(ts))
			// Touch cluster to update its state in the cache.
			d.idToCluster.Get(matchCluster.id)
		}
		return matchCluster
	}
}

func (d *Drain) String() string {
	return d.tree("", d.rootNode, 0)
}

func (d *Drain) tree(key string, root *Node, depth int) string {
	builder := strings.Builder{}
	builder.WriteString(fmt.Sprintf("%s%s [%d clusters]\n", strings.Repeat("-", depth), key, len(root.clusterIDs)))
	for _, child := range maps.Keys(root.keyToChildNode) {
		builder.WriteString(d.tree(child, root.keyToChildNode[child], depth+1))
	}
	return builder.String()
}

func (d *Drain) TrainPattern(content string, samples []*logproto.PatternSample) *LogCluster {
	tokens := d.tokenizer.Marshal([]byte(content))
	for {
		matchCluster := d.treeSearch(d.rootNode, tokens, d.config.SimTh, false)
		// Match no existing log cluster
		if matchCluster == nil {
			clusterID := d.clustersCounter + 1
			matchCluster = &LogCluster{
				Tokens: byteSlicesToStrings(tokens), // Copy the bytes to new strings because we are (probably) storing them
				id:     clusterID,
				Size:   1,
			}
			// This call may modify the prefix tree to replace a static value with a placeholder (e.g. 3214 -> <NUM>)
			treeModified := d.addSeqToPrefixTree(d.rootNode, matchCluster)
			if treeModified {
				continue
			}
			// No modification, we just add the new cluster
			d.clustersCounter++
			d.idToCluster.Set(clusterID, matchCluster)
		} else {
			newTemplateTokens := d.createTemplate(tokens, matchCluster.Tokens)
			matchCluster.Tokens = newTemplateTokens
			// Touch cluster to update its state in the cache.
			d.idToCluster.Get(matchCluster.id)
		}
		matchCluster.merge(samples)
		return matchCluster
	}
}

func deduplicatePlaceholders(tokens []string, param string) []string {
	if len(tokens) < 2 {
		return tokens
	}
	i := 1
	for k := 1; k < len(tokens); k++ {
		if tokens[k] != param || tokens[k] != tokens[k-1] {
			if i != k {
				tokens[i] = tokens[k]
			}
			i++
		}
	}
	return tokens[:i]
}

func (d *Drain) PatternString(c *LogCluster) string {
	s := strings.Join(deduplicatePlaceholders(c.Tokens, d.config.ParamString), " ")
	if s == d.config.ParamString {
		return ""
	}
	return s
}

func (d *Drain) Delete(cluster *LogCluster) {
	d.idToCluster.cache.Remove(cluster.id)
}

// Match against an already existing cluster. Match shall be perfect (sim_th=1.0). New cluster will not be created as a result of this call, nor any cluster modifications.
func (d *Drain) Match(content string) *LogCluster {
	contentTokens := d.tokenizer.Marshal([]byte(content))
	matchCluster := d.treeSearch(d.rootNode, contentTokens, 1.0, true)
	return matchCluster
}

func (d *Drain) treeSearch(rootNode *Node, tokens [][]byte, simTh float64, includeParams bool) *LogCluster {
	tokenCount := len(tokens)

	// at first level, children are grouped by token (word) count
	curNode, ok := rootNode.keyToChildNode[strconv.Itoa(tokenCount)]

	// no template with same token count yet
	if !ok {
		return nil
	}

	// handle case of empty log string - return the single cluster in that group
	if tokenCount < 2 {
		return d.idToCluster.Get(curNode.clusterIDs[0])
	}

	// find the leaf node for this log - a path of nodes matching the first N tokens (N=tree depth)
	curNodeDepth := 1
	for i, token := range tokens {
		// at max depth
		if curNodeDepth >= d.config.maxNodeDepth {
			break
		}

		// this is last token
		if curNodeDepth == tokenCount {
			break
		}

		keyToChildNode := curNode.keyToChildNode
		curNode, ok = keyToChildNode[unsafeBytesAsString(token)]
		if !ok { // no exact next token exists, try the preprocessed token
			processedKey := d.getPreprocessToken(token)
			curNode, ok = keyToChildNode[unsafeBytesAsString(processedKey)]
			if !ok { // no exact or processed token exist, try wildcard node
				curNode, ok = keyToChildNode[d.config.ParamString]
			} else {
				// If we matched a processed node, update our tokens to use this match
				tokens[i] = processedKey
			}
		}
		if !ok { // no wildcard node exist
			return nil
		}
		curNodeDepth++
	}

	// get best match among all clusters with same prefix, or None if no match is above sim_th
	cluster := d.fastMatch(curNode.clusterIDs, tokens, simTh, includeParams)
	return cluster
}

// Converts a byte slice into a string with zero allocations
// Should not be used outside the scope of the function it is created in (e.g. avoid using for map keys)
func unsafeBytesAsString(in []byte) string {
	return *(*string)(unsafe.Pointer(&in))
}

// fastMatch Find the best match for a log message (represented as tokens) versus a list of clusters
func (d *Drain) fastMatch(clusterIDs []int, tokens [][]byte, simTh float64, includeParams bool) *LogCluster {
	var matchCluster, maxCluster *LogCluster

	maxSim := -1.0
	maxParamCount := -1
	for _, clusterID := range clusterIDs {
		// Try to retrieve cluster from cache with bypassing eviction
		// algorithm as we are only testing candidates for a match.
		cluster := d.idToCluster.Get(clusterID)
		if cluster == nil {
			continue
		}
		curSim, paramCount := d.getSeqDistance(cluster.Tokens, tokens, includeParams)
		if paramCount < 0 {
			continue
		}
		if curSim > maxSim || (curSim == maxSim && paramCount > maxParamCount) {
			maxSim = curSim
			maxParamCount = paramCount
			maxCluster = cluster
		}
	}
	if maxSim >= simTh {
		matchCluster = maxCluster
	}
	return matchCluster
}

func stringAndBytesEqual(one string, two []byte) bool {
	if len(one) != len(two) {
		return false
	}
	for i, first := range one {
		if first != int32(two[i]) {
			return false
		}
	}
	return true
}

func (d *Drain) getSeqDistance(clusterTokens []string, tokens [][]byte, includeParams bool) (float64, int) {
	if len(clusterTokens) != len(tokens) {
		panic("seq1 seq2 be of same length")
	}

	simTokens := 0
	paramCount := 0
	for i := range clusterTokens {
		token1 := clusterTokens[i]
		token2 := tokens[i]
		// Require exact match for marked tokens
		if len(token1) > 0 && token1[0] == 0 && !stringAndBytesEqual(token1, token2) {
			return 0, -1
		}
		if token1 == d.config.ParamString {
			paramCount++
		} else if stringAndBytesEqual(token1, token2) {
			simTokens++
		}
	}
	if includeParams {
		simTokens += paramCount
	}
	retVal := float64(simTokens) / float64(len(clusterTokens))
	return retVal, paramCount
}

func (d *Drain) getPreprocessToken(token []byte) []byte {
	/*	val, ok := d.processedTokenCache.Get(token)
		if ok {
			return val
		}*/
	return tokenization.Preprocess(token, true, true)
	/*	d.processedTokenCache.Add(token, output)
		return output*/
}

func (d *Drain) addSeqToPrefixTree(rootNode *Node, cluster *LogCluster) bool {
	tokenCount := len(cluster.Tokens)
	tokenCountStr := strconv.Itoa(tokenCount)

	firstLayerNode, ok := rootNode.keyToChildNode[tokenCountStr]
	if !ok {
		firstLayerNode = createNode()
		rootNode.keyToChildNode[tokenCountStr] = firstLayerNode
	}
	curNode := firstLayerNode

	// handle case of empty log string
	if tokenCount == 0 {
		curNode.clusterIDs = append(curNode.clusterIDs, cluster.id)
		return false
	}

	currentDepth := 1
	treeModified := false
	for i, token := range cluster.Tokens {
		// if at max depth or this is last token in template - add current log cluster to the leaf node
		if (currentDepth >= d.config.maxNodeDepth) || currentDepth >= tokenCount {
			// clean up stale clusters before adding a new one.
			newClusterIDs := make([]int, 0, len(curNode.clusterIDs))
			for _, clusterID := range curNode.clusterIDs {
				if d.idToCluster.Get(clusterID) != nil {
					newClusterIDs = append(newClusterIDs, clusterID)
				}
			}
			newClusterIDs = append(newClusterIDs, cluster.id)
			curNode.clusterIDs = newClusterIDs
			break
		}

		// if token not matched in this layer of existing tree.
		if _, ok = curNode.keyToChildNode[token]; !ok {
			// There is no exact match. Rather than immediately creating a catch-all object, see if we're close to another key at this layer and join to that.
			processedToken := d.getPreprocessToken([]byte(token))
			matchingKey := ""
			for _, key := range maps.Keys(curNode.keyToChildNode) {
				processedKey := d.getPreprocessToken([]byte(key))
				if bytes.Equal(processedKey, processedToken) {
					matchingKey = key
					cluster.Tokens[i] = string(processedToken)
					break
				}
			}

			if matchingKey == "" {
				if _, ok = curNode.keyToChildNode[d.config.ParamString]; ok {
					if len(curNode.keyToChildNode) < d.config.MaxChildren {
						newNode := createNode()
						curNode.keyToChildNode[token] = newNode
						curNode = newNode
					} else {
						curNode = curNode.keyToChildNode[d.config.ParamString]
					}
				} else {
					if len(curNode.keyToChildNode)+1 < d.config.MaxChildren {
						newNode := createNode()
						curNode.keyToChildNode[token] = newNode
						curNode = newNode
					} else if len(curNode.keyToChildNode)+1 == d.config.MaxChildren {
						newNode := createNode()
						curNode.keyToChildNode[d.config.ParamString] = newNode
						curNode = newNode
					} else {
						curNode = curNode.keyToChildNode[d.config.ParamString]
					}
				}
			} else {
				// We're grouping with an existing log pattern, so rename the old edge to the aggregate key.
				if !stringAndBytesEqual(matchingKey, processedToken) {
					stringToken := string(processedToken)
					d.replaceAllTokens(curNode.keyToChildNode[matchingKey], i, stringToken)
					curNode.keyToChildNode[stringToken] = curNode.keyToChildNode[matchingKey]
					delete(curNode.keyToChildNode, matchingKey)
					treeModified = true
				}
			}
		} else {
			// if the token is matched
			curNode = curNode.keyToChildNode[token]
		}

		currentDepth++
	}
	return treeModified
}

func (d *Drain) replaceAllTokens(node *Node, i int, replacement string) {
	for _, cid := range node.clusterIDs {
		cluster := d.idToCluster.Get(cid)
		if cluster == nil {
			continue
		}
		cluster.Tokens[i] = replacement
	}
	for _, child := range node.keyToChildNode {
		d.replaceAllTokens(child, i, replacement)
	}
}

func (d *Drain) hasNumbers(s string) bool {
	for _, c := range s {
		if unicode.IsNumber(c) {
			return true
		}
	}
	return false
}

func (d *Drain) createTemplate(tokens [][]byte, matchClusterTokens []string) []string {
	if len(tokens) != len(matchClusterTokens) {
		panic("seq1 seq2 be of same length")
	}
	retVal := make([]string, len(matchClusterTokens))
	copy(retVal, matchClusterTokens)
	for i := range tokens {
		if !stringAndBytesEqual(matchClusterTokens[i], tokens[i]) {
			retVal[i] = d.config.ParamString
		}
	}
	return retVal
}
