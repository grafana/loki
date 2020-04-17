package ring

import (
	"context"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
)

type LeaveOnStoppingDelegate struct {
	next   BasicLifecyclerDelegate
	logger log.Logger
}

func NewLeaveOnStoppingDelegate(next BasicLifecyclerDelegate, logger log.Logger) *LeaveOnStoppingDelegate {
	return &LeaveOnStoppingDelegate{
		next:   next,
		logger: logger,
	}
}

func (d *LeaveOnStoppingDelegate) OnRingInstanceRegister(lifecycler *BasicLifecycler, ringDesc Desc, instanceExists bool, instanceID string, instanceDesc IngesterDesc) (IngesterState, Tokens) {
	return d.next.OnRingInstanceRegister(lifecycler, ringDesc, instanceExists, instanceID, instanceDesc)
}

func (d *LeaveOnStoppingDelegate) OnRingInstanceTokens(lifecycler *BasicLifecycler, tokens Tokens) {
	d.next.OnRingInstanceTokens(lifecycler, tokens)
}

func (d *LeaveOnStoppingDelegate) OnRingInstanceStopping(lifecycler *BasicLifecycler) {
	if err := lifecycler.changeState(context.Background(), LEAVING); err != nil {
		level.Error(d.logger).Log("msg", "failed to change instance state to LEAVING in the ring", "err", err)
	}

	d.next.OnRingInstanceStopping(lifecycler)
}

type TokensPersistencyDelegate struct {
	next       BasicLifecyclerDelegate
	logger     log.Logger
	tokensPath string
	loadState  IngesterState
}

func NewTokensPersistencyDelegate(path string, state IngesterState, next BasicLifecyclerDelegate, logger log.Logger) *TokensPersistencyDelegate {
	return &TokensPersistencyDelegate{
		next:       next,
		logger:     logger,
		tokensPath: path,
		loadState:  state,
	}
}

func (d *TokensPersistencyDelegate) OnRingInstanceRegister(lifecycler *BasicLifecycler, ringDesc Desc, instanceExists bool, instanceID string, instanceDesc IngesterDesc) (IngesterState, Tokens) {
	// Skip if no path has been configured.
	if d.tokensPath == "" {
		level.Info(d.logger).Log("msg", "not loading tokens from file, tokens file path is empty")
		return d.next.OnRingInstanceRegister(lifecycler, ringDesc, instanceExists, instanceID, instanceDesc)
	}

	// Do not load tokens from disk if the instance is already in the ring and has some tokens.
	if instanceExists && len(instanceDesc.GetTokens()) > 0 {
		level.Info(d.logger).Log("msg", "not loading tokens from file, instance already in the ring")
		return d.next.OnRingInstanceRegister(lifecycler, ringDesc, instanceExists, instanceID, instanceDesc)
	}

	tokensFromFile, err := LoadTokensFromFile(d.tokensPath)
	if err != nil {
		level.Error(d.logger).Log("msg", "error in getting tokens from file", "err", err)
		return d.next.OnRingInstanceRegister(lifecycler, ringDesc, instanceExists, instanceID, instanceDesc)
	}

	// Signal the next delegate that the tokens have been loaded, miming the
	// case the instance exist in the ring (which is OK because the lifecycler
	// will correctly reconcile this case too).
	return d.next.OnRingInstanceRegister(lifecycler, ringDesc, true, lifecycler.GetInstanceID(), IngesterDesc{
		Addr:      lifecycler.GetInstanceAddr(),
		Timestamp: time.Now().Unix(),
		State:     d.loadState,
		Tokens:    tokensFromFile,
		Zone:      lifecycler.GetInstanceZone(),
	})
}

func (d *TokensPersistencyDelegate) OnRingInstanceTokens(lifecycler *BasicLifecycler, tokens Tokens) {
	if d.tokensPath != "" {
		if err := tokens.StoreToFile(d.tokensPath); err != nil {
			level.Error(d.logger).Log("msg", "error storing tokens to disk", "path", d.tokensPath, "err", err)
		}
	}

	d.next.OnRingInstanceTokens(lifecycler, tokens)
}

func (d *TokensPersistencyDelegate) OnRingInstanceStopping(lifecycler *BasicLifecycler) {
	d.next.OnRingInstanceStopping(lifecycler)
}
