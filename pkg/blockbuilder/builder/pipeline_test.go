package builder

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

type testStage struct {
	parallelism int
	fn          func(context.Context) error
	cleanup     func(context.Context) error
}

func TestPipeline(t *testing.T) {
	tests := []struct {
		name        string
		stages      []testStage
		expectedErr error
	}{
		{
			name: "single stage success",
			stages: []testStage{
				{
					parallelism: 1,
					fn: func(_ context.Context) error {
						return nil
					},
				},
			},
		},
		{
			name: "multiple stages success",
			stages: []testStage{
				{
					parallelism: 2,
					fn: func(_ context.Context) error {
						return nil
					},
				},
				{
					parallelism: 1,
					fn: func(_ context.Context) error {
						return nil
					},
				},
			},
		},
		{
			name: "stage error propagates",
			stages: []testStage{
				{
					parallelism: 1,
					fn: func(_ context.Context) error {
						return errors.New("stage error")
					},
				},
			},
			expectedErr: errors.New("stage error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPipeline(context.Background())

			for i, stage := range tt.stages {
				if stage.cleanup != nil {
					p.AddStageWithCleanup(fmt.Sprint(i), stage.parallelism, stage.fn, stage.cleanup)
				} else {
					p.AddStage(fmt.Sprint(i), stage.parallelism, stage.fn)
				}
			}

			err := p.Run()
			if tt.expectedErr != nil {
				require.Error(t, err)
				require.Equal(t, tt.expectedErr.Error(), err.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}
