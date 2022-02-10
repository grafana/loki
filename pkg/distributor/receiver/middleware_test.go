package receiver

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/model/pdata"
	"google.golang.org/grpc/metadata"
)

type assertFunc func(*testing.T, context.Context)

type testConsumer struct {
	t          *testing.T
	assertFunc assertFunc
}

func (tc *testConsumer) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: false}
}

func newAssertingConsumer(t *testing.T, assertFunc assertFunc) consumer.Logs {
	return &testConsumer{
		t:          t,
		assertFunc: assertFunc,
	}
}

func (tc *testConsumer) ConsumeLogs(ctx context.Context, td pdata.Logs) error {
	tc.assertFunc(tc.t, ctx)
	return nil
}

func TestFakeTenantMiddleware(t *testing.T) {
	m := FakeTenantMiddleware()

	t.Run("injects org id", func(t *testing.T) {
		consumer := newAssertingConsumer(t, func(t *testing.T, ctx context.Context) {
			orgID, err := user.ExtractOrgID(ctx)
			require.NoError(t, err)
			require.Equal(t, orgID, "fake")
		})

		ctx := context.Background()
		require.NoError(t, m.Wrap(consumer).ConsumeLogs(ctx, pdata.Logs{}))
	})
}

func TestMultiTenancyMiddleware(t *testing.T) {
	m := MultiTenancyMiddleware()

	t.Run("injects org id", func(t *testing.T) {
		tenantID := "test-tenant-id"

		consumer := newAssertingConsumer(t, func(t *testing.T, ctx context.Context) {
			orgID, err := user.ExtractOrgID(ctx)
			require.NoError(t, err)
			require.Equal(t, orgID, tenantID)
		})

		ctx := metadata.NewIncomingContext(
			context.Background(),
			metadata.Pairs("X-Scope-OrgID", tenantID),
		)
		require.NoError(t, m.Wrap(consumer).ConsumeLogs(ctx, pdata.Logs{}))
	})

	t.Run("returns error if org id cannot be extracted", func(t *testing.T) {
		// no need to assert anything, because the wrapped function is never called
		consumer := newAssertingConsumer(t, func(t *testing.T, ctx context.Context) {})
		ctx := context.Background()
		require.EqualError(t, m.Wrap(consumer).ConsumeLogs(ctx, pdata.Logs{}), "no org id")
	})
}
