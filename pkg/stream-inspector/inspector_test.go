package stream_inspector

import (
	"encoding/json"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"reflect"
	"testing"
)

func Test_Inspector(t *testing.T) {
	tests := map[string]struct {
		streams            []StreamWithVolume
		expectedResultJSON string
	}{
		"expected 2 threes": {
			streams: []StreamWithVolume{
				makeStream("cl", "cluster-a", "ns", "loki-ops"),
				makeStream("cl", "cluster-a", "ns", "loki-dev"),
				makeStream("cl", "cluster-a", "ns", "loki-dev", "level", "error"),
				makeStream("stack-cl", "cluster-b", "stack-ns", "loki-dev"),
				makeStream("stack-cl", "cluster-b", "stack-ns", "loki-ops"),
				makeStream("stack-cl", "cluster-b", "stack-ns", "loki-prod"),
			},
			expectedResultJSON: `[
  {
    "root": {
      "name": "cl",
      "weight": 3,
      "children": [
        {
          "name": "cluster-a",
          "weight": 3,
          "children": [
            {
              "name": "ns",
              "weight": 3,
              "children": [
                {
                  "name": "loki-ops",
                  "weight": 1
                },
                {
                  "name": "loki-dev",
                  "weight": 2,
                  "children": [
                    {
                      "name": "level",
                      "weight": 1,
                      "children": [
                        {
                          "name": "error",
                          "weight": 1
                        }
                      ]
                    }
                  ]
                }
              ]
            }
          ]
        }
      ]
    }
  },
  {
    "root": {
      "name": "stack-cl",
      "weight": 3,
      "children": [
        {
          "name": "cluster-b",
          "weight": 3,
          "children": [
            {
              "name": "stack-ns",
              "weight": 3,
              "children": [
                {
                  "name": "loki-dev",
                  "weight": 1
                },
                {
                  "name": "loki-ops",
                  "weight": 1
                },
                {
                  "name": "loki-prod",
                  "weight": 1
                }
              ]
            }
          ]
        }
      ]
    }
  }
]
`,
		},
	}
	for name, testData := range tests {
		t.Run(name, func(t *testing.T) {
			inspector := Inspector{}
			forest, err := inspector.BuildTrees(testData.streams, nil)
			require.NoError(t, err)
			actualJson, err := json.Marshal(forest)
			require.NoError(t, err)
			require.JSONEq(t, testData.expectedResultJSON, string(actualJson))
		})
	}
}

func Test_Inspector_sortLabelNames(t *testing.T) {
	inspector := Inspector{}
	result := inspector.sortLabelNamesByPopularity([]StreamWithVolume{
		makeStream("cl", "cluster-a", "ns", "loki-ops"),
		makeStream("cl", "cluster-a", "ns", "loki-dev"),
		makeStream("stack-cl", "cluster-b", "stack-ns", "loki-dev"),
		makeStream("stack-cl", "cluster-b", "stack-ns", "loki-ops"),
		makeStream("stack-cl", "cluster-b", "stack-ns", "loki-prod"),
	}, nil)
	require.Equal(t, map[string]int{
		"cl": 2, "ns": 3, "stack-cl": 0, "stack-ns": 1,
	}, result,
		"must be sorted by streams count in descending order and after this by label name in ascending order")
}

func makeStream(labelValues ...string) StreamWithVolume {
	var createdLabels labels.Labels
	for i := 0; i < len(labelValues); i += 2 {
		createdLabels = append(createdLabels, labels.Label{
			Name:  labelValues[i],
			Value: labelValues[i+1],
		})
	}
	return StreamWithVolume{
		Labels: createdLabels,
		Volume: 1,
	}
}

// compareThree function to compare provided threes to check if they are equal
func compareThree(a, b Tree) bool {
	return reflect.DeepEqual(a, b)
}
