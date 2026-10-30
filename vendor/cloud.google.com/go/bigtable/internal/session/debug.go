// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package session

import (
	btransport "cloud.google.com/go/bigtable/internal/transport"
)

// SessionDebug returns the session-pool debug provider. Diverter() is
// empty here — the classic/session split is a mixed-mode concept that
// only exists on bigtable.Client, which composes its own
// SessionDebugProvider over this one and layers the diverter in.
//
// Returns nil when the client was constructed with EnableDebug false;
// pools in that mode skip every allocating debug recorder so there is
// no snapshot state to surface. Callers should treat nil as
// "no debug data available".
func (sc *sessionClient) SessionDebug() btransport.SessionDebugProvider {
	if !sc.enableDebug {
		return nil
	}
	return sessionDebugProviderImpl{sc: sc}
}

// ChannelDebug returns the channel-pool debug provider covering the one
// channel pool this Client owns. Role is always "session" here.
// The mixed-mode bigtable.Client composes its own ChannelDebugProvider
// on top of this one, prepending the classic pool.
func (sc *sessionClient) ChannelDebug() btransport.ChannelDebugProvider {
	return channelDebugProviderImpl{sc: sc}
}

// ConfigDebug returns the configuration debug provider, or nil when no
// ConfigurationManager was wired at construction (stub == nil path).
func (sc *sessionClient) ConfigDebug() btransport.ConfigDebugProvider {
	if sc.configManager == nil {
		return nil
	}
	return configDebugProviderImpl{mgr: sc.configManager}
}

// sessionDebugProviderImpl is the concrete SessionDebugProvider returned
// by *sessionClient.SessionDebug. Kept unexported — consumers hold the
// interface. Diverter() returns an empty snapshot; only bigtable.Client
// knows about the classic/session split.
type sessionDebugProviderImpl struct {
	sc *sessionClient
}

func (p sessionDebugProviderImpl) Snapshot() []btransport.PoolSnapshot {
	return p.sc.PoolSnapshots()
}

func (p sessionDebugProviderImpl) LoadBalancingSnapshots() []btransport.LoadBalancingSnapshot {
	return p.sc.LoadBalancingSnapshots()
}

func (p sessionDebugProviderImpl) Diverter() btransport.DiverterSnapshot {
	return btransport.DiverterSnapshot{}
}

// channelDebugProviderImpl is the concrete ChannelDebugProvider returned
// by *sessionClient.ChannelDebug. Emits exactly one ChannelPoolDebug
// entry with Role="session" — the ChannelPool this Client owns.
// Returns an empty slice when no BigtableChannelPool is available (e.g.
// a fake pool used in tests).
type channelDebugProviderImpl struct {
	sc *sessionClient
}

func (p channelDebugProviderImpl) Snapshot() []btransport.ChannelPoolDebug {
	pool := p.sc.ChannelPool()
	if pool == nil {
		return nil
	}
	return []btransport.ChannelPoolDebug{{
		Role:              "session",
		Snapshot:          pool.ChannelPoolSnapshot(),
		SessionsByChannel: sessionsByChannelForPools(p.sc.PoolSnapshots()),
	}}
}

// sessionsByChannelForPools groups the sessions across all owned pools
// by their ChannelIndex, matching what the mixed-mode channelDebugAdapter
// used to compute inside bigtable/session_debug.go. Sessions without a
// valid channel index (sentinel -1) are skipped.
func sessionsByChannelForPools(pools []btransport.PoolSnapshot) map[int][]btransport.SessionRef {
	var out map[int][]btransport.SessionRef
	for _, pool := range pools {
		for _, s := range pool.Sessions {
			if s.ChannelIndex < 0 {
				continue
			}
			if out == nil {
				out = map[int][]btransport.SessionRef{}
			}
			out[s.ChannelIndex] = append(out[s.ChannelIndex], btransport.SessionRef{
				PoolName: pool.Name,
				LogName:  s.LogName,
			})
		}
	}
	return out
}

// configDebugProviderImpl wraps a *ClientConfigurationManager so the
// Client can hand out a ConfigDebugProvider without exposing the
// manager directly.
type configDebugProviderImpl struct {
	mgr *btransport.ClientConfigurationManager
}

func (p configDebugProviderImpl) Snapshot() btransport.ConfigSnapshot {
	return p.mgr.Snapshot()
}
