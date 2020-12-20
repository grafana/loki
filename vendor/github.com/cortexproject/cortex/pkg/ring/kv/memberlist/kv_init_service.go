package memberlist

import (
	"context"
	"sync"

	"github.com/go-kit/kit/log"

	"github.com/cortexproject/cortex/pkg/util/services"
)

// This service initialized memberlist.KV on first call to GetMemberlistKV, and starts it. On stop,
// KV is stopped too. If KV fails, error is reported from the service.
type KVInitService struct {
	services.Service

	// config used for initialization
	cfg    *KVConfig
	logger log.Logger

	// init function, to avoid multiple initializations.
	init sync.Once

	// state
	kv      *KV
	err     error
	watcher *services.FailureWatcher
}

func NewKVInitService(cfg *KVConfig, logger log.Logger) *KVInitService {
	kvinit := &KVInitService{
		cfg:     cfg,
		watcher: services.NewFailureWatcher(),
		logger:  logger,
	}
	kvinit.Service = services.NewBasicService(nil, kvinit.running, kvinit.stopping)
	return kvinit
}

// This method will initialize Memberlist.KV on first call, and add it to service failure watcher.
func (kvs *KVInitService) GetMemberlistKV() (*KV, error) {
	kvs.init.Do(func() {
		kvs.kv = NewKV(*kvs.cfg, kvs.logger)
		kvs.watcher.WatchService(kvs.kv)
		kvs.err = kvs.kv.StartAsync(context.Background())
	})

	return kvs.kv, kvs.err
}

func (kvs *KVInitService) running(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return nil
	case err := <-kvs.watcher.Chan():
		// Only happens if KV service was actually initialized in GetMemberlistKV and it fails.
		return err
	}
}

func (kvs *KVInitService) stopping(_ error) error {
	if kvs.kv == nil {
		return nil
	}

	return services.StopAndAwaitTerminated(context.Background(), kvs.kv)
}
