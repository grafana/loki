package testutils

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFailingObjectClient_PassesThrough(t *testing.T) {
	store := NewFailingObjectClient(NewInMemoryObjectClient())
	require.NoError(t, store.PutObject(context.Background(), "wanted", bytes.NewReader([]byte("chunk bytes"))))
	store.Fail(errors.New("boom"), "other")

	body, size, err := store.GetObject(context.Background(), "wanted")
	require.NoError(t, err)
	t.Cleanup(func() { _ = body.Close() })

	read, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, []byte("chunk bytes"), read)
	require.Equal(t, int64(len("chunk bytes")), size)
}

func TestFailingObjectClient_Fail(t *testing.T) {
	injected := errors.New("boom")

	store := NewFailingObjectClient(NewInMemoryObjectClient())
	require.NoError(t, store.PutObject(context.Background(), "doomed", bytes.NewReader([]byte("chunk bytes"))))
	store.Fail(injected, "doomed")

	body, size, err := store.GetObject(context.Background(), "doomed")
	require.ErrorIs(t, err, injected)
	require.Nil(t, body, "no bytes may be handed out alongside the error")
	require.Zero(t, size)
}

func TestFailingObjectClient_Truncate(t *testing.T) {
	store := NewFailingObjectClient(NewInMemoryObjectClient())
	require.NoError(t, store.PutObject(context.Background(), "cut", bytes.NewReader([]byte("0123456789"))))
	store.Truncate("cut", 4)

	// The failure being modelled happens after a successful GetObject, so it has
	// to arrive through the reader rather than as a returned error.
	body, _, err := store.GetObject(context.Background(), "cut")
	require.NoError(t, err)
	t.Cleanup(func() { _ = body.Close() })

	read, err := io.ReadAll(body)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	require.Equal(t, []byte("0123"), read)
}

func TestFailingObjectClient_BlockReleasesOnContext(t *testing.T) {
	store := NewFailingObjectClient(NewInMemoryObjectClient())
	require.NoError(t, store.PutObject(context.Background(), "held", bytes.NewReader([]byte("chunk bytes"))))
	release := store.Block("held")

	ctx, cancel := context.WithCancel(context.Background())
	returned := make(chan error, 1)
	go func() {
		_, _, err := store.GetObject(ctx, "held")
		returned <- err
	}()

	cancel()
	require.ErrorIs(t, <-returned, context.Canceled)

	// The channel stays open on purpose. A test that cancels must not also have
	// to release the block, or the cancellation is not what freed the call.
	select {
	case <-release:
		t.Fatal("block channel was closed")
	default:
	}
}
