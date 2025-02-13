/*
 *
 * Copyright 2022 gRPC authors.
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

package xdsclient

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/transport"
	"google.golang.org/grpc/xds/internal/xdsclient/transport/ads"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
)

var _ XDSClient = &clientImpl{}

// ErrClientClosed is returned when the xDS client is closed.
var ErrClientClosed = errors.New("xds: the xDS client is closed")

// clientImpl is the real implementation of the xds client. The exported Client
// is a wrapper of this struct with a ref count.
type clientImpl struct {
	// The following fields are initialized at creation time and are read-only
	// after that, and therefore can be accessed without a mutex.
	done               *grpcsync.Event              // Fired when the client is closed.
	topLevelAuthority  *authority                   // The top-level authority, used only for old-style names without an authority.
	authorities        map[string]*authority        // Map from authority names in bootstrap to authority struct.
	config             *bootstrap.Config            // Complete bootstrap configuration.
	watchExpiryTimeout time.Duration                // Expiry timeout for ADS watch.
	backoff            func(int) time.Duration      // Backoff for ADS and LRS stream failures.
	transportBuilder   transport.Builder            // Builder to create transports to xDS server.
	resourceTypes      *resourceTypeRegistry        // Registry of resource types, for parsing incoming ADS responses.
	serializer         *grpcsync.CallbackSerializer // Serializer for invoking resource watcher callbacks.
	serializerClose    func()                       // Function to close the serializer.
	logger             *grpclog.PrefixLogger        // Logger for this client.

	// The clientImpl owns a bunch of channels to individual xDS servers
	// specified in the bootstrap configuration. Authorities acquire references
	// to these channels based on server configs within the authority config.
	// The clientImpl maintains a list of interested authorities for each of
	// these channels, and forwards updates from the channels to each of these
	// authorities.
	//
	// Once all references to a channel are dropped, the channel is closed.
	channelsMu        sync.Mutex
	xdsActiveChannels map[string]*channelState // Map from server config to in-use xdsChannels.
}

// channelState represents the state of an xDS channel. It tracks the number of
// LRS references, the authorities interested in the channel, and the server
// configuration used for the channel.
//
// It receives callbacks for events on the underlying ADS stream and invokes
// corresponding callbacks on interested authoririties.
type channelState struct {
	parent       *clientImpl
	serverConfig *bootstrap.ServerConfig

	// Access to the following fields should be protected by the parent's
	// channelsMu.
	channel               *xdsChannel
	lrsRefs               int
	interestedAuthorities map[*authority]bool
}

func (cs *channelState) adsStreamFailure(err error) {
	if cs.parent.done.HasFired() {
		return
	}

	cs.parent.channelsMu.Lock()
	defer cs.parent.channelsMu.Unlock()
	for authority := range cs.interestedAuthorities {
		authority.adsStreamFailure(cs.serverConfig, err)
	}
}

func (cs *channelState) adsResourceUpdate(typ xdsresource.Type, updates map[string]ads.DataAndErrTuple, md xdsresource.UpdateMetadata, onDone func()) {
	if cs.parent.done.HasFired() {
		return
	}

	cs.parent.channelsMu.Lock()
	defer cs.parent.channelsMu.Unlock()

	if len(cs.interestedAuthorities) == 0 {
		onDone()
		return
	}

	authorityCnt := new(atomic.Int64)
	authorityCnt.Add(int64(len(cs.interestedAuthorities)))
	done := func() {
		if authorityCnt.Add(-1) == 0 {
			onDone()
		}
	}
	for authority := range cs.interestedAuthorities {
		authority.adsResourceUpdate(cs.serverConfig, typ, updates, md, done)
	}
}

func (cs *channelState) adsResourceDoesNotExist(typ xdsresource.Type, resourceName string) {
	if cs.parent.done.HasFired() {
		return
	}

	cs.parent.channelsMu.Lock()
	defer cs.parent.channelsMu.Unlock()
	for authority := range cs.interestedAuthorities {
		authority.adsResourceDoesNotExist(typ, resourceName)
	}
}

// BootstrapConfig returns the configuration read from the bootstrap file.
// Callers must treat the return value as read-only.
func (c *clientImpl) BootstrapConfig() *bootstrap.Config {
	return c.config
}

// close closes the xDS client and releases all resources.
func (c *clientImpl) close() {
	if c.done.HasFired() {
		return
	}
	c.done.Fire()

	c.topLevelAuthority.close()
	for _, a := range c.authorities {
		a.close()
	}

	// Channel close cannot be invoked with the lock held, because it can race
	// with stream failure happening at the same time. The latter will callback
	// into the clientImpl and will attempt to grab the lock. This will result
	// in a deadlock. So instead, we release the lock and wait for all active
	// channels to be closed.
	var channelsToClose []*xdsChannel
	c.channelsMu.Lock()
	for _, cs := range c.xdsActiveChannels {
		channelsToClose = append(channelsToClose, cs.channel)
	}
	c.xdsActiveChannels = nil
	c.channelsMu.Unlock()
	for _, c := range channelsToClose {
		c.close()
	}

	c.serializerClose()
	<-c.serializer.Done()

	for _, s := range c.config.XDSServers() {
		for _, f := range s.Cleanups() {
			f()
		}
	}
	for _, a := range c.config.Authorities() {
		for _, s := range a.XDSServers {
			for _, f := range s.Cleanups() {
				f()
			}
		}
	}
	c.logger.Infof("Shutdown")
}

// getChannelForADS returns an xdsChannel for the given server configuration.
//
// If an xdsChannel exists for the given server configuration, it is returned.
// Else a new one is created. It also ensures that the calling authority is
// added to the set of interested authorities for the returned channel.
//
// It returns the xdsChannel and a function to release the calling authority's
// reference on the channel. The caller must call the cancel function when it is
// no longer interested in this channel.
//
// A non-nil error is returned if an xdsChannel was not created.
func (c *clientImpl) getChannelForADS(serverConfig *bootstrap.ServerConfig, callingAuthority *authority) (*xdsChannel, func(), error) {
	if c.done.HasFired() {
		return nil, nil, ErrClientClosed
	}

	initLocked := func(s *channelState) {
		if c.logger.V(2) {
			c.logger.Infof("Adding authority %q to the set of interested authorities for channel [%p]", callingAuthority.name, s.channel)
		}
		s.interestedAuthorities[callingAuthority] = true
	}
	deInitLocked := func(s *channelState) {
		if c.logger.V(2) {
			c.logger.Infof("Removing authority %q from the set of interested authorities for channel [%p]", callingAuthority.name, s.channel)
		}
		delete(s.interestedAuthorities, callingAuthority)
	}

	return c.getOrCreateChannel(serverConfig, initLocked, deInitLocked)
}

// getChannelForLRS returns an xdsChannel for the given server configuration.
//
// If an xdsChannel exists for the given server configuration, it is returned.
// Else a new one is created. A reference count that tracks the number of LRS
// calls on the returned channel is incremented before returning the channel.
//
// It returns the xdsChannel and a function to decrement the reference count
// that tracks the number of LRS calls on the returned channel. The caller must
// call the cancel function when it is no longer interested in this channel.
//
// A non-nil error is returned if an xdsChannel was not created.
func (c *clientImpl) getChannelForLRS(serverConfig *bootstrap.ServerConfig) (*xdsChannel, func(), error) {
	if c.done.HasFired() {
		return nil, nil, ErrClientClosed
	}

	initLocked := func(s *channelState) { s.lrsRefs++ }
	deInitLocked := func(s *channelState) { s.lrsRefs-- }

	return c.getOrCreateChannel(serverConfig, initLocked, deInitLocked)
}

// getOrCreateChannel returns an xdsChannel for the given server configuration.
//
// If an active xdsChannel exists for the given server configuration, it is
// returned. If an idle xdsChannel exists for the given server configuration, it
// is revived from the idle cache and returned. Else a new one is created.
//
// The initLocked function runs some initialization logic before the channel is
// returned. This includes adding the calling authority to the set of interested
// authorities for the channel or incrementing the count of the number of LRS
// calls on the channel.
//
// The deInitLocked function runs some cleanup logic when the returned cleanup
// function is called. This involves removing the calling authority from the set
// of interested authorities for the channel or decrementing the count of the
// number of LRS calls on the channel.
//
// Both initLocked and deInitLocked are called with the c.channelsMu held.
//
// Returns the xdsChannel and a cleanup function to be invoked when the channel
// is no longer required. A non-nil error is returned if an xdsChannel was not
// created.
func (c *clientImpl) getOrCreateChannel(serverConfig *bootstrap.ServerConfig, initLocked, deInitLocked func(*channelState)) (*xdsChannel, func(), error) {
	c.channelsMu.Lock()
	defer c.channelsMu.Unlock()

	if c.logger.V(2) {
		c.logger.Infof("Received request for a reference to an xdsChannel for server config %q", serverConfig)
	}

	// Use an existing channel, if one exists for this server config.
	if state, ok := c.xdsActiveChannels[serverConfig.String()]; ok {
		if c.logger.V(2) {
			c.logger.Infof("Reusing an existing xdsChannel for server config %q", serverConfig)
		}
		initLocked(state)
		return state.channel, c.releaseChannel(serverConfig, state, deInitLocked), nil
	}

	if c.logger.V(2) {
		c.logger.Infof("Creating a new xdsChannel for server config %q", serverConfig)
	}

	// Create a new transport and create a new xdsChannel, and add it to the
	// map of xdsChannels.
	tr, err := c.transportBuilder.Build(transport.BuildOptions{ServerConfig: serverConfig})
	if err != nil {
		return nil, func() {}, fmt.Errorf("failed to create transport for server config %s: %v", serverConfig, err)
	}
	state := &channelState{
		parent:                c,
		serverConfig:          serverConfig,
		interestedAuthorities: make(map[*authority]bool),
	}
	channel, err := newXDSChannel(xdsChannelOpts{
		transport:          tr,
		serverConfig:       serverConfig,
		bootstrapConfig:    c.config,
		resourceTypeGetter: c.resourceTypes.get,
		eventHandler:       state,
		backoff:            c.backoff,
		watchExpiryTimeout: c.watchExpiryTimeout,
		logPrefix:          clientPrefix(c),
	})
	if err != nil {
		return nil, func() {}, fmt.Errorf("failed to create xdsChannel for server config %s: %v", serverConfig, err)
	}
	state.channel = channel
	c.xdsActiveChannels[serverConfig.String()] = state
	initLocked(state)
	return state.channel, c.releaseChannel(serverConfig, state, deInitLocked), nil
}

// releaseChannel is a function that is called when a reference to an xdsChannel
// needs to be released. It handles closing channels with no active references.
//
// The function takes the following parameters:
// - serverConfig: the server configuration for the xdsChannel
// - state: the state of the xdsChannel
// - deInitLocked: a function that performs any necessary cleanup for the xdsChannel
//
// The function returns another function that can be called to release the
// reference to the xdsChannel. This returned function is idempotent, meaning
// it can be called multiple times without any additional effect.
func (c *clientImpl) releaseChannel(serverConfig *bootstrap.ServerConfig, state *channelState, deInitLocked func(*channelState)) func() {
	return grpcsync.OnceFunc(func() {
		c.channelsMu.Lock()

		if c.logger.V(2) {
			c.logger.Infof("Received request to release a reference to an xdsChannel for server config %q", serverConfig)
		}
		deInitLocked(state)

		// The channel has active users. Do nothing and return.
		if state.lrsRefs != 0 || len(state.interestedAuthorities) != 0 {
			if c.logger.V(2) {
				c.logger.Infof("xdsChannel %p has other active references", state.channel)
			}
			c.channelsMu.Unlock()
			return
		}

		delete(c.xdsActiveChannels, serverConfig.String())
		if c.logger.V(2) {
			c.logger.Infof("Closing xdsChannel [%p] for server config %s", state.channel, serverConfig)
		}
		channelToClose := state.channel
		c.channelsMu.Unlock()

		channelToClose.close()
	})
}
