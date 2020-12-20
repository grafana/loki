package client

import (
	context "context"
)

// SendQueryStream wraps the stream's Send() checking if the context is done
// before calling Send().
func SendQueryStream(s Ingester_QueryStreamServer, m *QueryStreamResponse) error {
	return sendWithContextErrChecking(s.Context(), func() error {
		return s.Send(m)
	})
}

// SendTimeSeriesChunk wraps the stream's Send() checking if the context is done
// before calling Send().
func SendTimeSeriesChunk(s Ingester_TransferChunksClient, m *TimeSeriesChunk) error {
	return sendWithContextErrChecking(s.Context(), func() error {
		return s.Send(m)
	})
}

func sendWithContextErrChecking(ctx context.Context, send func() error) error {
	// If the context has been canceled or its deadline exceeded, we should return it
	// instead of the cryptic error the Send() will return.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}

	if err := send(); err != nil {
		// Experimentally, we've seen the context switching to done after the Send()
		// has been  called, so here we do recheck the context in case of error.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}

		return err
	}

	return nil
}
