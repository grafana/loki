package etcd

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"time"

	"github.com/cortexproject/cortex/pkg/util/flagext"

	"go.etcd.io/etcd/embed"
	"go.etcd.io/etcd/etcdserver/api/v3client"

	"github.com/cortexproject/cortex/pkg/ring/kv/codec"
)

const etcdStartTimeout = 30 * time.Second

// Mock returns a Mock Etcd client.
// Inspired by https://github.com/ligato/cn-infra/blob/master/db/keyval/etcd/mocks/embeded_etcd.go.
func Mock(codec codec.Codec) (*Client, io.Closer, error) {
	dir, err := ioutil.TempDir("", "etcd")
	if err != nil {
		return nil, nil, err
	}

	cfg := embed.NewConfig()
	cfg.Logger = "zap"
	cfg.Dir = dir
	lpurl, _ := url.Parse("http://localhost:0")
	lcurl, _ := url.Parse("http://localhost:0")
	cfg.LPUrls = []url.URL{*lpurl}
	cfg.LCUrls = []url.URL{*lcurl}

	etcd, err := embed.StartEtcd(cfg)
	if err != nil {
		return nil, nil, err
	}

	select {
	case <-etcd.Server.ReadyNotify():
	case <-time.After(etcdStartTimeout):
		etcd.Server.Stop() // trigger a shutdown
		return nil, nil, fmt.Errorf("server took too long to start")
	}

	closer := CloserFunc(func() error {
		etcd.Server.Stop()
		return nil
	})

	var config Config
	flagext.DefaultValues(&config)

	client := &Client{
		cfg:   config,
		codec: codec,
		cli:   v3client.New(etcd.Server),
	}

	return client, closer, nil
}

// CloserFunc is like http.HandlerFunc but for io.Closers.
type CloserFunc func() error

// Close implements io.Closer.
func (f CloserFunc) Close() error {
	return f()
}

// NopCloser does nothing.
var NopCloser = CloserFunc(func() error {
	return nil
})

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix(f, "")
}
