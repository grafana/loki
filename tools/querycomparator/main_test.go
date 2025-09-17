package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMain(t *testing.T) {
	t.Skip("TODO")
	bucket = "TODO" // TODO: set bucket name for local engine support
	orgID = "TODO"  // TODO: set org ID for querying remote instances

	start := time.Date(2025, 9, 16, 14, 0, 0, 0, time.UTC)
	end := start.Add(1 * time.Minute)
	query := "{level=\"ERROR\"}"
	host1 := "localhost:3101"
	host2 := "localhost:3102"
	limit := 5
	require.NoError(t, doDebug(start, end, query, host1, host2, limit))
	require.NoError(t, doMetastoreLookup(start, end, query))
	require.NoError(t, doExecuteLocally(query, start, end, limit))
}
