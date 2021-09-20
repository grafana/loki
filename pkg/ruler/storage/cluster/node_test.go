// This directory was copied and adapted from https://github.com/grafana/agent/tree/main/pkg/metrics.
// We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.
// NOTE: many changes have been made to the original code for our use-case.
package cluster

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/util/services"
	"github.com/go-kit/kit/log/level"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/grafana/agent/pkg/agentproto"
	"github.com/grafana/agent/pkg/util"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"google.golang.org/grpc"
	"gopkg.in/yaml.v2"
)

func Test_node_Join(t *testing.T) {
	var (
		reg    = prometheus.NewRegistry()
		logger = util.TestLogger(t)

		localReshard  = make(chan struct{}, 2)
		remoteReshard = make(chan struct{}, 2)
	)

	local := &agentproto.FuncScrapingServiceServer{
		ReshardFunc: func(c context.Context, rr *agentproto.ReshardRequest) (*empty.Empty, error) {
			localReshard <- struct{}{}
			return &empty.Empty{}, nil
		},
	}

	remote := &agentproto.FuncScrapingServiceServer{
		ReshardFunc: func(c context.Context, rr *agentproto.ReshardRequest) (*empty.Empty, error) {
			remoteReshard <- struct{}{}
			return &empty.Empty{}, nil
		},
	}
	startNode(t, remote)

	nodeConfig := DefaultConfig
	nodeConfig.Enabled = true
	nodeConfig.Lifecycler = testLifecyclerConfig(t)

	n, err := newNode(reg, logger, nodeConfig, local)
	require.NoError(t, err)
	t.Cleanup(func() { _ = n.Stop() })

	require.NoError(t, n.WaitJoined(context.Background()))

	waitAll(t, remoteReshard, localReshard)
}

// waitAll waits for a message on all channels.
func waitAll(t *testing.T, chs ...chan struct{}) {
	timeoutCh := time.After(5 * time.Second)
	for _, ch := range chs {
		select {
		case <-timeoutCh:
			require.FailNow(t, "timeout exceeded")
		case <-ch:
		}
	}
}

func Test_node_Leave(t *testing.T) {
	var (
		reg    = prometheus.NewRegistry()
		logger = util.TestLogger(t)

		sendReshard   = atomic.NewBool(false)
		remoteReshard = make(chan struct{}, 2)
	)

	local := &agentproto.FuncScrapingServiceServer{
		ReshardFunc: func(c context.Context, rr *agentproto.ReshardRequest) (*empty.Empty, error) {
			return &empty.Empty{}, nil
		},
	}

	remote := &agentproto.FuncScrapingServiceServer{
		ReshardFunc: func(c context.Context, rr *agentproto.ReshardRequest) (*empty.Empty, error) {
			if sendReshard.Load() {
				remoteReshard <- struct{}{}
			}
			return &empty.Empty{}, nil
		},
	}
	startNode(t, remote)

	nodeConfig := DefaultConfig
	nodeConfig.Enabled = true
	nodeConfig.Lifecycler = testLifecyclerConfig(t)

	n, err := newNode(reg, logger, nodeConfig, local)
	require.NoError(t, err)
	require.NoError(t, n.WaitJoined(context.Background()))

	// Update the reshard function to write to remoteReshard on shutdown.
	sendReshard.Store(true)

	// Stop the node so it transfers data outward.
	require.NoError(t, n.Stop(), "failed to stop the node")

	level.Info(logger).Log("msg", "waiting for remote reshard to occur")
	waitAll(t, remoteReshard)
}

func Test_node_ApplyConfig(t *testing.T) {
	var (
		reg    = prometheus.NewRegistry()
		logger = util.TestLogger(t)

		localReshard = make(chan struct{}, 10)
	)

	local := &agentproto.FuncScrapingServiceServer{
		ReshardFunc: func(c context.Context, rr *agentproto.ReshardRequest) (*empty.Empty, error) {
			localReshard <- struct{}{}
			return &empty.Empty{}, nil
		},
	}

	nodeConfig := DefaultConfig
	nodeConfig.Enabled = true
	nodeConfig.Lifecycler = testLifecyclerConfig(t)

	n, err := newNode(reg, logger, nodeConfig, local)
	require.NoError(t, err)
	t.Cleanup(func() { _ = n.Stop() })
	require.NoError(t, n.WaitJoined(context.Background()))

	// Wait for the initial join to trigger.
	waitAll(t, localReshard)

	// An ApplyConfig working correctly should re-join the cluster, which can be
	// detected by local resharding applying twice.
	nodeConfig.Lifecycler.NumTokens = 1
	require.NoError(t, n.ApplyConfig(nodeConfig), "failed to apply new config")
	require.NoError(t, n.WaitJoined(context.Background()))

	waitAll(t, localReshard)
}

// startNode launches srv as a gRPC server and registers it to the ring.
func startNode(t *testing.T, srv agentproto.ScrapingServiceServer) {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	grpcServer := grpc.NewServer()
	agentproto.RegisterScrapingServiceServer(grpcServer, srv)

	go func() {
		_ = grpcServer.Serve(l)
	}()
	t.Cleanup(func() { grpcServer.Stop() })

	lcConfig := testLifecyclerConfig(t)
	lcConfig.Addr = l.Addr().(*net.TCPAddr).IP.String()
	lcConfig.Port = l.Addr().(*net.TCPAddr).Port

	lc, err := ring.NewLifecycler(lcConfig, ring.NewNoopFlushTransferer(), "agent", "agent", false, prometheus.NewRegistry())
	require.NoError(t, err)

	err = services.StartAndAwaitRunning(context.Background(), lc)
	require.NoError(t, err)

	// Wait for the new node to be in the ring.
	joinWaitCtx, joinWaitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer joinWaitCancel()
	err = waitJoined(joinWaitCtx, agentKey, lc.KVStore, lc.ID)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = services.StopAndAwaitTerminated(context.Background(), lc)
	})
}

func testLifecyclerConfig(t *testing.T) ring.LifecyclerConfig {
	t.Helper()

	cfgText := util.Untab(fmt.Sprintf(`
ring:
	kvstore:
		store: inmemory
		prefix: tests/%s
final_sleep: 0s
min_ready_duration: 0s
	`, t.Name()))

	// Apply default values by registering to a fake flag set.
	var lc ring.LifecyclerConfig
	lc.RegisterFlagsWithPrefix("", flag.NewFlagSet("", flag.ContinueOnError))

	err := yaml.Unmarshal([]byte(cfgText), &lc)
	require.NoError(t, err)

	// Assign a random default ID.
	var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	name := make([]rune, 10)
	for i := range name {
		name[i] = letters[rand.Intn(len(letters))]
	}
	lc.ID = string(name)

	// Add an invalid default address/port. Tests can override if they expect
	// incoming traffic.
	lc.Addr = "x.x.x.x"
	lc.Port = -1

	return lc
}
