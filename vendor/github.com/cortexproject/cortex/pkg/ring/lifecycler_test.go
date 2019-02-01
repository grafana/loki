package ring

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/cortexproject/cortex/pkg/util/test"
)

type flushTransferer struct {
	lifecycler *Lifecycler
}

func (f *flushTransferer) StopIncomingRequests() {}
func (f *flushTransferer) Flush()                {}
func (f *flushTransferer) TransferOut(ctx context.Context) error {
	if err := f.lifecycler.ClaimTokensFor(ctx, "ing1"); err != nil {
		return err
	}
	return f.lifecycler.ChangeState(ctx, ACTIVE)
}

func TestRingNormaliseMigration(t *testing.T) {
	var ringConfig Config
	flagext.DefaultValues(&ringConfig)
	ringConfig.Mock = NewInMemoryKVClient()

	r, err := New(ringConfig)
	require.NoError(t, err)
	defer r.Stop()

	// Add an 'ingester' with denormalised tokens.
	var lifecyclerConfig1 LifecyclerConfig
	flagext.DefaultValues(&lifecyclerConfig1)
	lifecyclerConfig1.Addr = "0.0.0.0"
	lifecyclerConfig1.Port = 1
	lifecyclerConfig1.RingConfig = ringConfig
	lifecyclerConfig1.NumTokens = 1
	lifecyclerConfig1.ClaimOnRollout = true
	lifecyclerConfig1.ID = "ing1"

	ft := &flushTransferer{}
	l1, err := NewLifecycler(lifecyclerConfig1, ft)
	require.NoError(t, err)

	// Check this ingester joined, is active, and has one token.
	test.Poll(t, 1000*time.Millisecond, true, func() interface{} {
		d, err := r.KVClient.Get(context.Background(), ConsulKey)
		require.NoError(t, err)

		desc, ok := d.(*Desc)
		return ok &&
			len(desc.Ingesters) == 1 &&
			desc.Ingesters["ing1"].State == ACTIVE &&
			len(desc.Ingesters["ing1"].Tokens) == 0 &&
			len(desc.Tokens) == 1
	})

	token := l1.tokens[0]

	// Add a second ingester with normalised tokens.
	var lifecyclerConfig2 = lifecyclerConfig1
	lifecyclerConfig2.JoinAfter = 100 * time.Second
	lifecyclerConfig2.NormaliseTokens = true
	lifecyclerConfig2.ID = "ing2"

	l2, err := NewLifecycler(lifecyclerConfig2, &flushTransferer{})
	require.NoError(t, err)

	// This will block until l1 has successfully left the ring.
	ft.lifecycler = l2 // When l1 shutsdown, call l2.ClaimTokensFor("ing1")
	l1.Shutdown()

	// Check the new ingester joined, has the same token, and is active.
	test.Poll(t, 1000*time.Millisecond, true, func() interface{} {
		d, err := r.KVClient.Get(context.Background(), ConsulKey)
		require.NoError(t, err)

		desc, ok := d.(*Desc)
		return ok &&
			len(desc.Ingesters) == 1 &&
			desc.Ingesters["ing2"].State == ACTIVE &&
			len(desc.Ingesters["ing2"].Tokens) == 1 &&
			desc.Ingesters["ing2"].Tokens[0] == token &&
			len(desc.Tokens) == 0
	})
}
