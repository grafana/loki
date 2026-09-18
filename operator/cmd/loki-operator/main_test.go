package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
)

func TestWaitForFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "availability-zone")
	require.NoError(t, os.WriteFile(path, nil, 0o600))

	writeResult := make(chan error, 1)
	go func() {
		time.Sleep(10 * time.Millisecond)
		writeResult <- os.WriteFile(path, []byte("zone-a"), 0o600)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	require.NoError(t, waitForFile(ctx, path, time.Millisecond, logr.Discard()))
	require.NoError(t, <-writeResult)
}
