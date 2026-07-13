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

package internal

import (
	"fmt"

	spb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
	"google.golang.org/protobuf/proto"
)

// SessionType represents the protocol target session type.
type SessionType int

const (
	// SessionTypeTable indicates standard table session type.
	SessionTypeTable SessionType = iota
	// SessionTypeAuthorizedView indicates authorized view session type.
	SessionTypeAuthorizedView
	// SessionTypeMaterializedView indicates materialized view session type.
	SessionTypeMaterializedView
)

func (t SessionType) String() string {
	switch t {
	case SessionTypeTable:
		return "table"
	case SessionTypeAuthorizedView:
		return "authorized_view"
	case SessionTypeMaterializedView:
		return "materialized_view"
	default:
		return "unknown"
	}
}

// SessionDescriptor models a dynamic envelope handshake parameters compiler.
type SessionDescriptor struct {
	Type       SessionType
	MethodName string
	HeaderKeys []string
	LogNameFn  func(req proto.Message) string
	MetadataFn func(req proto.Message) map[string]string // Populates session metadata headers required for OpenSession{} RPC
}

var (
	// TABLE_SESSION manages standard table scoped Session streams.
	TABLE_SESSION = &SessionDescriptor{
		Type:       SessionTypeTable,
		MethodName: "OpenTable",
		HeaderKeys: []string{"table_name", "app_profile_id", "permission"},
		LogNameFn: func(req proto.Message) string {
			r, ok := req.(*spb.OpenTableRequest)
			if !ok || r == nil {
				return "TableSession(nil)"
			}
			return fmt.Sprintf("TableSession(table=%s, app_profile=%s, perm=%s)", r.TableName, r.AppProfileId, r.Permission.String())
		},
		MetadataFn: func(req proto.Message) map[string]string {
			r, ok := req.(*spb.OpenTableRequest)
			if !ok || r == nil {
				return nil
			}
			return map[string]string{
				"open_session.payload.table_name":     r.TableName,
				"open_session.payload.app_profile_id": r.AppProfileId,
				"open_session.payload.permission":     r.Permission.String(),
			}
		},
	}

	// AUTHORIZED_VIEW_SESSION manages authorized view scoped Session streams.
	AUTHORIZED_VIEW_SESSION = &SessionDescriptor{
		Type:       SessionTypeAuthorizedView,
		MethodName: "OpenAuthorizedView",
		HeaderKeys: []string{"authorized_view_name", "app_profile_id", "permission"},
		LogNameFn: func(req proto.Message) string {
			r, ok := req.(*spb.OpenAuthorizedViewRequest)
			if !ok || r == nil {
				return "AuthorizedViewSession(nil)"
			}
			return fmt.Sprintf("AuthorizedViewSession(view=%s, app_profile=%s, perm=%s)", r.AuthorizedViewName, r.AppProfileId, r.Permission.String())
		},
		MetadataFn: func(req proto.Message) map[string]string {
			r, ok := req.(*spb.OpenAuthorizedViewRequest)
			if !ok || r == nil {
				return nil
			}
			return map[string]string{
				"open_session.payload.authorized_view_name": r.AuthorizedViewName,
				"open_session.payload.app_profile_id":       r.AppProfileId,
				"open_session.payload.permission":           r.Permission.String(),
			}
		},
	}

	// MATERIALIZED_VIEW_SESSION manages materialized view scoped Session streams (Read-Only).
	MATERIALIZED_VIEW_SESSION = &SessionDescriptor{
		Type:       SessionTypeMaterializedView,
		MethodName: "OpenMaterializedView",
		HeaderKeys: []string{"materialized_view_name", "app_profile_id", "permission"},
		LogNameFn: func(req proto.Message) string {
			r, ok := req.(*spb.OpenMaterializedViewRequest)
			if !ok || r == nil {
				return "MaterializedViewSession(nil)"
			}
			return fmt.Sprintf("MaterializedViewSession(view=%s, app_profile=%s, perm=%s)", r.MaterializedViewName, r.AppProfileId, r.Permission.String())
		},
		MetadataFn: func(req proto.Message) map[string]string {
			r, ok := req.(*spb.OpenMaterializedViewRequest)
			if !ok || r == nil {
				return nil
			}
			return map[string]string{
				"open_session.payload.materialized_view_name": r.MaterializedViewName,
				"open_session.payload.app_profile_id":         r.AppProfileId,
				"open_session.payload.permission":             r.Permission.String(),
			}
		},
	}
)
