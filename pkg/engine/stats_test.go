package engine

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
)

func TestResultErrorClass(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "nil", err: nil, want: ""},
		{name: "canceled", err: context.Canceled, want: "canceled"},
		{name: "wrapped canceled", err: fmt.Errorf("read batch: %w", context.Canceled), want: "canceled"},
		{name: "deadline exceeded", err: context.DeadlineExceeded, want: "deadline_exceeded"},
		{name: "wrapped deadline", err: fmt.Errorf("read batch: %w", context.DeadlineExceeded), want: "deadline_exceeded"},
		{name: "other", err: errors.New("boom"), want: "other"},
		// EOF should never reach the classifier (it is handled inline), but if
		// it ever did it must fall into the bounded "other" bucket.
		{name: "eof", err: executor.EOF, want: "other"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, resultErrorClass(tt.err))
		})
	}
}
