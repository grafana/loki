package memberlist

import (
	"sync"
)

// This struct holds state of initialization of memberlist.KV instance.
type KVInit struct {
	// config used for initialization
	cfg *KVConfig

	// init function, to avoid multiple initializations.
	init sync.Once

	// state
	kv  *KV
	err error
}

func NewKVInit(cfg *KVConfig) *KVInit {
	return &KVInit{
		cfg: cfg,
	}
}

func (kvs *KVInit) GetMemberlistKV() (*KV, error) {
	kvs.init.Do(func() {
		kvs.kv, kvs.err = NewKV(*kvs.cfg)
	})

	return kvs.kv, kvs.err
}

func (kvs *KVInit) Stop() {
	if kvs.kv != nil {
		kvs.kv.Stop()
	}
}
