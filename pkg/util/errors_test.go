package util //nolint:revive

import (
	"bytes"
	"context"
	"errors"
	"testing"

	gokitlog "github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/util/log"
)

func TestLogErrorWithContext(t *testing.T) {
	var buf bytes.Buffer
	defer func(l gokitlog.Logger) { log.Logger = l }(log.Logger)
	log.Logger = gokitlog.NewLogfmtLogger(&buf)

	closeErr := func() error { return errors.New("close failed") }

	LogErrorWithContext(context.Background(), "closing iterator", closeErr)
	require.Contains(t, buf.String(), "level=error")

	buf.Reset()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	LogErrorWithContext(ctx, "closing iterator", closeErr)
	require.Contains(t, buf.String(), "level=debug")
	require.NotContains(t, buf.String(), "level=error")
}
