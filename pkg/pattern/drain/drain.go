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
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/hashicorp/golang-lru/v2/simplelru"
	"github.com/prometheus/common/model"
)

const (
	timeResolution = model.Time(int64(time.Second*10) / 1e6)

	defaultVolumeSize = 500
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
	return &Config{
		LogClusterDepth: 8,
		SimTh:           0.3,
		MaxChildren:     100,
		ParamString:     "<_>",
		MaxClusters:     0,
	}
}

func New(config *Config) *Drain {
	if config.LogClusterDepth < 3 {
		panic("depth argument must be at least 3")
	}
	config.maxNodeDepth = config.LogClusterDepth - 2

	d := &Drain{
		config:      config,
		rootNode:    createNode(),
		idToCluster: createLogClusterCache(config.MaxClusters),
	}
	return d
}

type Drain struct {
	config          *Config
	rootNode        *Node
	idToCluster     *LogClusterCache
	clustersCounter int
}

func (d *Drain) Clusters() []*LogCluster {
	return d.idToCluster.Values()
}

// func (d *Drain) Iterate(fn func(*LogCluster) bool) {
// 	d.idToCluster.Iterate(fn)
// }

func (d *Drain) TrainTokens(tokens []string, stringer func([]string) string, ts int64) *LogCluster {
	return d.train(tokens, stringer, ts)
}

func (d *Drain) Train(content string, ts int64) *LogCluster {
	return d.train(d.getContentAsTokens(content), nil, ts)
}

func (d *Drain) train(tokens []string, stringer func([]string) string, ts int64) *LogCluster {
	matchCluster := d.treeSearch(d.rootNode, tokens, d.config.SimTh, false)
	// Match no existing log cluster
	if matchCluster == nil {
		d.clustersCounter++
		clusterID := d.clustersCounter
		matchCluster = &LogCluster{
			Tokens:   tokens,
			id:       clusterID,
			Size:     1,
			Stringer: stringer,
			Volume:   initVolume(model.TimeFromUnixNano(ts)),
		}
		d.idToCluster.Set(clusterID, matchCluster)
		d.addSeqToPrefixTree(d.rootNode, matchCluster)
	} else {
		newTemplateTokens := d.createTemplate(tokens, matchCluster.Tokens)
		matchCluster.Tokens = newTemplateTokens
		matchCluster.Size++
		matchCluster.append(model.TimeFromUnixNano(ts))
		// Touch cluster to update its state in the cache.
		d.idToCluster.Get(matchCluster.id)
	}
	return matchCluster
}

// Match against an already existing cluster. Match shall be perfect (sim_th=1.0). New cluster will not be created as a result of this call, nor any cluster modifications.
func (d *Drain) Match(content string) *LogCluster {
	contentTokens := d.getContentAsTokens(content)
	matchCluster := d.treeSearch(d.rootNode, contentTokens, 1.0, true)
	return matchCluster
}

func (d *Drain) getContentAsTokens(content string) []string {
	content = strings.TrimSpace(content)
	for _, extraDelimiter := range d.config.ExtraDelimiters {
		content = strings.Replace(content, extraDelimiter, " ", -1)
	}
	return strings.Split(content, " ")
}

func (d *Drain) treeSearch(rootNode *Node, tokens []string, simTh float64, includeParams bool) *LogCluster {
	tokenCount := len(tokens)

	// at first level, children are grouped by token (word) count
	curNode, ok := rootNode.keyToChildNode[strconv.Itoa(tokenCount)]

	// no template with same token count yet
	if !ok {
		return nil
	}

	// handle case of empty log string - return the single cluster in that group
	if tokenCount == 0 {
		return d.idToCluster.Get(curNode.clusterIDs[0])
	}

	// find the leaf node for this log - a path of nodes matching the first N tokens (N=tree depth)
	curNodeDepth := 1
	for _, token := range tokens {
		// at max depth
		if curNodeDepth >= d.config.maxNodeDepth {
			break
		}

		// this is last token
		if curNodeDepth == tokenCount {
			break
		}

		keyToChildNode := curNode.keyToChildNode
		curNode, ok = keyToChildNode[token]
		if !ok { // no exact next token exist, try wildcard node
			curNode, ok = keyToChildNode[d.config.ParamString]
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

// fastMatch Find the best match for a log message (represented as tokens) versus a list of clusters
func (d *Drain) fastMatch(clusterIDs []int, tokens []string, simTh float64, includeParams bool) *LogCluster {
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

func (d *Drain) getSeqDistance(clusterTokens, tokens []string, includeParams bool) (float64, int) {
	if len(clusterTokens) != len(tokens) {
		panic("seq1 seq2 be of same length")
	}

	simTokens := 0
	paramCount := 0
	for i := range clusterTokens {
		token1 := clusterTokens[i]
		token2 := tokens[i]
		// Require exact match for marked tokens
		if len(token1) > 0 && token1[0] == 0 && token1 != token2 {
			return 0, -1
		}
		if token1 == d.config.ParamString {
			paramCount++
		} else if token1 == token2 {
			simTokens++
		}
	}
	if includeParams {
		simTokens += paramCount
	}
	retVal := float64(simTokens) / float64(len(clusterTokens))
	return retVal, paramCount
}

func (d *Drain) addSeqToPrefixTree(rootNode *Node, cluster *LogCluster) {
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
		return
	}

	currentDepth := 1
	for _, token := range cluster.Tokens {
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
			if !d.hasNumbers(token) {
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
				if _, ok = curNode.keyToChildNode[d.config.ParamString]; !ok {
					newNode := createNode()
					curNode.keyToChildNode[d.config.ParamString] = newNode
					curNode = newNode
				} else {
					curNode = curNode.keyToChildNode[d.config.ParamString]
				}
			}
		} else {
			// if the token is matched
			curNode = curNode.keyToChildNode[token]
		}

		currentDepth++
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

func (d *Drain) createTemplate(tokens, matchClusterTokens []string) []string {
	if len(tokens) != len(matchClusterTokens) {
		panic("seq1 seq2 be of same length")
	}
	retVal := make([]string, len(matchClusterTokens))
	copy(retVal, matchClusterTokens)
	for i := range tokens {
		if tokens[i] != matchClusterTokens[i] {
			retVal[i] = d.config.ParamString
		}
	}
	return retVal
}
