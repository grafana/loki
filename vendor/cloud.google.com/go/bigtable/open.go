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

package bigtable

import (
	"cloud.google.com/go/bigtable/internal/session"

	"google.golang.org/grpc/metadata"
)

// Open opens a table. The returned *Table honors the Client's Diverter:
// when c.diverter is set, Apply and ReadRow are routed through an
// internal TableShim so calls can be diverted to the session data path
// under the diverter's SessionLoad ratio. Backward-compatible — the
// return type stays *Table and every method that already existed keeps
// its signature and behavior.
func (c *Client) Open(table string) *Table {
	t := &Table{
		c:     c,
		table: table,
		md: metadata.Join(metadata.Pairs(
			resourcePrefixHeader, c.fullTableName(table),
			requestParamsHeader, c.reqParamsHeaderValTable(table),
		), c.featureFlagsMD),
	}
	t.divertible = c.buildDivertible(t, func() session.TableAPI {
		return c.getOrCreateSessionTable(table)
	})
	return t
}

// buildDivertible wraps t's classic side in a TableShim so Apply /
// ReadRow can be routed to the session data path. Returns nil when
// c.diverter is nil so the caller stays on the zero-cost classic path.
//
// The classic side is a *tableImpl over a value-copy of t with
// divertible nil-ed — that break in the loop is what stops
// tableImpl.Apply/ReadRow from recursing back through the outer gate.
func (c *Client) buildDivertible(t *Table, openSession func() session.TableAPI) TableAPI {
	if c.diverter == nil {
		return nil
	}
	inner := *t
	inner.divertible = nil
	var sess session.TableAPI
	if c.sessionImpl != nil && openSession != nil {
		sess = openSession()
	}
	return NewTableShim(&tableImpl{Table: inner}, sess, c.diverter)
}

// OpenTable opens a table. Returns a TableShim that routes each RPC via
// the Client's Diverter — with sessionLoad=0.0 every call lands on the
// classic path. The session TableAPI is wired from the Client's
// sessionImpl when present; it's nil when the caller pre-dialed a
// custom gRPC conn or opted out via ClientConfig.DisableSession, in
// which case the shim runs classic-only and useSession() reports
// false. Server-driven SessionLoad updates from
// ClientConfigurationManager retarget traffic without re-opening the
// table.
func (c *Client) OpenTable(table string) TableAPI {
	classic := &tableImpl{Table{
		c:     c,
		table: table,
		md: metadata.Join(metadata.Pairs(
			resourcePrefixHeader, c.fullTableName(table),
			requestParamsHeader, c.reqParamsHeaderValTable(table),
		), c.featureFlagsMD),
	}}
	return NewTableShim(classic, c.getOrCreateSessionTable(table), c.diverter)
}

// OpenAuthorizedView opens an authorized view. See OpenTable for the
// diverter routing story.
func (c *Client) OpenAuthorizedView(table, authorizedView string) TableAPI {
	classic := &tableImpl{Table{
		c:     c,
		table: table,
		md: metadata.Join(metadata.Pairs(
			resourcePrefixHeader, c.fullAuthorizedViewName(table, authorizedView),
			requestParamsHeader, c.reqParamsHeaderValTable(table),
		), c.featureFlagsMD),
		authorizedView: authorizedView,
	}}
	return NewTableShim(classic, c.getOrCreateSessionAuthorizedView(table, authorizedView), c.diverter)
}

// OpenMaterializedView opens a materialized view. See OpenTable for the
// diverter routing story.
func (c *Client) OpenMaterializedView(materializedView string) TableAPI {
	classic := &tableImpl{Table{
		c: c,
		md: metadata.Join(metadata.Pairs(
			resourcePrefixHeader, c.fullMaterializedViewName(materializedView),
			requestParamsHeader, c.reqParamsHeaderValTable(materializedView),
		), c.featureFlagsMD),
		materializedView: materializedView,
	}}
	return NewTableShim(classic, c.getOrCreateSessionMaterializedView(materializedView), c.diverter)
}

// getOrCreateSessionTable returns a cached session TableAPI handle
// for this table. Returns nil when the session backend isn't wired
// (hand-built or emulator-only Clients where sessionImpl is nil).
// TableShim treats a nil session as classic-only.
//
// The cache key is the fully-qualified table resource name
// ("projects/P/instances/I/tables/T") — same identity Cloud Bigtable
// uses over the wire, so table + AV + MV keys never collide even
// though they share one cache. Handles evict after
// session.DefaultTableCacheTTL of idle (default 1 h) or when the
// caller Close()s them explicitly. See internal/session/table_cache.go.
func (c *Client) getOrCreateSessionTable(table string) session.TableAPI {
	if c.sessionImpl == nil {
		return nil
	}
	return c.sessionTables.GetOrOpen(c.fullTableName(table), func() session.TableAPI {
		return c.sessionImpl.OpenTable(table)
	})
}

// getOrCreateSessionAuthorizedView is the cache lookup for authorized
// views. Cache key is the fully-qualified AV resource name
// ("projects/P/instances/I/tables/T/authorizedViews/V") — table-
// qualified by construction, so two AVs with the same view id on
// different tables get distinct session pools + distinct sessionz
// labels.
func (c *Client) getOrCreateSessionAuthorizedView(table, view string) session.TableAPI {
	if c.sessionImpl == nil {
		return nil
	}
	return c.sessionTables.GetOrOpen(c.fullAuthorizedViewName(table, view), func() session.TableAPI {
		return c.sessionImpl.OpenAuthorizedView(table, view)
	})
}

// getOrCreateSessionMaterializedView is the cache lookup for
// materialized views. Cache key is the fully-qualified MV resource
// name ("projects/P/instances/I/materializedViews/V").
func (c *Client) getOrCreateSessionMaterializedView(view string) session.TableAPI {
	if c.sessionImpl == nil {
		return nil
	}
	return c.sessionTables.GetOrOpen(c.fullMaterializedViewName(view), func() session.TableAPI {
		return c.sessionImpl.OpenMaterializedView(view)
	})
}
