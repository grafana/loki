package stream_inspector

import (
	"github.com/grafana/loki/pkg/storage/stores/tsdb"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/exp/slices"
	"math"
	"sort"
)

type Inspector struct {
}

func (i *Inspector) BuildTrees(streams []tsdb.Series, volumesMap map[model.Fingerprint]float64, matchers []*labels.Matcher) ([]*Tree, error) {
	labelNamesOrder := i.sortLabelNamesByPopularity(streams, matchers)

	rootLabelNameToThreeMap := make(map[string]*Tree)
	var threes []*Tree

	for _, stream := range streams {
		streamLabels := make(labels.Labels, 0, len(stream.Labels))
		for _, label := range stream.Labels {
			if _, skip := labelsToSkip[label.Name]; skip {
				continue
			}
			streamLabels = append(streamLabels, label)
		}

		slices.SortStableFunc(streamLabels, func(a, b labels.Label) bool {
			return labelNamesOrder[a.Name] < labelNamesOrder[b.Name]
		})
		rootLabelName := streamLabels[0].Name
		rootThree, exists := rootLabelNameToThreeMap[rootLabelName]
		if !exists {
			rootThree = &Tree{Root: &Node{Name: rootLabelName, ChildNameToIndex: make(map[string]int)}}
			rootLabelNameToThreeMap[rootLabelName] = rootThree
			threes = append(threes, rootThree)
		}
		streamVolume, err := i.getStreamVolume(stream, volumesMap)
		if err != nil {
			return nil, errors.Wrap(err, "can not build tree due to error")
		}
		currentNode := rootThree.Root
		for i, label := range streamLabels {
			var labelNameNode *Node
			if i == 0 {
				labelNameNode = currentNode
			} else {
				labelNameNode = currentNode.FindOrCreateChild(label.Name)
			}
			labelNameNode.Weight += streamVolume

			labelValueNode := labelNameNode.FindOrCreateChild(label.Value)
			labelValueNode.Weight += streamVolume
			if i < len(streamLabels)-1 {
				currentNode = labelValueNode
			}
		}

	}
	return threes, nil
}

func (i *Inspector) getStreamVolume(stream tsdb.Series, volumesMap map[model.Fingerprint]float64) (float64, error) {
	streamVolume, exists := volumesMap[stream.Fingerprint]
	if !exists {
		return 0, errors.New("stream volume not found")
	}
	return streamVolume, nil
}

type Iterator[T any] struct {
	values   []T
	position int
}

func NewIterator[T any](values []T) *Iterator[T] {
	return &Iterator[T]{values: values}
}

func (i *Iterator[T]) HasNext() bool {
	return i.position < len(i.values)-1
}

func (i *Iterator[T]) Next() T {
	next := i.values[i.position]
	i.position++
	return next
}

func (i *Iterator[T]) Reset() {
	i.position = 0
}

var labelsToSkip = map[string]any{
	"__stream_shard__": nil,
}

func (i *Inspector) sortLabelNamesByPopularity(streams []tsdb.Series, matchers []*labels.Matcher) map[string]int {
	labelNameCounts := make(map[string]uint32)
	uniqueLabelNames := make([]string, 0, 1000)
	for _, stream := range streams {
		for _, label := range stream.Labels {
			if _, skip := labelsToSkip[label.Name]; skip {
				continue
			}
			count := labelNameCounts[label.Name]
			if count == 0 {
				uniqueLabelNames = append(uniqueLabelNames, label.Name)
			}
			labelNameCounts[label.Name] = count + 1
		}
	}
	matchersLabels := make(map[string]int, len(matchers))
	for idx, matcher := range matchers {
		matchersLabels[matcher.Name] = idx
	}
	sort.SliceStable(uniqueLabelNames, func(i, j int) bool {
		leftLabel := uniqueLabelNames[i]
		rightLabel := uniqueLabelNames[j]
		leftLabelPriority := -1
		if leftLabelIndex, used := matchersLabels[leftLabel]; used {
			leftLabelPriority = math.MaxInt - leftLabelIndex
		}
		rightLabelPriority := -1
		if rightLabelIndex, used := matchersLabels[rightLabel]; used {
			rightLabelPriority = math.MaxInt - rightLabelIndex
		}
		leftCount := labelNameCounts[leftLabel]
		rightCount := labelNameCounts[rightLabel]
		// sort by streams count (desc) and by label name (asc)

		return leftLabelPriority > rightLabelPriority || (leftLabelPriority == rightLabelPriority && leftCount > rightCount) || leftCount == rightCount && leftLabel < rightLabel
	})
	labelNameToOrderMap := make(map[string]int, len(uniqueLabelNames))
	for idx, name := range uniqueLabelNames {
		labelNameToOrderMap[name] = idx
	}
	return labelNameToOrderMap
}

type Tree struct {
	Root *Node `json:"root"`
}

type Node struct {
	Name             string         `json:"name,omitempty"`
	Weight           float64        `json:"weight,omitempty"`
	ChildNameToIndex map[string]int `json:"-"`
	Children         []*Node        `json:"children,omitempty"`
}

func (n *Node) FindOrCreateChild(childName string) *Node {
	childIndex, exists := n.ChildNameToIndex[childName]
	if !exists {
		n.Children = append(n.Children, &Node{Name: childName, ChildNameToIndex: make(map[string]int)})
		childIndex = len(n.Children) - 1
		n.ChildNameToIndex[childName] = childIndex
	}
	return n.Children[childIndex]
}
