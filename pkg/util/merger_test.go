package util

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func TestMergeSampleSets(t *testing.T) {
	now := model.Now()
	sample1 := model.SamplePair{Timestamp: now, Value: 1}
	sample2 := model.SamplePair{Timestamp: now.Add(1 * time.Second), Value: 2}
	sample3 := model.SamplePair{Timestamp: now.Add(4 * time.Second), Value: 3}
	sample4 := model.SamplePair{Timestamp: now.Add(8 * time.Second), Value: 7}

	for _, c := range []struct {
		samplesA []model.SamplePair
		samplesB []model.SamplePair
		expected []model.SamplePair
	}{
		{
			samplesA: []model.SamplePair{},
			samplesB: []model.SamplePair{},
			expected: []model.SamplePair{},
		},
		{
			samplesA: []model.SamplePair{sample1},
			samplesB: []model.SamplePair{},
			expected: []model.SamplePair{sample1},
		},
		{
			samplesA: []model.SamplePair{},
			samplesB: []model.SamplePair{sample1},
			expected: []model.SamplePair{sample1},
		},
		{
			samplesA: []model.SamplePair{sample1},
			samplesB: []model.SamplePair{sample1},
			expected: []model.SamplePair{sample1},
		},
		{
			samplesA: []model.SamplePair{sample1, sample2, sample3},
			samplesB: []model.SamplePair{sample1, sample3, sample4},
			expected: []model.SamplePair{sample1, sample2, sample3, sample4},
		},
	} {
		samples := MergeSampleSets(c.samplesA, c.samplesB)
		require.Equal(t, c.expected, samples)
	}
}

func TestMergeNSampleSets(t *testing.T) {
	now := model.Now()
	sample1 := model.SamplePair{Timestamp: now, Value: 1}
	sample2 := model.SamplePair{Timestamp: now.Add(1 * time.Second), Value: 2}
	sample3 := model.SamplePair{Timestamp: now.Add(4 * time.Second), Value: 3}
	sample4 := model.SamplePair{Timestamp: now.Add(8 * time.Second), Value: 7}

	for _, c := range []struct {
		sampleSets [][]model.SamplePair
		expected   []model.SamplePair
	}{
		{
			sampleSets: [][]model.SamplePair{{}, {}, {}},
			expected:   []model.SamplePair{},
		},
		{
			sampleSets: [][]model.SamplePair{
				{sample1, sample2},
				{sample2},
				{sample1, sample3, sample4},
			},
			expected: []model.SamplePair{sample1, sample2, sample3, sample4},
		},
	} {
		samples := MergeNSampleSets(c.sampleSets...)
		require.Equal(t, c.expected, samples)
	}
}
