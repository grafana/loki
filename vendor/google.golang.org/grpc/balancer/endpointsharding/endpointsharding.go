/*
 *
 * Copyright 2024 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

// Package endpointsharding implements a load balancing policy that manages
// homogeneous child policies each owning a single endpoint.
//
// # Experimental
//
// Notice: This package is EXPERIMENTAL and may be changed or removed in a
// later release.
package endpointsharding

import (
	"encoding/json"
	"errors"
	"fmt"
	rand "math/rand/v2"
	"sync"
	"sync/atomic"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/balancer/pickfirst"
	"google.golang.org/grpc/balancer/pickfirst/pickfirstleaf"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/balancer/gracefulswitch"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

// PickFirstConfig is a pick first config without shuffling enabled.
var PickFirstConfig string

func init() {
	name := pickfirst.Name
	if !envconfig.NewPickFirstEnabled {
		name = pickfirstleaf.Name
	}
	PickFirstConfig = fmt.Sprintf("[{%q: {}}]", name)
}

// ChildState is the balancer state of a child along with the endpoint which
// identifies the child balancer.
type ChildState struct {
	Endpoint resolver.Endpoint
	State    balancer.State
}

// NewBalancer returns a load balancing policy that manages homogeneous child
// policies each owning a single endpoint.
func NewBalancer(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	es := &endpointSharding{
		cc:    cc,
		bOpts: opts,
	}
	es.children.Store(resolver.NewEndpointMap())
	return es
}

// endpointSharding is a balancer that wraps child balancers. It creates a child
// balancer with child config for every unique Endpoint received. It updates the
// child states on any update from parent or child.
type endpointSharding struct {
	cc    balancer.ClientConn
	bOpts balancer.BuildOptions

	childMu  sync.Mutex // syncs balancer.Balancer calls into children
	children atomic.Pointer[resolver.EndpointMap]
	closed   bool

	// inhibitChildUpdates is set during UpdateClientConnState/ResolverError
	// calls (calls to children will each produce an update, only want one
	// update).
	inhibitChildUpdates atomic.Bool

	mu sync.Mutex // Sync updateState callouts and childState recent state updates
}

// UpdateClientConnState creates a child for new endpoints and deletes children
// for endpoints that are no longer present. It also updates all the children,
// and sends a single synchronous update of the childrens' aggregated state at
// the end of the UpdateClientConnState operation. If any endpoint has no
// addresses it will ignore that endpoint. Otherwise, returns first error found
// from a child, but fully processes the new update.
func (es *endpointSharding) UpdateClientConnState(state balancer.ClientConnState) error {
	es.childMu.Lock()
	defer es.childMu.Unlock()

	es.inhibitChildUpdates.Store(true)
	defer func() {
		es.inhibitChildUpdates.Store(false)
		es.updateState()
	}()
	var ret error

	children := es.children.Load()
	newChildren := resolver.NewEndpointMap()

	// Update/Create new children.
	for _, endpoint := range state.ResolverState.Endpoints {
		if _, ok := newChildren.Get(endpoint); ok {
			// Endpoint child was already created, continue to avoid duplicate
			// update.
			continue
		}
		var bal *balancerWrapper
		if child, ok := children.Get(endpoint); ok {
			bal = child.(*balancerWrapper)
		} else {
			bal = &balancerWrapper{
				childState: ChildState{Endpoint: endpoint},
				ClientConn: es.cc,
				es:         es,
			}
			bal.Balancer = gracefulswitch.NewBalancer(bal, es.bOpts)
		}
		newChildren.Set(endpoint, bal)
		if err := bal.UpdateClientConnState(balancer.ClientConnState{
			BalancerConfig: state.BalancerConfig,
			ResolverState: resolver.State{
				Endpoints:  []resolver.Endpoint{endpoint},
				Attributes: state.ResolverState.Attributes,
			},
		}); err != nil && ret == nil {
			// Return first error found, and always commit full processing of
			// updating children. If desired to process more specific errors
			// across all endpoints, caller should make these specific
			// validations, this is a current limitation for simplicity sake.
			ret = err
		}
	}
	// Delete old children that are no longer present.
	for _, e := range children.Keys() {
		child, _ := children.Get(e)
		bal := child.(balancer.Balancer)
		if _, ok := newChildren.Get(e); !ok {
			bal.Close()
		}
	}
	es.children.Store(newChildren)
	if newChildren.Len() == 0 {
		return balancer.ErrBadResolverState
	}
	return ret
}

// ResolverError forwards the resolver error to all of the endpointSharding's
// children and sends a single synchronous update of the childStates at the end
// of the ResolverError operation.
func (es *endpointSharding) ResolverError(err error) {
	es.childMu.Lock()
	defer es.childMu.Unlock()
	es.inhibitChildUpdates.Store(true)
	defer func() {
		es.inhibitChildUpdates.Store(false)
		es.updateState()
	}()
	children := es.children.Load()
	for _, child := range children.Values() {
		bal := child.(balancer.Balancer)
		bal.ResolverError(err)
	}
}

func (es *endpointSharding) UpdateSubConnState(balancer.SubConn, balancer.SubConnState) {
	// UpdateSubConnState is deprecated.
}

func (es *endpointSharding) Close() {
	es.childMu.Lock()
	defer es.childMu.Unlock()
	children := es.children.Load()
	for _, child := range children.Values() {
		bal := child.(balancer.Balancer)
		bal.Close()
	}
	es.closed = true
}

// updateState updates this component's state. It sends the aggregated state,
// and a picker with round robin behavior with all the child states present if
// needed.
func (es *endpointSharding) updateState() {
	if es.inhibitChildUpdates.Load() {
		return
	}
	var readyPickers, connectingPickers, idlePickers, transientFailurePickers []balancer.Picker

	es.mu.Lock()
	defer es.mu.Unlock()

	children := es.children.Load()
	childStates := make([]ChildState, 0, children.Len())

	for _, child := range children.Values() {
		bw := child.(*balancerWrapper)
		childState := bw.childState
		childStates = append(childStates, childState)
		childPicker := childState.State.Picker
		switch childState.State.ConnectivityState {
		case connectivity.Ready:
			readyPickers = append(readyPickers, childPicker)
		case connectivity.Connecting:
			connectingPickers = append(connectingPickers, childPicker)
		case connectivity.Idle:
			idlePickers = append(idlePickers, childPicker)
		case connectivity.TransientFailure:
			transientFailurePickers = append(transientFailurePickers, childPicker)
			// connectivity.Shutdown shouldn't appear.
		}
	}

	// Construct the round robin picker based off the aggregated state. Whatever
	// the aggregated state, use the pickers present that are currently in that
	// state only.
	var aggState connectivity.State
	var pickers []balancer.Picker
	if len(readyPickers) >= 1 {
		aggState = connectivity.Ready
		pickers = readyPickers
	} else if len(connectingPickers) >= 1 {
		aggState = connectivity.Connecting
		pickers = connectingPickers
	} else if len(idlePickers) >= 1 {
		aggState = connectivity.Idle
		pickers = idlePickers
	} else if len(transientFailurePickers) >= 1 {
		aggState = connectivity.TransientFailure
		pickers = transientFailurePickers
	} else {
		aggState = connectivity.TransientFailure
		pickers = []balancer.Picker{base.NewErrPicker(errors.New("no children to pick from"))}
	} // No children (resolver error before valid update).
	p := &pickerWithChildStates{
		pickers:     pickers,
		childStates: childStates,
		next:        uint32(rand.IntN(len(pickers))),
	}
	es.cc.UpdateState(balancer.State{
		ConnectivityState: aggState,
		Picker:            p,
	})
}

// pickerWithChildStates delegates to the pickers it holds in a round robin
// fashion. It also contains the childStates of all the endpointSharding's
// children.
type pickerWithChildStates struct {
	pickers     []balancer.Picker
	childStates []ChildState
	next        uint32
}

func (p *pickerWithChildStates) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	nextIndex := atomic.AddUint32(&p.next, 1)
	picker := p.pickers[nextIndex%uint32(len(p.pickers))]
	return picker.Pick(info)
}

// ChildStatesFromPicker returns the state of all the children managed by the
// endpoint sharding balancer that created this picker.
func ChildStatesFromPicker(picker balancer.Picker) []ChildState {
	p, ok := picker.(*pickerWithChildStates)
	if !ok {
		return nil
	}
	return p.childStates
}

// balancerWrapper is a wrapper of a balancer. It ID's a child balancer by
// endpoint, and persists recent child balancer state.
type balancerWrapper struct {
	balancer.Balancer   // Simply forward balancer.Balancer operations.
	balancer.ClientConn // embed to intercept UpdateState, doesn't deal with SubConns

	es *endpointSharding

	childState ChildState
}

func (bw *balancerWrapper) UpdateState(state balancer.State) {
	bw.es.mu.Lock()
	bw.childState.State = state
	bw.es.mu.Unlock()
	// When a child balancer says it's IDLE, ping it to exit idle and reconnect.
	// TODO: In the future, perhaps make this a knob in configuration.
	if ei, ok := bw.Balancer.(balancer.ExitIdler); state.ConnectivityState == connectivity.Idle && ok {
		go func() {
			bw.es.childMu.Lock()
			if !bw.es.closed {
				ei.ExitIdle()
			}
			bw.es.childMu.Unlock()
		}()
	}
	bw.es.updateState()
}

// ParseConfig parses a child config list and returns an LB config to use with
// the endpointsharding balancer.
//
// cfg is expected to be a JSON array of LB policy names + configs as the
// format of the loadBalancingConfig field in ServiceConfig.
func ParseConfig(cfg json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	return gracefulswitch.ParseConfig(cfg)
}
