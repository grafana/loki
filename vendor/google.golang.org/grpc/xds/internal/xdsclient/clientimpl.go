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
	"sync"
	"time"

	"google.golang.org/grpc/internal/cache"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
)

var _ XDSClient = &clientImpl{}

// clientImpl is the real implementation of the xds client. The exported Client
// is a wrapper of this struct with a ref count.
//
// Implements UpdateHandler interface.
// TODO(easwars): Make a wrapper struct which implements this interface in the
// style of ccBalancerWrapper so that the Client type does not implement these
// exported methods.
type clientImpl struct {
	done   *grpcsync.Event
	config *bootstrap.Config

	// authorityMu protects the authority fields. It's necessary because an
	// authority is created when it's used.
	authorityMu sync.Mutex
	// authorities is a map from ServerConfig to authority. So that
	// different authorities sharing the same ServerConfig can share the
	// authority.
	//
	// The key is **ServerConfig.String()**, not the authority name.
	//
	// An authority is either in authorities, or idleAuthorities,
	// never both.
	authorities map[string]*authority
	// idleAuthorities keeps the authorities that are not used (the last
	// watch on it was canceled). They are kept in the cache and will be deleted
	// after a timeout. The key is ServerConfig.String().
	//
	// An authority is either in authorities, or idleAuthorities,
	// never both.
	idleAuthorities *cache.TimeoutCache

	logger             *grpclog.PrefixLogger
	watchExpiryTimeout time.Duration
}

// BootstrapConfig returns the configuration read from the bootstrap file.
// Callers must treat the return value as read-only.
func (c *clientImpl) BootstrapConfig() *bootstrap.Config {
	return c.config
}

// Close closes the gRPC connection to the management server.
func (c *clientImpl) Close() {
	if c.done.HasFired() {
		return
	}
	c.done.Fire()
	// TODO: Should we invoke the registered callbacks here with an error that
	// the client is closed?

	// Note that Close needs to check for nils even if some of them are always
	// set in the constructor. This is because the constructor defers Close() in
	// error cases, and the fields might not be set when the error happens.

	c.authorityMu.Lock()
	for _, a := range c.authorities {
		a.close()
	}
	c.idleAuthorities.Clear(true)
	c.authorityMu.Unlock()

	c.logger.Infof("Shutdown")
}
