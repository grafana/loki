package client

import (
	"net/url"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func TestNewLogger(t *testing.T) {
	_, err := NewLogger([]Config{}...)
	require.Error(t, err)

	l, err := NewLogger([]Config{{URL: flagext.URLValue{URL: &url.URL{Host: "string"}}}}...)
	require.NoError(t, err)
	err = l.Handle(model.LabelSet{"foo": "bar"}, time.Now(), "entry")
	require.NoError(t, err)
}
