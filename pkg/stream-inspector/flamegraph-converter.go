package stream_inspector

import "golang.org/x/exp/slices"

const magicNumber = 0

type FlamebearerMode uint8

const (
	Diff = iota
	Single
)

type FlamegraphConverter struct {
	Left  []*Tree
	Right []*Tree
	Mode  FlamebearerMode
}

func (f *FlamegraphConverter) CovertTrees() FlameGraph {
	dictionary := make(map[string]int)
	var levels []FlamegraphLevel
	//limit := 50000
	//added := 0
	iterator := NewTreesLevelsIterator(f.Left, f.Right, f.Mode)
	levelIndex := -1
	for iterator.HasNextLevel() /* && added <= limit*/ {
		levelIndex++
		levelIterator := iterator.NextLevelIterator()
		var level FlamegraphLevel
		for levelIterator.HasNext() {
			block := levelIterator.Next()
			var blockName string
			if block.leftNode != nil {
				blockName = block.leftNode.Name
			} else {
				blockName = block.rightNode.Name
			}

			var leftNodeWeight float64
			if block.leftNode != nil {
				leftNodeWeight = block.leftNode.Weight
			}
			var rightNodeWeight float64
			if block.rightNode != nil {
				rightNodeWeight = block.rightNode.Weight
			}
			currentBlockOffset := float64(0)
			// we need to find the offset for the first child block to place it exactly under the parent
			if levelIndex > 0 && block.childBlockIndex == 0 {
				previousLevel := levels[levelIndex-1]
				parentsIndexInPreviousLevel := block.parentBlock.indexInLevel
				parentsGlobalOffset := previousLevel.blocksGlobalOffsets[parentsIndexInPreviousLevel]
				currentBlockOffset = parentsGlobalOffset - level.curentWidth
			}

			index, exists := dictionary[blockName]
			if !exists {
				index = len(dictionary)
				dictionary[blockName] = index
			}
			level.blocksGlobalOffsets = append(level.blocksGlobalOffsets, currentBlockOffset+level.curentWidth)
			level.curentWidth += currentBlockOffset + leftNodeWeight + rightNodeWeight

			level.blocks = append(level.blocks, []float64{currentBlockOffset, leftNodeWeight, magicNumber}...)
			if f.Mode == Diff {
				level.blocks = append(level.blocks, []float64{0, rightNodeWeight, magicNumber}...)
			}
			level.blocks = append(level.blocks, float64(index))
			//added++
		}
		levels = append(levels, level)
	}

	firstLevel := levels[0].blocks
	totalLeft := float64(0)
	totalRight := float64(0)

	blockParamsLength := 4
	if f.Mode == Diff {
		blockParamsLength = 7
	}
	for i := 1; i < len(firstLevel); i += blockParamsLength {
		// leftWidth
		totalLeft += firstLevel[i]
		if f.Mode == Diff {
			//rightWidth
			totalRight += firstLevel[i+3]
		}
	}
	names := make([]string, len(dictionary))
	for name, index := range dictionary {
		names[index] = name
	}
	levelsBlocks := make([][]float64, 0, len(levels))
	for _, level := range levels {
		levelsBlocks = append(levelsBlocks, level.blocks)
	}
	var leftTicks *float64
	var rightTicks *float64
	format := "single"
	if f.Mode == Diff {
		format = "double"
		leftTicks = &totalLeft
		rightTicks = &totalRight
	}

	return FlameGraph{
		Version: 1,
		FlameBearer: FlameBearer{
			Units:    "bytes",
			NumTicks: totalLeft + totalRight,
			MaxSelf:  totalLeft + totalRight,
			Names:    names,
			Levels:   levelsBlocks,
		},
		Metadata: Metadata{
			Format:     format,
			SpyName:    "dotnetspy",
			SampleRate: 100,
			Units:      "bytes",
			Name:       "Logs Volumes",
		},
		LeftTicks:  leftTicks,
		RightTicks: rightTicks,
	}

}

type Metadata struct {
	Format     string `json:"format,omitempty"`
	SpyName    string `json:"spyName,omitempty"`
	SampleRate int    `json:"sampleRate,omitempty"`
	Units      string `json:"units,omitempty"`
	Name       string `json:"name,omitempty"`
}

type FlameGraph struct {
	Version     int         `json:"version,omitempty"`
	FlameBearer FlameBearer `json:"flamebearer"`
	Metadata    Metadata    `json:"metadata"`
	LeftTicks   *float64    `json:"leftTicks,omitempty"`
	RightTicks  *float64    `json:"rightTicks,omitempty"`
}

type FlamegraphLevel struct {
	curentWidth         float64
	blocks              []float64
	blocksGlobalOffsets []float64
}

type TreesLevelsIterator struct {
	left         []*Tree
	right        []*Tree
	currentLevel *LevelBlocksIterator
	mode         FlamebearerMode
}

func NewTreesLevelsIterator(left []*Tree, right []*Tree, mode FlamebearerMode) *TreesLevelsIterator {
	return &TreesLevelsIterator{left: left, right: right, mode: mode}
}

func (i *TreesLevelsIterator) HasNextLevel() bool {
	if i.currentLevel == nil {
		return true
	}

	//reset  before and after
	i.currentLevel.Reset()
	defer i.currentLevel.Reset()

	for i.currentLevel.HasNext() {
		block := i.currentLevel.Next()
		// if at least one block at current level has children
		if block.leftNode != nil && len(block.leftNode.Children) > 0 || block.rightNode != nil && len(block.rightNode.Children) > 0 {
			return true
		}
	}
	return false
}

func (i *TreesLevelsIterator) mergeNodes(parent *LevelBlock, left []*Node, right []*Node) []*LevelBlock {
	leftByNameMap := i.mapByNodeName(left)
	rightByNameMap := i.mapByNodeName(right)
	blocks := make([]*LevelBlock, 0, len(leftByNameMap)+len(rightByNameMap))
	for _, leftNode := range leftByNameMap {
		rightNode := rightByNameMap[leftNode.Name]
		blocks = append(blocks, &LevelBlock{
			parentBlock: parent,
			leftNode:    leftNode,
			rightNode:   rightNode,
		})
	}
	for _, rightNode := range rightByNameMap {
		//skip nodes that exists in leftNodes to add only diff from the right nodes
		_, exists := leftByNameMap[rightNode.Name]
		if exists {
			continue
		}

		blocks = append(blocks, &LevelBlock{
			parentBlock: parent,
			rightNode:   rightNode,
		})
	}
	slices.SortStableFunc(blocks, func(a, b *LevelBlock) bool {
		return maxWeight(a.leftNode, a.rightNode) > maxWeight(b.leftNode, b.rightNode)
	})
	return blocks
}

func maxWeight(left *Node, right *Node) float64 {
	if left == nil {
		return right.Weight
	}
	if right == nil {
		return left.Weight
	}
	if left.Weight > right.Weight {
		return left.Weight
	}
	return right.Weight
}

func (i *TreesLevelsIterator) NextLevelIterator() *LevelBlocksIterator {
	if i.currentLevel == nil {
		leftNodes := make([]*Node, 0, len(i.left))
		for _, tree := range i.left {
			leftNodes = append(leftNodes, tree.Root)
		}
		rightNodes := make([]*Node, 0, len(i.right))
		for _, tree := range i.right {
			rightNodes = append(rightNodes, tree.Root)
		}
		levelBlocks := i.mergeNodes(nil, leftNodes, rightNodes)

		for index, block := range levelBlocks {
			if index > 0 {
				block.leftNeighbour = levelBlocks[index-1]
			}
			block.childBlockIndex = index
			block.indexInLevel = index
		}
		i.currentLevel = NewLevelBlocksIterator(levelBlocks)
		return i.currentLevel
	}

	var nextLevelBlocks []*LevelBlock
	for i.currentLevel.HasNext() {
		currentBlock := i.currentLevel.Next()
		var left []*Node
		if currentBlock.leftNode != nil {
			left = currentBlock.leftNode.Children
		}
		var right []*Node
		if currentBlock.rightNode != nil {
			right = currentBlock.rightNode.Children
		}
		levelBlocks := i.mergeNodes(currentBlock, left, right)
		for index, block := range levelBlocks {
			block.childBlockIndex = index
			block.indexInLevel = len(nextLevelBlocks)
			if len(nextLevelBlocks) > 0 {
				block.leftNeighbour = nextLevelBlocks[len(nextLevelBlocks)-1]
			}
			nextLevelBlocks = append(nextLevelBlocks, block)
		}
	}
	i.currentLevel = NewLevelBlocksIterator(nextLevelBlocks)
	return i.currentLevel
}

func (i *TreesLevelsIterator) mapByNodeName(nodes []*Node) map[string]*Node {
	result := make(map[string]*Node, len(nodes))
	for _, node := range nodes {
		result[node.Name] = node
	}
	return result
}

type LevelBlock struct {
	parentBlock     *LevelBlock
	leftNeighbour   *LevelBlock
	childBlockIndex int
	leftNode        *Node
	rightNode       *Node
	indexInLevel    int
}

// iterates over Nodes at the level
type LevelBlocksIterator struct {
	blocks []*LevelBlock
	index  int
}

func NewLevelBlocksIterator(blocks []*LevelBlock) *LevelBlocksIterator {
	return &LevelBlocksIterator{blocks: blocks, index: 0}
}

func (i *LevelBlocksIterator) HasNext() bool {
	return i.index < len(i.blocks)
}

func (i *LevelBlocksIterator) Reset() {
	i.index = 0
}

func (i *LevelBlocksIterator) Next() *LevelBlock {
	next := i.blocks[i.index]
	i.index++
	return next
}

type FlameBearer struct {
	Units    string      `json:"units,omitempty"`
	NumTicks float64     `json:"numTicks" json:"num_ticks,omitempty"`
	MaxSelf  float64     `json:"maxSelf" json:"max_self,omitempty"`
	Names    []string    `json:"names,omitempty" json:"names,omitempty" json:"names,omitempty"`
	Levels   [][]float64 `json:"levels,omitempty" json:"levels,omitempty" json:"levels,omitempty"`
}
