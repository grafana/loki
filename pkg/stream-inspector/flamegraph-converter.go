package stream_inspector

import "golang.org/x/exp/slices"

const magicNumber = 0

type FlamegraphConverter struct {
}

func (f *FlamegraphConverter) CovertTrees(trees []*Tree) FlameBearer {
	dictionary := make(map[string]int)
	var levels [][]float64

	iterator := NewTreesLevelsIterator(trees)
	levelIndex := -1
	for iterator.HasNextLevel() {
		levelIndex++
		levelIterator := iterator.NextLevelIterator()
		var level []float64
		for levelIterator.HasNext() {
			block := levelIterator.Next()
			blockName := block.node.Name
			blockWeight := block.node.Weight
			currentBlockOffset := float64(0)
			// we need to find the offset for the first child block to place it exactly under the parent
			if levelIndex > 0 && block.childIndex == 0 {
				previousLevel := levels[levelIndex-1]
				parentsIndexInPreviousLevel := block.parentBlock.indexInLevel

				// calculate the offset of the parent from the left side(offsets+weight of all left neighbours + parents offset)
				parentsOffset := previousLevel[parentsIndexInPreviousLevel*4]
				parentsGlobalOffset := parentsOffset
				parentsLeftNeighbour := block.parentBlock.leftNeighbour
				for parentsLeftNeighbour != nil {
					leftNeighbourOffset := previousLevel[parentsLeftNeighbour.indexInLevel*4]
					leftNeighbourWeight := previousLevel[parentsLeftNeighbour.indexInLevel*4+1]
					parentsGlobalOffset += leftNeighbourOffset + leftNeighbourWeight
					parentsLeftNeighbour = parentsLeftNeighbour.leftNeighbour
				}

				currentBlockOffset = parentsGlobalOffset
				left := block.leftNeighbour
				// iterate over all already added blocks from the left
				for left != nil {
					leftNeighbourOffset := level[left.indexInLevel*4]
					leftNeighbourWeight := level[left.indexInLevel*4+1]
					currentBlockOffset -= leftNeighbourOffset + leftNeighbourWeight
					left = left.leftNeighbour
				}
			}

			index, exists := dictionary[blockName]
			if !exists {
				index = len(dictionary)
				dictionary[blockName] = index
			}

			level = append(level, []float64{currentBlockOffset, blockWeight, magicNumber, float64(index)}...)
		}
		levels = append(levels, level)
	}
	firstLevel := levels[0]
	totalWidth := float64(0)
	for i := 1; i < len(firstLevel); i += 4 {
		totalWidth += firstLevel[i]
	}
	names := make([]string, len(dictionary))
	for name, index := range dictionary {
		names[index] = name
	}
	return FlameBearer{
		Units:    "bytes",
		NumTicks: totalWidth,
		MaxSelf:  totalWidth,
		Names:    names,
		Levels:   levels,
	}
}

type TreesLevelsIterator struct {
	trees        []*Tree
	currentLevel *LevelBlocksIterator
}

func NewTreesLevelsIterator(trees []*Tree) *TreesLevelsIterator {
	return &TreesLevelsIterator{trees: trees}
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
		if len(block.node.Children) > 0 {
			return true
		}
	}
	return false
}

func (i *TreesLevelsIterator) NextLevelIterator() *LevelBlocksIterator {
	if i.currentLevel == nil {
		levelNodes := make([]*LevelBlock, 0, len(i.trees))
		for index, tree := range i.trees {
			var leftNeighbour *LevelBlock
			if index > 0 {
				leftNeighbour = levelNodes[index-1]
			}
			levelNodes = append(levelNodes, &LevelBlock{leftNeighbour: leftNeighbour, node: tree.Root, childIndex: index, indexInLevel: index})
		}
		slices.SortStableFunc(levelNodes, func(a, b *LevelBlock) bool {
			return a.node.Weight > b.node.Weight
		})
		i.currentLevel = NewLevelBlocksIterator(levelNodes)
		return i.currentLevel
	}

	var nextLevelBlocks []*LevelBlock
	for i.currentLevel.HasNext() {
		block := i.currentLevel.Next()
		slices.SortStableFunc(block.node.Children, func(a, b *Node) bool {
			return a.Weight > b.Weight
		})
		for index, child := range block.node.Children {
			var leftNeighbour *LevelBlock
			if len(nextLevelBlocks) > 0 {
				leftNeighbour = nextLevelBlocks[len(nextLevelBlocks)-1]
			}
			nextLevelBlocks = append(nextLevelBlocks, &LevelBlock{leftNeighbour: leftNeighbour, childIndex: index, indexInLevel: len(nextLevelBlocks), node: child, parentBlock: block})
		}
	}
	i.currentLevel = NewLevelBlocksIterator(nextLevelBlocks)
	return i.currentLevel
}

type LevelBlock struct {
	parentBlock   *LevelBlock
	leftNeighbour *LevelBlock
	childIndex    int
	node          *Node
	indexInLevel  int
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
