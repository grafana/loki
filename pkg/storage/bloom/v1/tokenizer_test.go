package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	v2 "github.com/grafana/loki/v3/pkg/iter/v2"

	"github.com/grafana/loki/pkg/push"
)

func TestStructuredMetadataTokenizer(t *testing.T) {
	tokenizer := NewStructuredMetadataTokenizer("chunk")

	metadata := push.LabelAdapter{Name: "pod", Value: "loki-1"}
	expected := []string{"pod", "chunkpod", "pod=loki-1", "chunkpod=loki-1"}

	tokenIter := tokenizer.Tokens(metadata)
	got, err := v2.Collect(tokenIter)
	require.NoError(t, err)
	require.Equal(t, expected, got)
}
