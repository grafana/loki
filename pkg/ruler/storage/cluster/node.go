package cluster

import (
	"context"
	"fmt"
	"hash/fnv"
	"net/http"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/ring/kv"
	cortex_util "github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/services"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gorilla/mux"
	pb "github.com/grafana/agent/pkg/agentproto"
	"github.com/grafana/agent/pkg/metrics/cluster/client"
	"github.com/grafana/agent/pkg/util"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaveworks/common/user"
)

const (
	// agentKey is the key used for storing the hash ring.
	agentKey = "agent"
)

var backoffConfig = cortex_util.BackoffConfig{
	MinBackoff: time.Second,
	MaxBackoff: 2 * time.Minute,
	MaxRetries: 10,
}

// node manages membership within a ring. when a node joins or leaves the ring,
// it will inform other nodes to reshard their workloads. After a node joins
// the ring, it will inform the local service to reshard.
type node struct {
	log log.Logger
	reg *util.Unregisterer
	srv pb.ScrapingServiceServer

	mut  sync.RWMutex
	cfg  Config
	ring *ring.Ring
	lc   *ring.Lifecycler

	exited bool
	reload chan struct{}
}

// newNode creates a new node and registers it to the ring.
func newNode(reg prometheus.Registerer, log log.Logger, cfg Config, s pb.ScrapingServiceServer) (*node, error) {
	n := &node{
		reg: util.WrapWithUnregisterer(reg),
		srv: s,
		log: log,

		reload: make(chan struct{}, 1),
	}
	if err := n.ApplyConfig(cfg); err != nil {
		return nil, err
	}
	go n.run()
	return n, nil
}

func (n *node) ApplyConfig(cfg Config) error {
	n.mut.Lock()
	defer n.mut.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Detect if the config changed.
	if util.CompareYAML(n.cfg, cfg) {
		return nil
	}

	if n.exited {
		return fmt.Errorf("node already exited")
	}

	level.Info(n.log).Log("msg", "applying config")

	// Shut down old components before re-creating the updated ones.
	n.reg.UnregisterAll()

	if n.lc != nil {
		// Note that this will call performClusterReshard and will block until it
		// completes.
		err := services.StopAndAwaitTerminated(ctx, n.lc)
		if err != nil {
			return fmt.Errorf("failed to stop lifecycler: %w", err)
		}
		n.lc = nil
	}

	if n.ring != nil {
		err := services.StopAndAwaitTerminated(ctx, n.ring)
		if err != nil {
			return fmt.Errorf("failed to stop ring: %w", err)
		}
		n.ring = nil
	}

	if !cfg.Enabled {
		n.cfg = cfg
		return nil
	}

	r, err := newRing(cfg.Lifecycler.RingConfig, "agent_viewer", agentKey, n.reg)
	if err != nil {
		return fmt.Errorf("failed to create ring: %w", err)
	}
	if err := n.reg.Register(r); err != nil {
		return fmt.Errorf("failed to register ring metrics: %w", err)
	}
	if err := services.StartAndAwaitRunning(context.Background(), r); err != nil {
		return fmt.Errorf("failed to start ring: %w", err)
	}
	n.ring = r

	lc, err := ring.NewLifecycler(cfg.Lifecycler, n, "agent", agentKey, false, n.reg)
	if err != nil {
		return fmt.Errorf("failed to create lifecycler: %w", err)
	}
	if err := services.StartAndAwaitRunning(context.Background(), lc); err != nil {
		if err := services.StopAndAwaitTerminated(ctx, r); err != nil {
			level.Error(n.log).Log("msg", "failed to stop ring when returning error. next config reload will fail", "err", err)
		}
		return fmt.Errorf("failed to start lifecycler: %w", err)
	}
	n.lc = lc

	n.cfg = cfg

	// Reload and reshard the cluster.
	n.reload <- struct{}{}
	return nil
}

// newRing creates a new Cortex Ring that ignores unhealthy nodes.
func newRing(cfg ring.Config, name, key string, reg prometheus.Registerer) (*ring.Ring, error) {
	codec := ring.GetCodec()
	store, err := kv.NewClient(
		cfg.KVStore,
		codec,
		kv.RegistererWithKVName(reg, name+"-ring"),
	)
	if err != nil {
		return nil, err
	}
	return ring.NewWithStoreClientAndStrategy(cfg, name, key, store, ring.NewIgnoreUnhealthyInstancesReplicationStrategy())
}

// run waits for connection to the ring and kickstarts the join process.
func (n *node) run() {
	for range n.reload {
		n.mut.RLock()

		if err := n.performClusterReshard(context.Background(), true); err != nil {
			level.Warn(n.log).Log("msg", "dynamic cluster reshard did not succeed", "err", err)
		}

		n.mut.RUnlock()
	}

	level.Info(n.log).Log("msg", "node run loop exiting")
}

// performClusterReshard informs the cluster to immediately trigger a reshard
// of their workloads. if joining is true, the server provided to newNode will
// also be informed.
func (n *node) performClusterReshard(ctx context.Context, joining bool) error {
	if n.ring == nil || n.lc == nil {
		level.Info(n.log).Log("msg", "node disabled, not resharding")
		return nil
	}

	if n.cfg.ClusterReshardEventTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, n.cfg.ClusterReshardEventTimeout)
		defer cancel()
	}

	var (
		rs  ring.ReplicationSet
		err error
	)

	backoff := cortex_util.NewBackoff(ctx, backoffConfig)
	for backoff.Ongoing() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		rs, err = n.ring.GetAllHealthy(ring.Read)
		if err == nil {
			break
		}
		backoff.Wait()
	}

	if len(rs.Instances) > 0 {
		level.Info(n.log).Log("msg", "informing remote nodes to reshard")
	}

	// These are not in the go routine below due to potential race condition with n.lc.addr
	_, err = rs.Do(ctx, 500*time.Millisecond, func(c context.Context, id *ring.InstanceDesc) (interface{}, error) {
		// Skip over ourselves.
		if id.Addr == n.lc.Addr {
			return nil, nil
		}

		notifyCtx := user.InjectOrgID(c, "fake")
		return nil, n.notifyReshard(notifyCtx, id)
	})

	if err != nil {
		level.Error(n.log).Log("msg", "notifying other nodes failed", "err", err)
	}

	if joining {
		level.Info(n.log).Log("msg", "running local reshard")
		if _, err := n.srv.Reshard(ctx, &pb.ReshardRequest{}); err != nil {
			level.Warn(n.log).Log("msg", "dynamic local reshard did not succeed", "err", err)
		}
	}
	return err
}

// notifyReshard informs an individual node to reshard.
func (n *node) notifyReshard(ctx context.Context, id *ring.InstanceDesc) error {
	cli, err := client.New(n.cfg.Client, id.Addr)
	if err != nil {
		return err
	}
	defer cli.Close()

	level.Info(n.log).Log("msg", "attempting to notify remote agent to reshard", "addr", id.Addr)

	backoff := cortex_util.NewBackoff(ctx, backoffConfig)
	for backoff.Ongoing() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, err := cli.Reshard(ctx, &pb.ReshardRequest{})
		if err == nil {
			break
		}

		level.Warn(n.log).Log("msg", "reshard notification attempt failed", "addr", id.Addr, "err", err, "attempt", backoff.NumRetries())
		backoff.Wait()
	}

	return backoff.Err()
}

// WaitJoined waits for the node the join the cluster and enter the
// ACTIVE state.
func (n *node) WaitJoined(ctx context.Context) error {
	n.mut.RLock()
	defer n.mut.RUnlock()

	level.Info(n.log).Log("msg", "waiting for the node to join the cluster")
	defer level.Info(n.log).Log("msg", "node has joined the cluster")

	if n.ring == nil || n.lc == nil {
		return fmt.Errorf("node disabled")
	}

	return waitJoined(ctx, agentKey, n.ring.KVClient, n.lc.ID)
}

func waitJoined(ctx context.Context, key string, kvClient kv.Client, id string) error {
	kvClient.WatchKey(ctx, key, func(value interface{}) bool {
		if value == nil {
			return true
		}

		desc := value.(*ring.Desc)
		for ingID, ing := range desc.Ingesters {
			if ingID == id && ing.State == ring.ACTIVE {
				return false
			}
		}

		return true
	})

	return ctx.Err()
}

func (n *node) WireAPI(r *mux.Router) {
	r.HandleFunc("/debug/ring", func(rw http.ResponseWriter, r *http.Request) {
		n.mut.RLock()
		defer n.mut.RUnlock()

		if n.ring == nil {
			http.NotFoundHandler().ServeHTTP(rw, r)
			return
		}

		n.ring.ServeHTTP(rw, r)
	})
}

// Stop stops the node and cancels it from running. The node cannot be used
// again once Stop is called.
func (n *node) Stop() error {
	n.mut.Lock()
	defer n.mut.Unlock()

	if n.exited {
		return fmt.Errorf("node already exited")
	}
	n.exited = true

	level.Info(n.log).Log("msg", "shutting down node")

	// Shut down dependencies. The lifecycler *MUST* be shut down first since n.ring is
	// used during the shutdown process to inform other nodes to reshard.
	//
	// Note that stopping the lifecycler will call performClusterReshard and will block
	// until it completes.
	var (
		firstError error
		deps       []services.Service
	)

	if n.lc != nil {
		deps = append(deps, n.lc)
	}
	if n.ring != nil {
		deps = append(deps, n.ring)
	}
	for _, dep := range deps {
		err := services.StopAndAwaitTerminated(context.Background(), dep)
		if err != nil && firstError == nil {
			firstError = err
		}
	}

	close(n.reload)
	level.Info(n.log).Log("msg", "node shut down")
	return firstError
}

// Flush implements ring.FlushTransferer. It's a no-op.
func (n *node) Flush() {}

// TransferOut implements ring.FlushTransferer. It connects to all other healthy agents and
// tells them to reshard. TransferOut should NOT be called manually unless the mutex is
// held.
func (n *node) TransferOut(ctx context.Context) error {
	return n.performClusterReshard(ctx, false)
}

// Owns checks to see if a key is owned by this node. owns will return
// an error if the ring is empty or if there aren't enough healthy nodes.
func (n *node) Owns(key string) (bool, error) {
	n.mut.RLock()
	defer n.mut.RUnlock()

	rs, err := n.ring.Get(keyHash(key), ring.Write, nil, nil, nil)
	if err != nil {
		return false, err
	}
	for _, r := range rs.Instances {
		if r.Addr == n.lc.Addr {
			return true, nil
		}
	}
	return false, nil
}

func keyHash(key string) uint32 {
	h := fnv.New32()
	_, _ = h.Write([]byte(key))
	return h.Sum32()
}
