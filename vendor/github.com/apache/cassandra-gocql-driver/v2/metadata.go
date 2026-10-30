// Copyright (c) 2015 The gocql Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
/*
 * Content before git sha 34fdeebefcbf183ed7f916f931aa0586fdaa1b40
 * Copyright (c) 2016, The Gocql authors,
 * provided under the BSD-3-Clause License.
 * See the NOTICE file distributed with this work for additional information.
 */

package gocql

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// schema metadata for a keyspace
type KeyspaceMetadata struct {
	Name              string
	DurableWrites     bool
	StrategyClass     string
	StrategyOptions   map[string]interface{}
	placementStrategy placementStrategy
	Tables            map[string]*TableMetadata
	Functions         map[string]*FunctionMetadata
	Aggregates        map[string]*AggregateMetadata
	MaterializedViews map[string]*MaterializedViewMetadata
	UserTypes         map[string]*UserTypeMetadata
}

// Clone creates a deep copy of the KeyspaceMetadata.
func (k *KeyspaceMetadata) Clone() *KeyspaceMetadata {
	if k == nil {
		return nil
	}

	clone := &KeyspaceMetadata{
		Name:              k.Name,
		DurableWrites:     k.DurableWrites,
		StrategyClass:     k.StrategyClass,
		placementStrategy: k.placementStrategy,
	}

	// Clone StrategyOptions map
	if k.StrategyOptions != nil {
		clone.StrategyOptions = make(map[string]interface{}, len(k.StrategyOptions))
		for key, value := range k.StrategyOptions {
			clone.StrategyOptions[key] = value
		}
	}

	// Clone Tables map
	if k.Tables != nil {
		clone.Tables = make(map[string]*TableMetadata, len(k.Tables))
		for key, value := range k.Tables {
			clone.Tables[key] = value.Clone()
		}
	}

	// Clone Functions map
	if k.Functions != nil {
		clone.Functions = make(map[string]*FunctionMetadata, len(k.Functions))
		for key, value := range k.Functions {
			clone.Functions[key] = value.Clone()
		}
	}

	// Clone Aggregates map
	if k.Aggregates != nil {
		clone.Aggregates = make(map[string]*AggregateMetadata, len(k.Aggregates))
		for key, value := range k.Aggregates {
			clone.Aggregates[key] = value.Clone()
		}
	}

	// Clone MaterializedViews map
	if k.MaterializedViews != nil {
		clone.MaterializedViews = make(map[string]*MaterializedViewMetadata, len(k.MaterializedViews))
		for key, value := range k.MaterializedViews {
			clone.MaterializedViews[key] = value.Clone()
		}
	}

	// Clone UserTypes map
	if k.UserTypes != nil {
		clone.UserTypes = make(map[string]*UserTypeMetadata, len(k.UserTypes))
		for key, value := range k.UserTypes {
			clone.UserTypes[key] = value.Clone()
		}
	}

	return clone
}

// schema metadata for a table (a.k.a. column family)
type TableMetadata struct {
	Keyspace          string
	Name              string
	KeyValidator      string
	Comparator        string
	DefaultValidator  string
	KeyAliases        []string
	ColumnAliases     []string
	ValueAlias        string
	PartitionKey      []*ColumnMetadata
	ClusteringColumns []*ColumnMetadata
	Columns           map[string]*ColumnMetadata
	OrderedColumns    []string
}

// Clone creates a deep copy of the TableMetadata.
func (t *TableMetadata) Clone() *TableMetadata {
	if t == nil {
		return nil
	}

	clone := &TableMetadata{
		Keyspace:         t.Keyspace,
		Name:             t.Name,
		KeyValidator:     t.KeyValidator,
		Comparator:       t.Comparator,
		DefaultValidator: t.DefaultValidator,
		ValueAlias:       t.ValueAlias,
	}

	// Clone KeyAliases slice
	if t.KeyAliases != nil {
		clone.KeyAliases = make([]string, len(t.KeyAliases))
		copy(clone.KeyAliases, t.KeyAliases)
	}

	// Clone ColumnAliases slice
	if t.ColumnAliases != nil {
		clone.ColumnAliases = make([]string, len(t.ColumnAliases))
		copy(clone.ColumnAliases, t.ColumnAliases)
	}

	// Clone PartitionKey slice
	if t.PartitionKey != nil {
		clone.PartitionKey = make([]*ColumnMetadata, len(t.PartitionKey))
		for i, col := range t.PartitionKey {
			clone.PartitionKey[i] = col.Clone()
		}
	}

	// Clone ClusteringColumns slice
	if t.ClusteringColumns != nil {
		clone.ClusteringColumns = make([]*ColumnMetadata, len(t.ClusteringColumns))
		for i, col := range t.ClusteringColumns {
			clone.ClusteringColumns[i] = col.Clone()
		}
	}

	// Clone Columns map
	if t.Columns != nil {
		clone.Columns = make(map[string]*ColumnMetadata, len(t.Columns))
		for key, value := range t.Columns {
			clone.Columns[key] = value.Clone()
		}
	}

	// Clone OrderedColumns slice
	if t.OrderedColumns != nil {
		clone.OrderedColumns = make([]string, len(t.OrderedColumns))
		copy(clone.OrderedColumns, t.OrderedColumns)
	}

	return clone
}

// schema metadata for a column
type ColumnMetadata struct {
	Keyspace        string
	Table           string
	Name            string
	ComponentIndex  int
	Kind            ColumnKind
	Validator       string
	Type            TypeInfo
	ClusteringOrder string
	Order           ColumnOrder
	Index           ColumnIndexMetadata
}

// Clone creates a deep copy of the ColumnMetadata.
func (c *ColumnMetadata) Clone() *ColumnMetadata {
	if c == nil {
		return nil
	}

	// ColumnMetadata contains only value types and TypeInfo (which is an interface)
	// We create a shallow copy which is sufficient since TypeInfo implementations
	// are typically immutable
	clone := &ColumnMetadata{
		Keyspace:        c.Keyspace,
		Table:           c.Table,
		Name:            c.Name,
		ComponentIndex:  c.ComponentIndex,
		Kind:            c.Kind,
		Validator:       c.Validator,
		Type:            c.Type,
		ClusteringOrder: c.ClusteringOrder,
		Order:           c.Order,
		Index:           c.Index,
	}

	return clone
}

// FunctionMetadata holds metadata for function constructs
type FunctionMetadata struct {
	Keyspace          string
	Name              string
	ArgumentTypes     []TypeInfo
	ArgumentNames     []string
	Body              string
	CalledOnNullInput bool
	Language          string
	ReturnType        TypeInfo

	// row string values for the argument types
	argumentTypesRaw []string

	// row string value for the return type
	returnTypeRaw string
}

// Clone creates a deep copy of the FunctionMetadata.
func (f *FunctionMetadata) Clone() *FunctionMetadata {
	if f == nil {
		return nil
	}

	clone := &FunctionMetadata{
		Keyspace:          f.Keyspace,
		Name:              f.Name,
		Body:              f.Body,
		CalledOnNullInput: f.CalledOnNullInput,
		Language:          f.Language,
		ReturnType:        f.ReturnType,
		returnTypeRaw:     f.returnTypeRaw,
		argumentTypesRaw:  f.argumentTypesRaw, // Shallow copy - unexported field
	}

	// Clone ArgumentTypes slice
	if f.ArgumentTypes != nil {
		clone.ArgumentTypes = make([]TypeInfo, len(f.ArgumentTypes))
		copy(clone.ArgumentTypes, f.ArgumentTypes)
	}

	// Clone ArgumentNames slice
	if f.ArgumentNames != nil {
		clone.ArgumentNames = make([]string, len(f.ArgumentNames))
		copy(clone.ArgumentNames, f.ArgumentNames)
	}

	return clone
}

// AggregateMetadata holds metadata for aggregate constructs
type AggregateMetadata struct {
	Keyspace      string
	Name          string
	ArgumentTypes []TypeInfo
	FinalFunc     FunctionMetadata
	InitCond      string
	ReturnType    TypeInfo
	StateFunc     FunctionMetadata
	StateType     TypeInfo

	stateFunc string
	finalFunc string

	// raw string value for the state type
	stateTypeRaw string
	// raw string values for the argument types
	argumentTypesRaw []string
	// raw string value for the return type
	returnTypeRaw string
}

// Clone creates a deep copy of the AggregateMetadata.
func (a *AggregateMetadata) Clone() *AggregateMetadata {
	if a == nil {
		return nil
	}

	clone := &AggregateMetadata{
		Keyspace:         a.Keyspace,
		Name:             a.Name,
		FinalFunc:        a.FinalFunc,
		InitCond:         a.InitCond,
		ReturnType:       a.ReturnType,
		StateFunc:        a.StateFunc,
		StateType:        a.StateType,
		stateFunc:        a.stateFunc,
		finalFunc:        a.finalFunc,
		stateTypeRaw:     a.stateTypeRaw,
		returnTypeRaw:    a.returnTypeRaw,
		argumentTypesRaw: a.argumentTypesRaw, // Shallow copy - unexported field
	}

	// Clone ArgumentTypes slice
	if a.ArgumentTypes != nil {
		clone.ArgumentTypes = make([]TypeInfo, len(a.ArgumentTypes))
		copy(clone.ArgumentTypes, a.ArgumentTypes)
	}

	return clone
}

// MaterializedViewMetadata holds the metadata for materialized views.
type MaterializedViewMetadata struct {
	Keyspace                string
	Name                    string
	AdditionalWritePolicy   string
	BaseTableId             UUID
	BaseTable               *TableMetadata
	BloomFilterFpChance     float64
	Caching                 map[string]string
	Comment                 string
	Compaction              map[string]string
	Compression             map[string]string
	CrcCheckChance          float64
	DcLocalReadRepairChance float64
	DefaultTimeToLive       int
	Extensions              map[string]string
	GcGraceSeconds          int
	Id                      UUID
	IncludeAllColumns       bool
	MaxIndexInterval        int
	MemtableFlushPeriodInMs int
	MinIndexInterval        int
	ReadRepair              string  // Only present in Cassandra 4.0+
	ReadRepairChance        float64 // Note: Cassandra 4.0 removed ReadRepairChance and added ReadRepair instead
	SpeculativeRetry        string

	baseTableName string
}

// Clone creates a deep copy of the MaterializedViewMetadata.
func (m *MaterializedViewMetadata) Clone() *MaterializedViewMetadata {
	if m == nil {
		return nil
	}

	clone := &MaterializedViewMetadata{
		Keyspace:                m.Keyspace,
		Name:                    m.Name,
		AdditionalWritePolicy:   m.AdditionalWritePolicy,
		BaseTableId:             m.BaseTableId,
		BaseTable:               m.BaseTable.Clone(),
		BloomFilterFpChance:     m.BloomFilterFpChance,
		Comment:                 m.Comment,
		CrcCheckChance:          m.CrcCheckChance,
		DcLocalReadRepairChance: m.DcLocalReadRepairChance,
		DefaultTimeToLive:       m.DefaultTimeToLive,
		GcGraceSeconds:          m.GcGraceSeconds,
		Id:                      m.Id,
		IncludeAllColumns:       m.IncludeAllColumns,
		MaxIndexInterval:        m.MaxIndexInterval,
		MemtableFlushPeriodInMs: m.MemtableFlushPeriodInMs,
		MinIndexInterval:        m.MinIndexInterval,
		ReadRepair:              m.ReadRepair,
		ReadRepairChance:        m.ReadRepairChance,
		SpeculativeRetry:        m.SpeculativeRetry,
		baseTableName:           m.baseTableName,
	}

	// Clone Caching map
	if m.Caching != nil {
		clone.Caching = make(map[string]string, len(m.Caching))
		for key, value := range m.Caching {
			clone.Caching[key] = value
		}
	}

	// Clone Compaction map
	if m.Compaction != nil {
		clone.Compaction = make(map[string]string, len(m.Compaction))
		for key, value := range m.Compaction {
			clone.Compaction[key] = value
		}
	}

	// Clone Compression map
	if m.Compression != nil {
		clone.Compression = make(map[string]string, len(m.Compression))
		for key, value := range m.Compression {
			clone.Compression[key] = value
		}
	}

	// Clone Extensions map
	if m.Extensions != nil {
		clone.Extensions = make(map[string]string, len(m.Extensions))
		for key, value := range m.Extensions {
			clone.Extensions[key] = value
		}
	}

	return clone
}

// UserTypeMetadata represents metadata information about a Cassandra User Defined Type (UDT).
// This Go struct holds descriptive information about a UDT that exists in the Cassandra schema,
// including the type name, keyspace, field names, and field types. It is not the UDT itself,
// but rather a representation of the UDT's schema structure for use within the gocql driver.
//
// A Cassandra User Defined Type is a custom data type that allows you to group related fields
// together. This metadata struct provides the necessary information to marshal and unmarshal
// values to and from the corresponding UDT in Cassandra.
//
// For type information used in marshaling/unmarshaling operations, see UDTTypeInfo.
// Actual UDT values are typically represented as map[string]interface{}, Go structs with
// cql tags, or types implementing UDTMarshaler/UDTUnmarshaler interfaces.
type UserTypeMetadata struct {
	Keyspace      string     // The keyspace where the UDT is defined
	Name          string     // The name of the User Defined Type
	FieldNames    []string   // Ordered list of field names in the UDT
	FieldTypes    []TypeInfo // Corresponding type information for each field
	fieldTypesRaw []string   // Raw string values for the field types
}

// Clone creates a deep copy of the UserTypeMetadata.
func (u *UserTypeMetadata) Clone() *UserTypeMetadata {
	if u == nil {
		return nil
	}

	clone := &UserTypeMetadata{
		Keyspace:      u.Keyspace,
		Name:          u.Name,
		fieldTypesRaw: u.fieldTypesRaw, // Shallow copy - unexported field
	}

	// Clone FieldNames slice
	if u.FieldNames != nil {
		clone.FieldNames = make([]string, len(u.FieldNames))
		copy(clone.FieldNames, u.FieldNames)
	}

	// Clone FieldTypes slice
	if u.FieldTypes != nil {
		clone.FieldTypes = make([]TypeInfo, len(u.FieldTypes))
		copy(clone.FieldTypes, u.FieldTypes)
	}

	return clone
}

// ColumnOrder represents the ordering of a column with regard to its comparator.
// It indicates whether the column is sorted in ascending or descending order.
// Available values: ASC, DESC.
type ColumnOrder bool

const (
	ASC  ColumnOrder = false
	DESC ColumnOrder = true
)

// ColumnIndexMetadata represents metadata for a column index in Cassandra.
// It contains the index name, type, and configuration options.
type ColumnIndexMetadata struct {
	Name    string
	Type    string
	Options map[string]interface{}
}

// ColumnKind represents the kind of column in a Cassandra table.
// It indicates whether the column is part of the partition key, clustering key, or a regular column.
// Available values: ColumnUnkownKind, ColumnPartitionKey, ColumnClusteringKey, ColumnRegular, ColumnCompact, ColumnStatic.
type ColumnKind int

const (
	ColumnUnkownKind ColumnKind = iota
	ColumnPartitionKey
	ColumnClusteringKey
	ColumnRegular
	ColumnCompact
	ColumnStatic
)

func (c ColumnKind) String() string {
	switch c {
	case ColumnPartitionKey:
		return "partition_key"
	case ColumnClusteringKey:
		return "clustering_key"
	case ColumnRegular:
		return "regular"
	case ColumnCompact:
		return "compact"
	case ColumnStatic:
		return "static"
	default:
		return fmt.Sprintf("unknown_column_%d", c)
	}
}

func (c *ColumnKind) UnmarshalCQL(typ TypeInfo, p []byte) error {
	if typ.Type() != TypeVarchar {
		return unmarshalErrorf("unable to marshall %s into ColumnKind, expected Varchar", typ)
	}

	kind, err := columnKindFromSchema(string(p))
	if err != nil {
		return fmt.Errorf("failed to parse column kind from schema: %w", err)
	}
	*c = kind

	return nil
}

func columnKindFromSchema(kind string) (ColumnKind, error) {
	switch kind {
	case "partition_key":
		return ColumnPartitionKey, nil
	case "clustering_key", "clustering":
		return ColumnClusteringKey, nil
	case "regular":
		return ColumnRegular, nil
	case "compact_value":
		return ColumnCompact, nil
	case "static":
		return ColumnStatic, nil
	default:
		return -1, fmt.Errorf("unknown column kind: %q", kind)
	}
}

// default alias values
const (
	DEFAULT_KEY_ALIAS    = "key"
	DEFAULT_COLUMN_ALIAS = "column"
	DEFAULT_VALUE_ALIAS  = "value"
)

// queries the cluster for schema information for a specific keyspace
type schemaDescriber struct {
	session         *Session
	schemaRefresher *refreshDebouncer
	schemaMeta      atomic.Value // *schemaMeta
}

// Schema change type constants as defined in the Cassandra Native Protocol specification.
// These values indicate the nature of schema modifications that occurred.
//
// See: https://cassandra.apache.org/doc/latest/cassandra/reference/native-protocol.html
//
// Schema change events are server-initiated messages sent to clients that have registered
// for schema change notifications. These events indicate modifications to keyspaces, tables,
// user-defined types, functions, or aggregates.
const (
	SchemaChangeTypeCreated = "CREATED" // Schema object was created
	SchemaChangeTypeUpdated = "UPDATED" // Schema object was modified
	SchemaChangeTypeDropped = "DROPPED" // Schema object was removed
)

type schemaMeta struct {
	keyspaceMeta map[string]*KeyspaceMetadata
}

// creates a session bound schema describer which will query and cache
// keyspace metadata
func newSchemaDescriber(session *Session, schemaRefresher *refreshDebouncer) *schemaDescriber {
	meta := new(schemaMeta)
	describer := &schemaDescriber{
		session: session,
	}
	describer.schemaMeta.Store(meta)
	describer.schemaRefresher = schemaRefresher
	return describer
}

func (s *schemaDescriber) getSchemaMetaForRead() *schemaMeta {
	meta, _ := s.schemaMeta.Load().(*schemaMeta)
	return meta
}

func (s *schemaDescriber) getSchemaMetaForUpdate() *schemaMeta {
	meta := s.getSchemaMetaForRead()
	metaNew := new(schemaMeta)
	if meta != nil {
		*metaNew = *meta
	}
	return metaNew
}

// getSchema returns the KeyspaceMetadata for the specified keyspace.
//
// Behavior by CacheMode:
//   - Disabled: Fetches full metadata directly from Cassandra (calls fetchSchema)
//   - Full: Returns cloned full metadata from cache
//   - KeyspaceOnly: Returns cloned keyspace-level metadata from cache (no tables, functions, etc.)
//
// Returns a clone of the metadata to prevent external modifications to the cache.
func (s *schemaDescriber) getSchema(keyspaceName string) (*KeyspaceMetadata, error) {
	if s.session.cfg.Metadata.CacheMode == Disabled {
		return s.fetchSchema(keyspaceName)
	}
	metadata, found := s.getSchemaMetaForRead().keyspaceMeta[keyspaceName]

	if !found {
		return nil, ErrKeyspaceDoesNotExist
	}

	return metadata.Clone(), nil
}

// getAllSchema returns all KeyspaceMetadata for all keyspaces.
//
// Behavior by CacheMode:
//   - Disabled: Fetches full metadata directly from Cassandra (calls fetchAllSchema with fetchFullMetadata=true)
//   - Full: Returns cloned full metadata from cache
//   - KeyspaceOnly: Returns cloned keyspace-level metadata from cache (no tables, functions, etc.)
//
// Returns clones of the metadata to prevent external modifications to the cache.
func (s *schemaDescriber) getAllSchema() (map[string]*KeyspaceMetadata, error) {
	if s.session.cfg.Metadata.CacheMode == Disabled {
		// Always fetch full metadata in Disabled mode to match fetchSchema() behavior
		return s.fetchAllSchema(true)
	}
	metadata := s.getSchemaMetaForRead().keyspaceMeta
	if metadata == nil {
		return nil, fmt.Errorf("cache is nil, this should never happen - report this issue to the GoCQL project on Slack, JIRA or Github")
	}

	// Return clones of the metadata to prevent external modifications to the cache
	result := make(map[string]*KeyspaceMetadata, len(metadata))
	for k, v := range metadata {
		result[k] = v.Clone()
	}
	return result, nil
}

// fetchSchema retrieves full metadata for a specific keyspace directly from Cassandra.
// Always fetches complete metadata including tables, columns, functions, aggregates,
// user types, and materialized views, regardless of CacheMode setting.
//
// This method is called by getSchema when CacheMode is Disabled.
func (s *schemaDescriber) fetchSchema(keyspaceName string) (*KeyspaceMetadata, error) {
	var err error

	// query the system keyspace for schema data
	// TODO retrieve concurrently
	keyspace, err := getKeyspaceMetadata(s.session, keyspaceName)
	if err != nil {
		return nil, err
	}
	tables, err := getTableMetadata(s.session, keyspaceName)
	if err != nil {
		return nil, err
	}
	columns, err := getColumnMetadata(s.session, keyspaceName)
	if err != nil {
		return nil, err
	}
	functions, err := getFunctionsMetadata(s.session, keyspaceName)
	if err != nil {
		return nil, err
	}
	aggregates, err := getAggregatesMetadata(s.session, keyspaceName)
	if err != nil {
		return nil, err
	}
	userTypes, err := getUserTypeMetadata(s.session, keyspaceName)
	if err != nil {
		return nil, err
	}
	materializedViews, err := getMaterializedViewsMetadata(s.session, keyspaceName)
	if err != nil {
		return nil, err
	}

	// organize the schema data
	err = compileMetadata(s.session, keyspace, tables, columns, functions, aggregates, userTypes,
		materializedViews)
	if err != nil {
		return nil, fmt.Errorf("failed to compile keyspace metadata for keyspace %s: %w", keyspaceName, err)
	}

	return keyspace, nil
}

// fetchAllSchema retrieves metadata for all keyspaces directly from Cassandra.
//
// Parameters:
//   - fetchFullMetadata: When true, fetches complete metadata (tables, columns, functions, etc.).
//     When false, fetches only keyspace-level metadata.
//
// This method is called by:
//   - getAllSchema when CacheMode is Disabled (with fetchFullMetadata=true)
//   - refreshSchemas for cache population (with fetchFullMetadata based on CacheMode)
//
// The fetchFullMetadata parameter decouples the fetch behavior from CacheMode,
// allowing consistent behavior between fetchSchema and fetchAllSchema while
// still supporting KeyspaceOnly mode for cache population.
func (s *schemaDescriber) fetchAllSchema(fetchFullMetadata bool) (map[string]*KeyspaceMetadata, error) {
	// query the system keyspace for schema data
	keyspaceStart := time.Now()
	keyspaces, err := getAllKeyspaceMetadata(s.session)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve keyspace metadata: %w", err)
	}
	keyspaceElapsed := time.Since(keyspaceStart)
	s.session.logger.Debug("Keyspace metadata fetch completed",
		NewLogFieldString("duration", keyspaceElapsed.String()))
	var tables map[string][]TableMetadata
	var columns map[string][]ColumnMetadata
	var functions map[string][]FunctionMetadata
	var aggregates map[string][]AggregateMetadata
	var userTypes map[string][]UserTypeMetadata
	var materializedViews map[string][]MaterializedViewMetadata

	// Fetch full metadata if requested
	if fetchFullMetadata {
		tables, err = getAllTablesMetadata(s.session)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve table metadata: %w", err)
		}
		columns, err = getAllColumnMetadata(s.session)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve column metadata: %w", err)
		}
		functions, err = getAllFunctionsMetadata(s.session)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve function metadata: %w", err)
		}
		aggregates, err = getAllAggregatesMetadata(s.session)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve aggregate metadata: %w", err)
		}
		userTypes, err = getAllUserTypeMetadata(s.session)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve user type metadata: %w", err)
		}
		materializedViews, err = getAllMaterializedViewsMetadata(s.session)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve materialized view metadata: %w", err)
		}
	}

	// organize the schema data
	if fetchFullMetadata {
		for keyspaceName, keyspace := range keyspaces {
			err = compileMetadata(s.session,
				keyspace,
				tables[keyspaceName],
				columns[keyspaceName],
				functions[keyspaceName],
				aggregates[keyspaceName],
				userTypes[keyspaceName],
				materializedViews[keyspaceName])
			if err != nil {
				return nil, fmt.Errorf("failed to compile keyspace metadata for keyspace %s: %w", keyspaceName, err)
			}
		}
	}

	return keyspaces, nil
}

// forcibly updates the current KeyspaceMetadata held by the schema describer
// for all the keyspaces.
// This function is called via schemaRefresher refreshDebouncer to batch and
// debounce schema refresh requests.
func refreshSchemas(session *Session) error {
	start := time.Now()
	var refreshErr error
	defer func() {
		elapsed := time.Since(start)
		if refreshErr != nil {
			session.logger.Debug("Schema refresh failed",
				NewLogFieldString("duration", elapsed.String()),
				NewLogFieldError("err", refreshErr))
		} else {
			session.logger.Debug("Schema refresh completed",
				NewLogFieldString("duration", elapsed.String()))
		}
	}()

	if session.cfg.Metadata.CacheMode == Disabled {
		return nil
	}
	awaitErr := session.control.awaitSchemaAgreementWithTimeout(10 * time.Second)
	if awaitErr != nil {
		session.logger.Warning("Failed to await schema agreement, proceeding with schema refresh",
			NewLogFieldError("err", awaitErr))
	}

	var keyspaces map[string]*KeyspaceMetadata
	// Fetch full metadata only when CacheMode is Full
	// This allows KeyspaceOnly mode to cache only keyspace-level metadata
	fetchFull := session.cfg.Metadata.CacheMode == Full
	keyspaces, refreshErr = session.schemaDescriber.fetchAllSchema(fetchFull)
	if refreshErr != nil {
		return refreshErr
	}

	meta := session.schemaDescriber.getSchemaMetaForUpdate()
	oldKeyspaceMeta := meta.keyspaceMeta

	var newKeyspaces []string
	var updatedKeyspaces []string
	var droppedKeyspaces []string

	for keyspaceName, keyspace := range keyspaces {
		if _, ok := oldKeyspaceMeta[keyspaceName]; !ok {
			newKeyspaces = append(newKeyspaces, keyspaceName)
		} else {
			newStrat := keyspace.placementStrategy
			oldKs := oldKeyspaceMeta[keyspaceName]
			oldStrat := oldKs.placementStrategy

			if (newStrat == nil) != (oldStrat == nil) {
				updatedKeyspaces = append(updatedKeyspaces, keyspaceName)
			} else if newStrat != nil && newStrat.strategyKey() != oldStrat.strategyKey() {
				updatedKeyspaces = append(updatedKeyspaces, keyspaceName)
			} else if oldKs.DurableWrites != keyspace.DurableWrites {
				// If the durable writes flag has changed, we need to notify the KeyspaceChangeListener
				updatedKeyspaces = append(updatedKeyspaces, keyspaceName)
			}
		}
	}
	droppedKeyspaces = getDroppedKeyspaces(oldKeyspaceMeta, keyspaces)
	meta.keyspaceMeta = keyspaces
	session.schemaDescriber.schemaMeta.Store(meta)
	refreshCacheElapsed := time.Since(start)
	session.logger.Debug("Schema metadata cache refresh completed",
		NewLogFieldString("duration", refreshCacheElapsed.String()))

	sessionInitialized := session.initialized()

	// Notify policy if it supports schema refresh notifications
	notifier, supportsRefresh := session.policy.(schemaRefreshNotifier)
	hasKeyspaceListener := session.schemaListeners.hasKeyspace() && sessionInitialized

	// Notify the policy if it supports schema refresh notifications
	if supportsRefresh {
		notifier.schemaRefreshed(session.schemaDescriber.getSchemaMetaForRead())
	}

	session.logger.Debug("Computed schema keyspace change events",
		NewLogFieldInt("created_keyspaces_count", len(newKeyspaces)),
		NewLogFieldInt("updated_keyspaces_count", len(updatedKeyspaces)),
		NewLogFieldInt("dropped_keyspaces_count", len(droppedKeyspaces)),
	)

	// If we don't support schemaRefreshed OR we have listeners, we need to loop
	if !supportsRefresh || hasKeyspaceListener {
		for _, name := range newKeyspaces {
			if !supportsRefresh {
				session.policy.KeyspaceChanged(KeyspaceUpdateEvent{Keyspace: name, Change: SchemaChangeTypeCreated})
			}
			if hasKeyspaceListener {
				session.schemaListeners.OnKeyspaceCreated(OnKeyspaceCreatedEvent{Keyspace: keyspaces[name].Clone()})
			}
		}

		for _, name := range droppedKeyspaces {
			if !supportsRefresh {
				session.policy.KeyspaceChanged(KeyspaceUpdateEvent{Keyspace: name, Change: SchemaChangeTypeDropped})
			}
			if hasKeyspaceListener {
				session.schemaListeners.OnKeyspaceDropped(OnKeyspaceDroppedEvent{Keyspace: oldKeyspaceMeta[name].Clone()})
			}
		}

		for _, name := range updatedKeyspaces {
			if !supportsRefresh {
				session.policy.KeyspaceChanged(KeyspaceUpdateEvent{Keyspace: name, Change: SchemaChangeTypeUpdated})
			}
			if hasKeyspaceListener {
				session.schemaListeners.OnKeyspaceUpdated(OnKeyspaceUpdatedEvent{
					Old: oldKeyspaceMeta[name].Clone(),
					New: keyspaces[name].Clone(),
				})
			}
		}
	}

	// If we have full metadata cache mode, notify the non-keyspace change listeners if they are set.
	if session.cfg.Metadata.CacheMode == Full &&
		session.schemaListeners.hasNonKeyspaceSchemaChangeListeners() &&
		sessionInitialized {
		start := time.Now()

		handleFullSchemaChanges(session, oldKeyspaceMeta, keyspaces)

		elapsed := time.Since(start)
		session.logger.Debug("Finished calling schema change listeners.",
			NewLogFieldInt("old_keyspaces_count", len(oldKeyspaceMeta)),
			NewLogFieldInt("new_keyspaces_count", len(keyspaces)),
			NewLogFieldString("duration", elapsed.String()),
		)
	}

	return nil
}

func (s *schemaDescriber) debounceRefreshSchemaMetadata() {
	s.schemaRefresher.debounce()
}

func (s *schemaDescriber) refreshSchemaMetadata() error {
	err, ok := <-s.schemaRefresher.refreshNow()
	if !ok {
		return errors.New("could not refresh the schema because stop was requested")
	}

	if err != nil {
		return fmt.Errorf("failed to refresh schema metadata: %w", err)
	}
	return nil
}

// getDroppedKeyspaces returns the list of keyspace names that existed in oldKeyspaces
// but do not exist in newKeyspaces (i.e., keyspaces that were dropped).
func getDroppedKeyspaces(oldKeyspaces, newKeyspaces map[string]*KeyspaceMetadata) []string {
	var dropped []string
	for keyspaceName := range oldKeyspaces {
		if _, exists := newKeyspaces[keyspaceName]; !exists {
			dropped = append(dropped, keyspaceName)
		}
	}
	return dropped
}

// "compiles" derived information about keyspace, table, and column metadata
// for a keyspace from the basic queried metadata objects returned by
// getKeyspaceMetadata, getTableMetadata, and getColumnMetadata respectively;
// Links the metadata objects together and derives the column composition of
// the partition key and clustering key for a table.
func compileMetadata(
	session *Session,
	keyspace *KeyspaceMetadata,
	tables []TableMetadata,
	columns []ColumnMetadata,
	functions []FunctionMetadata,
	aggregates []AggregateMetadata,
	uTypes []UserTypeMetadata,
	materializedViews []MaterializedViewMetadata,
) error {
	keyspace.Tables = make(map[string]*TableMetadata)
	for i := range tables {
		tables[i].Columns = make(map[string]*ColumnMetadata)

		keyspace.Tables[tables[i].Name] = &tables[i]
	}
	keyspace.Functions = make(map[string]*FunctionMetadata, len(functions))
	for i := range functions {
		keyspace.Functions[functions[i].Name] = &functions[i]
	}
	keyspace.Aggregates = make(map[string]*AggregateMetadata, len(aggregates))
	for i, _ := range aggregates {
		finalFunc := keyspace.Functions[aggregates[i].finalFunc]
		if finalFunc != nil {
			aggregates[i].FinalFunc = *finalFunc
		}
		stateFunc := keyspace.Functions[aggregates[i].stateFunc]
		if stateFunc != nil {
			aggregates[i].StateFunc = *stateFunc
		}
		keyspace.Aggregates[aggregates[i].Name] = &aggregates[i]
	}
	keyspace.UserTypes = make(map[string]*UserTypeMetadata, len(uTypes))
	for i := range uTypes {
		keyspace.UserTypes[uTypes[i].Name] = &uTypes[i]
	}
	keyspace.MaterializedViews = make(map[string]*MaterializedViewMetadata, len(materializedViews))
	for i, _ := range materializedViews {
		materializedViews[i].BaseTable = keyspace.Tables[materializedViews[i].baseTableName]
		keyspace.MaterializedViews[materializedViews[i].Name] = &materializedViews[i]
	}

	// add columns from the schema data
	var err error
	for i := range columns {
		col := &columns[i]
		// decode the validator for TypeInfo and order
		if col.ClusteringOrder != "" { // Cassandra 3.x+
			col.Type, err = session.types.typeInfoFromString(session.cfg.ProtoVersion, col.Validator)
			if err != nil {
				// we don't error out completely for unknown types because we didn't before
				// and the caller might not care about this type
				col.Type = unknownTypeInfo(col.Validator)
			}
			col.Order = ASC
			if col.ClusteringOrder == "desc" {
				col.Order = DESC
			}
		} else {
			validatorParsed, err := parseType(session, col.Validator)
			if err != nil {
				return fmt.Errorf("failed to parse column validator type for column %s.%s: %w", col.Table, col.Name, err)
			}
			col.Type = validatorParsed.types[0]
			col.Order = ASC
			if validatorParsed.reversed[0] {
				col.Order = DESC
			}
		}

		table, ok := keyspace.Tables[col.Table]
		if !ok {
			// if the schema is being updated we will race between seeing
			// the metadata be complete. Potentially we should check for
			// schema versions before and after reading the metadata and
			// if they dont match try again.
			continue
		}

		table.Columns[col.Name] = col
		table.OrderedColumns = append(table.OrderedColumns, col.Name)
	}

	return compileV2Metadata(tables, session)
}

// The simpler compile case for V2+ protocol
func compileV2Metadata(tables []TableMetadata, session *Session) error {
	for i := range tables {
		table := &tables[i]

		clusteringColumnCount := componentColumnCountOfType(table.Columns, ColumnClusteringKey)
		table.ClusteringColumns = make([]*ColumnMetadata, clusteringColumnCount)

		if table.KeyValidator != "" {
			keyValidatorParsed, err := parseType(session, table.KeyValidator)
			if err != nil {
				return fmt.Errorf("failed to parse key validator type for table %s.%s: %w", table.Keyspace, table.Name, err)
			}
			table.PartitionKey = make([]*ColumnMetadata, len(keyValidatorParsed.types))
		} else { // Cassandra 3.x+
			partitionKeyCount := componentColumnCountOfType(table.Columns, ColumnPartitionKey)
			table.PartitionKey = make([]*ColumnMetadata, partitionKeyCount)
		}

		for _, columnName := range table.OrderedColumns {
			column := table.Columns[columnName]
			if column.Kind == ColumnPartitionKey {
				table.PartitionKey[column.ComponentIndex] = column
			} else if column.Kind == ColumnClusteringKey {
				table.ClusteringColumns[column.ComponentIndex] = column
			}
		}
	}
	return nil
}

// returns the count of coluns with the given "kind" value.
func componentColumnCountOfType(columns map[string]*ColumnMetadata, kind ColumnKind) int {
	maxComponentIndex := -1
	for _, column := range columns {
		if column.Kind == kind && column.ComponentIndex > maxComponentIndex {
			maxComponentIndex = column.ComponentIndex
		}
	}
	return maxComponentIndex + 1
}

// query only for the keyspace metadata for the specified keyspace from system.schema_keyspace
func getKeyspaceMetadata(session *Session, keyspaceName string) (*KeyspaceMetadata, error) {
	var stmt string

	if session.useSystemSchema { // Cassandra 3.x+
		stmt = `
		SELECT keyspace_name, durable_writes, replication
		FROM system_schema.keyspaces
		WHERE keyspace_name = ?`

	} else {
		stmt = `
		SELECT keyspace_name, durable_writes, strategy_class, strategy_options
		FROM system.schema_keyspaces
		WHERE keyspace_name = ?`
	}
	iter := session.control.query(stmt, keyspaceName)
	if iter.NumRows() == 0 {
		return nil, ErrKeyspaceDoesNotExist
	}
	keyspaces, err := getKeyspaceMetadataFromIter(session, iter)
	if err != nil {
		return nil, err
	}
	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("error querying keyspaces schema: %v", err)
	}
	return keyspaces[keyspaceName], nil
}

// query for the keyspace metadata for all keyspaces from system.schema_keyspaces
func getAllKeyspaceMetadata(session *Session) (map[string]*KeyspaceMetadata, error) {
	var stmt string
	if session.useSystemSchema { // Cassandra 3.x+
		stmt = `
		SELECT keyspace_name, durable_writes, replication
		FROM system_schema.keyspaces`

	} else {
		stmt = `
		SELECT keyspace_name, durable_writes, strategy_class, strategy_options
		FROM system.schema_keyspaces`
	}
	iter := session.control.query(stmt)

	keyspaces, err := getKeyspaceMetadataFromIter(session, iter)
	if err != nil {
		return nil, err
	}
	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("error querying keyspaces schema: %v", err)
	}

	return keyspaces, nil
}

func getKeyspaceMetadataFromIter(session *Session, iter *Iter) (map[string]*KeyspaceMetadata, error) {
	keyspaces := make(map[string]*KeyspaceMetadata)
	if session.useSystemSchema { // Cassandra 3.x+

		var (
			keyspaceName  string
			durableWrites bool
			replication   map[string]string
		)

		for iter.Scan(&keyspaceName, &durableWrites, &replication) {
			keyspace := &KeyspaceMetadata{
				Name:          keyspaceName,
				DurableWrites: durableWrites,
			}

			keyspace.StrategyClass = replication["class"]
			delete(replication, "class")

			keyspace.StrategyOptions = make(map[string]interface{}, len(replication))
			for k, v := range replication {
				keyspace.StrategyOptions[k] = v
			}
			keyspace.placementStrategy = getStrategy(keyspace, session.logger)
			keyspaces[keyspaceName] = keyspace

		}
	} else {
		var (
			keyspaceName        string
			durableWrites       bool
			strategyClass       string
			strategyOptionsJSON []byte
		)
		for iter.Scan(&keyspaceName, &durableWrites, &strategyClass, &strategyOptionsJSON) {
			keyspace := &KeyspaceMetadata{
				Name:          keyspaceName,
				DurableWrites: durableWrites,
				StrategyClass: strategyClass,
			}

			err := json.Unmarshal(strategyOptionsJSON, &keyspace.StrategyOptions)
			if err != nil {
				return nil, fmt.Errorf(
					"invalid JSON value '%s' as strategy_options for in keyspace '%s': %v",
					strategyOptionsJSON, keyspace.Name, err,
				)
			}

			keyspace.placementStrategy = getStrategy(keyspace, session.logger)
			keyspaces[keyspaceName] = keyspace
		}
	}

	return keyspaces, nil
}

// query for only the table metadata in the specified keyspace from system.schema_columnfamilies
func getTableMetadata(session *Session, keyspaceName string) ([]TableMetadata, error) {

	var (
		iter *Iter
		scan func(iter *Iter, table *TableMetadata) bool
		stmt string
	)

	if session.useSystemSchema { // Cassandra 3.x+
		stmt = `
		SELECT
			table_name
		FROM system_schema.tables
		WHERE keyspace_name = ?`

		switchIter := func() *Iter {
			iter.Close()
			stmt = `
				SELECT
					view_name
				FROM system_schema.views
				WHERE keyspace_name = ?`
			iter = session.control.query(stmt, keyspaceName)
			return iter
		}

		scan = func(iter *Iter, table *TableMetadata) bool {
			r := iter.Scan(
				&table.Name,
			)
			if !r {
				iter = switchIter()
				if iter != nil {
					switchIter = func() *Iter { return nil }
					r = iter.Scan(&table.Name)
				}
			}
			return r
		}
	} else {
		stmt = `
		SELECT
			columnfamily_name,
			key_validator,
			comparator,
			default_validator
		FROM system.schema_columnfamilies
		WHERE keyspace_name = ?`

		scan = func(iter *Iter, table *TableMetadata) bool {
			return iter.Scan(
				&table.Name,
				&table.KeyValidator,
				&table.Comparator,
				&table.DefaultValidator,
			)
		}
	}

	iter = session.control.query(stmt, keyspaceName)

	tables := []TableMetadata{}
	table := TableMetadata{Keyspace: keyspaceName}

	for scan(iter, &table) {
		tables = append(tables, table)
		table = TableMetadata{Keyspace: keyspaceName}
	}

	err := iter.Close()
	if err != nil && err != ErrNotFound {
		return nil, fmt.Errorf("error querying table schema: %v", err)
	}

	return tables, nil
}

// query for the table metadata for all tables from system.schema_columnfamilies
func getAllTablesMetadata(session *Session) (map[string][]TableMetadata, error) {
	var (
		iter *Iter
		scan func(iter *Iter, table *TableMetadata) bool
		stmt string
	)
	if session.useSystemSchema { // Cassandra 3.x+
		stmt = `
		SELECT
			keyspace_name, table_name
		FROM system_schema.tables`

		switchIter := func() *Iter {
			iter.Close()
			stmt = `
				SELECT
					keyspace_name, view_name
				FROM system_schema.views`
			iter = session.control.query(stmt)
			return iter
		}
		scan = func(iter *Iter, table *TableMetadata) bool {
			r := iter.Scan(
				&table.Keyspace,
				&table.Name,
			)
			if !r {
				iter = switchIter()
				if iter != nil {
					switchIter = func() *Iter { return nil }
					r = iter.Scan(&table.Keyspace, &table.Name)
				}
			}
			return r
		}
	} else {
		stmt = `
		SELECT
		    keyspace_name,
			columnfamily_name,
			key_validator,
			comparator,
			default_validator
		FROM system.schema_columnfamilies`

		scan = func(iter *Iter, table *TableMetadata) bool {
			return iter.Scan(
				&table.Keyspace,
				&table.Name,
				&table.KeyValidator,
				&table.Comparator,
				&table.DefaultValidator,
			)
		}
	}
	iter = session.control.query(stmt)
	tablesByKeyspace := make(map[string][]TableMetadata)
	table := TableMetadata{}
	for scan(iter, &table) {
		tablesByKeyspace[table.Keyspace] = append(tablesByKeyspace[table.Keyspace], table)
		table = TableMetadata{}
	}
	err := iter.Close()
	if err != nil && err != ErrNotFound {
		return nil, fmt.Errorf("error querying table schema: %v", err)
	}

	return tablesByKeyspace, nil
}

func (s *Session) scanColumnMetadataV1(keyspace string) ([]ColumnMetadata, error) {
	const stmt = `
		SELECT
		    	keyspace_name,
				columnfamily_name,
				column_name,
				component_index,
				validator,
				index_name,
				index_type,
				index_options
			FROM system.schema_columns
			WHERE keyspace_name = ?`
	itr := s.control.query(stmt, keyspace)
	columns, err := scanColumnMetadataV1FromIter(itr)
	if err != nil {
		return nil, err
	}

	return columns[keyspace], nil
}

func (s *Session) scanAllColumnMetadataV1() (map[string][]ColumnMetadata, error) {
	const stmt = `
		SELECT
		    	keyspace_name,
				columnfamily_name,
				column_name,
				component_index,
				validator,
				index_name,
				index_type,
				index_options
			FROM system.schema_columns`
	itr := s.control.query(stmt)
	return scanColumnMetadataV1FromIter(itr)
}

func scanColumnMetadataV1FromIter(iter *Iter) (map[string][]ColumnMetadata, error) {
	// V1 does not support the type column, and all returned rows are
	// of kind "regular".

	var columns = make(map[string][]ColumnMetadata)

	rows := iter.Scanner()
	for rows.Next() {
		var (
			column           = ColumnMetadata{}
			indexOptionsJSON []byte
		)

		// all columns returned by V1 are regular
		column.Kind = ColumnRegular

		err := rows.Scan(&column.Keyspace,
			&column.Table,
			&column.Name,
			&column.ComponentIndex,
			&column.Validator,
			&column.Index.Name,
			&column.Index.Type,
			&indexOptionsJSON)

		if err != nil {
			return nil, err
		}

		if len(indexOptionsJSON) > 0 {
			err := json.Unmarshal(indexOptionsJSON, &column.Index.Options)
			if err != nil {
				return nil, fmt.Errorf(
					"invalid JSON value '%s' as index_options for column '%s' in table '%s': %v",
					indexOptionsJSON,
					column.Name,
					column.Table,
					err)
			}
		}

		columns[column.Keyspace] = append(columns[column.Keyspace], column)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return columns, nil
}

func (s *Session) scanColumnMetadataV2(keyspace string) ([]ColumnMetadata, error) {
	// V2+ supports the type column
	const stmt = `
			SELECT
			    keyspace_name,
				columnfamily_name,
				column_name,
				component_index,
				validator,
				index_name,
				index_type,
				index_options,
				type
			FROM system.schema_columns
			WHERE keyspace_name = ?`

	iter := s.control.query(stmt, keyspace)
	columns, err := scanColumnMetadataV2FromIter(iter)
	if err != nil {
		return nil, err
	}
	return columns[keyspace], nil
}

func (s *Session) scanAllColumnMetadataV2() (map[string][]ColumnMetadata, error) {
	// V2+ supports the type column
	const stmt = `
			SELECT
			    keyspace_name,
				columnfamily_name,
				column_name,
				component_index,
				validator,
				index_name,
				index_type,
				index_options,
				type
			FROM system.schema_columns`

	iter := s.control.query(stmt)
	columns, err := scanColumnMetadataV2FromIter(iter)
	if err != nil {
		return nil, err
	}
	return columns, nil
}

func scanColumnMetadataV2FromIter(iter *Iter) (map[string][]ColumnMetadata, error) {
	rows := iter.Scanner()
	columns := make(map[string][]ColumnMetadata)
	for rows.Next() {
		var (
			column           = ColumnMetadata{}
			indexOptionsJSON []byte
		)

		err := rows.Scan(&column.Keyspace,
			&column.Table,
			&column.Name,
			&column.ComponentIndex,
			&column.Validator,
			&column.Index.Name,
			&column.Index.Type,
			&indexOptionsJSON,
			&column.Kind,
		)
		if err != nil {
			return nil, err
		}
		if len(indexOptionsJSON) > 0 {
			err := json.Unmarshal(indexOptionsJSON, &column.Index.Options)
			if err != nil {
				return nil, fmt.Errorf(
					"invalid JSON value '%s' as index_options for column '%s' in table '%s': %v",
					indexOptionsJSON,
					column.Name,
					column.Table,
					err)
			}
		}
		columns[column.Keyspace] = append(columns[column.Keyspace], column)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return columns, nil
}

func (s *Session) scanColumnMetadataSystem(keyspace string) ([]ColumnMetadata, error) {
	const stmt = `
			SELECT
			    keyspace_name,
				table_name,
				column_name,
				clustering_order,
				type,
				kind,
				position
			FROM system_schema.columns
			WHERE keyspace_name = ?`

	var iter = s.control.query(stmt, keyspace)

	columns, err := scanColumnMetadataSystemFromIter(iter)

	if err != nil {
		return nil, err
	}

	return columns[keyspace], nil
}

func (s *Session) scanAllColumnMetadataSystem() (map[string][]ColumnMetadata, error) {
	const stmt = `
			SELECT
			    keyspace_name,
				table_name,
				column_name,
				clustering_order,
				type,
				kind,
				position
			FROM system_schema.columns`

	var iter = s.control.query(stmt)

	columns, err := scanColumnMetadataSystemFromIter(iter)

	if err != nil {
		return nil, err
	}

	return columns, nil
}

func scanColumnMetadataSystemFromIter(iter *Iter) (map[string][]ColumnMetadata, error) {

	var columns = make(map[string][]ColumnMetadata)

	rows := iter.Scanner()
	for rows.Next() {
		column := ColumnMetadata{}

		err := rows.Scan(&column.Keyspace,
			&column.Table,
			&column.Name,
			&column.ClusteringOrder,
			&column.Validator,
			&column.Kind,
			&column.ComponentIndex,
		)

		if err != nil {
			return nil, err
		}

		columns[column.Keyspace] = append(columns[column.Keyspace], column)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// TODO(zariel): get column index info from system_schema.indexes

	return columns, nil
}

// query for only the column metadata in the specified keyspace from system.schema_columns
func getColumnMetadata(session *Session, keyspaceName string) ([]ColumnMetadata, error) {
	var (
		columns []ColumnMetadata
		err     error
	)

	// Deal with differences in protocol versions
	if session.cfg.ProtoVersion == 1 {
		columns, err = session.scanColumnMetadataV1(keyspaceName)
	} else if session.useSystemSchema { // Cassandra 3.x+
		columns, err = session.scanColumnMetadataSystem(keyspaceName)
	} else {
		columns, err = session.scanColumnMetadataV2(keyspaceName)
	}

	if err != nil && err != ErrNotFound {
		return nil, fmt.Errorf("error querying column schema: %v", err)
	}

	return columns, nil
}

// query for the column metadata for all keyspaces from system.schema_columns
func getAllColumnMetadata(session *Session) (map[string][]ColumnMetadata, error) {
	var (
		columns map[string][]ColumnMetadata
		err     error
	)

	// Deal with differences in protocol versions
	if session.cfg.ProtoVersion == 1 {
		columns, err = session.scanAllColumnMetadataV1()
	} else if session.useSystemSchema { // Cassandra 3.x+
		columns, err = session.scanAllColumnMetadataSystem()
	} else {
		columns, err = session.scanAllColumnMetadataV2()
	}

	if err != nil && err != ErrNotFound {
		return nil, fmt.Errorf("error querying column schema: %v", err)
	}

	return columns, nil
}

func getUserTypeMetadata(session *Session, keyspaceName string) ([]UserTypeMetadata, error) {
	var tableName string
	if session.useSystemSchema {
		tableName = "system_schema.types"
	} else {
		tableName = "system.schema_usertypes"
	}
	stmt := fmt.Sprintf(`
		SELECT
			keyspace_name,
			type_name,
			field_names,
			field_types
		FROM %s
		WHERE keyspace_name = ?`, tableName)

	iter := session.control.query(stmt, keyspaceName)
	uTypes, err := getUserTypeMetadataFromIter(session, iter)

	if err != nil {
		return nil, err
	}

	return uTypes[keyspaceName], nil
}

func getAllUserTypeMetadata(session *Session) (map[string][]UserTypeMetadata, error) {
	var tableName string
	if session.useSystemSchema {
		tableName = "system_schema.types"
	} else {
		tableName = "system.schema_usertypes"
	}
	stmt := fmt.Sprintf(`
		SELECT
			keyspace_name,
			type_name,
			field_names,
			field_types
		FROM %s`, tableName)

	iter := session.control.query(stmt)
	uTypes, err := getUserTypeMetadataFromIter(session, iter)

	if err != nil {
		return nil, err
	}

	return uTypes, nil
}

func getUserTypeMetadataFromIter(session *Session, iter *Iter) (map[string][]UserTypeMetadata, error) {
	uTypes := make(map[string][]UserTypeMetadata)
	rows := iter.Scanner()
	for rows.Next() {
		uType := UserTypeMetadata{}
		err := rows.Scan(&uType.Keyspace,
			&uType.Name,
			&uType.FieldNames,
			&uType.fieldTypesRaw,
		)
		if err != nil {
			return nil, err
		}
		uType.FieldTypes = make([]TypeInfo, len(uType.fieldTypesRaw))
		for i, argumentType := range uType.fieldTypesRaw {
			uType.FieldTypes[i], err = session.types.typeInfoFromString(session.cfg.ProtoVersion, argumentType)
			if err != nil {
				// we don't error out completely for unknown types because we didn't before
				// and the caller might not care about this type
				uType.FieldTypes[i] = unknownTypeInfo(argumentType)
			}
		}
		uTypes[uType.Keyspace] = append(uTypes[uType.Keyspace], uType)
	}
	// TODO: if a UDT refers to another UDT, should we resolve it?

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return uTypes, nil
}

func bytesMapToStringsMap(byteData map[string][]byte) map[string]string {
	extensions := make(map[string]string, len(byteData))
	for key, rowByte := range byteData {
		extensions[key] = string(rowByte)
	}

	return extensions
}

func materializedViewMetadataFromMap(currentObject map[string]interface{}, materializedView *MaterializedViewMetadata) error {
	const errorMessage = "gocql.materializedViewMetadataFromMap failed to read column %s"
	var ok bool
	for key, value := range currentObject {
		switch key {
		case "keyspace_name":
			materializedView.Keyspace, ok = value.(string)
			if !ok {
				return fmt.Errorf(errorMessage, key)
			}

		case "view_name":
			materializedView.Name, ok = value.(string)
			if !ok {
				return fmt.Errorf(errorMessage, key)
			}

		case "additional_write_policy":
			materializedView.AdditionalWritePolicy, ok = value.(string)
			if !ok {
				return fmt.Errorf(errorMessage, key)
			}

		case "base_table_id":
			materializedView.BaseTableId, ok = value.(UUID)
			if !ok {
				return fmt.Errorf(errorMessage, key)
			}

		case "base_table_name":
			materializedView.baseTableName, ok = value.(string)
			if !ok {
				return fmt.Errorf(errorMessage, key)
			}

		case "bloom_filter_fp_chance":
			materializedView.BloomFilterFpChance, ok = value.(float64)
			if !ok {
				return fmt.Errorf(errorMessage, key)
			}

		case "caching":
			materializedView.Caching, ok = value.(map[string]string)
			if !ok {
				return fmt.Errorf(errorMessage, key)
			}

		case "comment":
			materializedView.Comment, ok = value.(string)
			if !ok {
				return fmt.Errorf(errorMessage, key)
			}

		case "compaction":
			materializedView.Compaction, ok = value.(map[string]string)
			if !ok {
				return fmt.Errorf(errorMessage, key)
			}

		case "compression":
			materializedView.Compression, ok = value.(map[string]string)
			if !ok {
				return fmt.Errorf(errorMessage, key)
			}

		case "crc_check_chance":
			materializedView.CrcCheckChance, ok = value.(float64)
			if !ok {
				return fmt.Errorf(errorMessage, key)
			}

		case "dclocal_read_repair_chance":
			materializedView.DcLocalReadRepairChance, ok = value.(float64)
			if !ok {
				return fmt.Errorf(errorMessage, key)
			}

		case "default_time_to_live":
			materializedView.DefaultTimeToLive, ok = value.(int)
			if !ok {
				return fmt.Errorf(errorMessage, key)
			}

		case "extensions":
			byteData, ok := value.(map[string][]byte)
			if !ok {
				return fmt.Errorf(errorMessage, key)
			}

			materializedView.Extensions = bytesMapToStringsMap(byteData)

		case "gc_grace_seconds":
			materializedView.GcGraceSeconds, ok = value.(int)
			if !ok {
				return fmt.Errorf(errorMessage, key)
			}

		case "id":
			materializedView.Id, ok = value.(UUID)
			if !ok {
				return fmt.Errorf(errorMessage, key)
			}

		case "include_all_columns":
			materializedView.IncludeAllColumns, ok = value.(bool)
			if !ok {
				return fmt.Errorf(errorMessage, key)
			}

		case "max_index_interval":
			materializedView.MaxIndexInterval, ok = value.(int)
			if !ok {
				return fmt.Errorf(errorMessage, key)
			}

		case "memtable_flush_period_in_ms":
			materializedView.MemtableFlushPeriodInMs, ok = value.(int)
			if !ok {
				return fmt.Errorf(errorMessage, key)
			}

		case "min_index_interval":
			materializedView.MinIndexInterval, ok = value.(int)
			if !ok {
				return fmt.Errorf(errorMessage, key)
			}

		case "read_repair":
			materializedView.ReadRepair, ok = value.(string)
			if !ok {
				return fmt.Errorf(errorMessage, key)
			}

		case "read_repair_chance":
			materializedView.ReadRepairChance, ok = value.(float64)
			if !ok {
				return fmt.Errorf(errorMessage, key)
			}

		case "speculative_retry":
			materializedView.SpeculativeRetry, ok = value.(string)
			if !ok {
				return fmt.Errorf(errorMessage, key)
			}

		}
	}
	return nil
}

func parseSystemSchemaViews(iter *Iter) (map[string][]MaterializedViewMetadata, error) {
	var materializedViews = make(map[string][]MaterializedViewMetadata)
	numCols := len(iter.Columns())
	for {
		row := make(map[string]interface{}, numCols)
		if !iter.MapScan(row) {
			break
		}
		var materializedView MaterializedViewMetadata
		err := materializedViewMetadataFromMap(row, &materializedView)
		if err != nil {
			return nil, err
		}

		materializedViews[materializedView.Keyspace] = append(materializedViews[materializedView.Keyspace], materializedView)
	}
	if iter.err != nil {
		return nil, iter.err
	}
	return materializedViews, nil
}

func getMaterializedViewsMetadata(session *Session, keyspaceName string) ([]MaterializedViewMetadata, error) {
	if !session.useSystemSchema {
		return nil, nil
	}
	var tableName = "system_schema.views"
	stmt := fmt.Sprintf(`
		SELECT *
		FROM %s
		WHERE keyspace_name = ?`, tableName)

	var materializedViews map[string][]MaterializedViewMetadata

	iter := session.control.query(stmt, keyspaceName)

	materializedViews, err := parseSystemSchemaViews(iter)
	if err != nil {
		return nil, err
	}

	return materializedViews[keyspaceName], nil
}

func getAllMaterializedViewsMetadata(session *Session) (map[string][]MaterializedViewMetadata, error) {
	if !session.useSystemSchema {
		return nil, nil
	}
	var tableName = "system_schema.views"
	stmt := fmt.Sprintf(`
		SELECT *
		FROM %s`, tableName)

	var materializedViews map[string][]MaterializedViewMetadata

	iter := session.control.query(stmt)

	materializedViews, err := parseSystemSchemaViews(iter)
	if err != nil {
		return nil, err
	}

	return materializedViews, nil
}

func getFunctionsMetadata(session *Session, keyspaceName string) ([]FunctionMetadata, error) {
	if !session.hasAggregatesAndFunctions {
		return nil, nil
	}
	var tableName string
	if session.useSystemSchema {
		tableName = "system_schema.functions"
	} else {
		tableName = "system.schema_functions"
	}
	stmt := fmt.Sprintf(`
		SELECT
		    keyspace_name,
			function_name,
			argument_types,
			argument_names,
			body,
			called_on_null_input,
			language,
			return_type
		FROM %s
		WHERE keyspace_name = ?`, tableName)

	iter := session.control.query(stmt, keyspaceName)
	functions, err := getFunctionsMetadataFromIter(session, iter)

	if err != nil {
		return nil, err
	}

	return functions[keyspaceName], nil
}

func getAllFunctionsMetadata(session *Session) (map[string][]FunctionMetadata, error) {
	if !session.hasAggregatesAndFunctions {
		return nil, nil
	}
	var tableName string
	if session.useSystemSchema {
		tableName = "system_schema.functions"
	} else {
		tableName = "system.schema_functions"
	}
	stmt := fmt.Sprintf(`
		SELECT
		    keyspace_name,
			function_name,
			argument_types,
			argument_names,
			body,
			called_on_null_input,
			language,
			return_type
		FROM %s`, tableName)

	iter := session.control.query(stmt)
	functions, err := getFunctionsMetadataFromIter(session, iter)

	if err != nil {
		return nil, err
	}

	return functions, nil
}

func getFunctionsMetadataFromIter(session *Session, iter *Iter) (map[string][]FunctionMetadata, error) {
	functions := make(map[string][]FunctionMetadata)
	rows := iter.Scanner()
	for rows.Next() {
		function := FunctionMetadata{}
		err := rows.Scan(&function.Keyspace,
			&function.Name,
			&function.argumentTypesRaw,
			&function.ArgumentNames,
			&function.Body,
			&function.CalledOnNullInput,
			&function.Language,
			&function.returnTypeRaw,
		)
		if err != nil {
			return nil, err
		}
		function.ReturnType, err = session.types.typeInfoFromString(session.cfg.ProtoVersion, function.returnTypeRaw)
		if err != nil {
			// we don't error out completely for unknown types because we didn't before
			// and the caller might not care about this type
			function.ReturnType = unknownTypeInfo(function.returnTypeRaw)
		}
		function.ArgumentTypes = make([]TypeInfo, len(function.argumentTypesRaw))
		for i, argumentType := range function.argumentTypesRaw {
			function.ArgumentTypes[i], err = session.types.typeInfoFromString(session.cfg.ProtoVersion, argumentType)
			if err != nil {
				// we don't error out completely for unknown types because we didn't before
				// and the caller might not care about this type
				function.ArgumentTypes[i] = unknownTypeInfo(argumentType)
			}
		}
		functions[function.Keyspace] = append(functions[function.Keyspace], function)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return functions, nil
}

func getAggregatesMetadata(session *Session, keyspaceName string) ([]AggregateMetadata, error) {
	if !session.hasAggregatesAndFunctions {
		return nil, nil
	}
	var tableName string
	if session.useSystemSchema {
		tableName = "system_schema.aggregates"
	} else {
		tableName = "system.schema_aggregates"
	}

	stmt := fmt.Sprintf(`
		SELECT
			keyspace_name,
			aggregate_name,
			argument_types,
			final_func,
			initcond,
			return_type,
			state_func,
			state_type
		FROM %s
		WHERE keyspace_name = ?`, tableName)

	iter := session.control.query(stmt, keyspaceName)
	aggregates, err := getAggregatesMetadataFromIter(session, iter)

	if err != nil {
		return nil, err
	}

	return aggregates[keyspaceName], nil
}

func getAllAggregatesMetadata(session *Session) (map[string][]AggregateMetadata, error) {
	if !session.hasAggregatesAndFunctions {
		return nil, nil
	}
	var tableName string
	if session.useSystemSchema {
		tableName = "system_schema.aggregates"
	} else {
		tableName = "system.schema_aggregates"
	}

	stmt := fmt.Sprintf(`
		SELECT
			keyspace_name,
			aggregate_name,
			argument_types,
			final_func,
			initcond,
			return_type,
			state_func,
			state_type
		FROM %s`, tableName)

	iter := session.control.query(stmt)
	aggregates, err := getAggregatesMetadataFromIter(session, iter)

	if err != nil {
		return nil, err
	}

	return aggregates, nil
}

func getAggregatesMetadataFromIter(session *Session, iter *Iter) (map[string][]AggregateMetadata, error) {
	aggregates := make(map[string][]AggregateMetadata)
	rows := iter.Scanner()
	for rows.Next() {
		aggregate := AggregateMetadata{}
		err := rows.Scan(&aggregate.Keyspace,
			&aggregate.Name,
			&aggregate.argumentTypesRaw,
			&aggregate.finalFunc,
			&aggregate.InitCond,
			&aggregate.returnTypeRaw,
			&aggregate.stateFunc,
			&aggregate.stateTypeRaw,
		)
		if err != nil {
			return nil, err
		}
		aggregate.ReturnType, err = session.types.typeInfoFromString(session.cfg.ProtoVersion, aggregate.returnTypeRaw)
		if err != nil {
			// we don't error out completely for unknown types because we didn't before
			// and the caller might not care about this type
			aggregate.ReturnType = unknownTypeInfo(aggregate.returnTypeRaw)
		}
		aggregate.StateType, err = session.types.typeInfoFromString(session.cfg.ProtoVersion, aggregate.stateTypeRaw)
		if err != nil {
			// we don't error out completely for unknown types because we didn't before
			// and the caller might not care about this type
			aggregate.StateType = unknownTypeInfo(aggregate.stateTypeRaw)
		}
		aggregate.ArgumentTypes = make([]TypeInfo, len(aggregate.argumentTypesRaw))
		for i, argumentType := range aggregate.argumentTypesRaw {
			aggregate.ArgumentTypes[i], err = session.types.typeInfoFromString(session.cfg.ProtoVersion, argumentType)
			if err != nil {
				// we don't error out completely for unknown types because we didn't before
				// and the caller might not care about this type
				aggregate.ArgumentTypes[i] = unknownTypeInfo(argumentType)
			}
		}
		aggregates[aggregate.Keyspace] = append(aggregates[aggregate.Keyspace], aggregate)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return aggregates, nil
}

// type definition parser state
type typeParser struct {
	input   string
	index   int
	session *Session
}

// the type definition parser result
type typeParserResult struct {
	isComposite bool
	types       []TypeInfo
	reversed    []bool
	collections map[string]TypeInfo
}

// Parse the type definition used for validator and comparator schema data
func parseType(session *Session, def string) (typeParserResult, error) {
	parser := &typeParser{
		input:   def,
		session: session,
	}
	res, ok, err := parser.parse()
	if err != nil {
		return typeParserResult{}, err
	}
	if !ok {
		t, err := session.types.typeInfoFromString(session.cfg.ProtoVersion, def)
		if err != nil {
			// we don't error out completely for unknown types because we didn't before
			// and the caller might not care about this type
			t = unknownTypeInfo(def)
		}
		// treat this is a custom type
		return typeParserResult{
			isComposite: false,
			types:       []TypeInfo{t},
			reversed:    []bool{false},
			collections: nil,
		}, nil
	}
	return res, err
}

const (
	REVERSED_TYPE   = "org.apache.cassandra.db.marshal.ReversedType"
	COMPOSITE_TYPE  = "org.apache.cassandra.db.marshal.CompositeType"
	COLLECTION_TYPE = "org.apache.cassandra.db.marshal.ColumnToCollectionType"
	VECTOR_TYPE     = "org.apache.cassandra.db.marshal.VectorType"
)

// represents a class specification in the type def AST
type typeParserClassNode struct {
	name   string
	params []typeParserParamNode
	// this is the segment of the input string that defined this node
	input   string
	session *Session
}

// represents a class parameter in the type def AST
type typeParserParamNode struct {
	name  *string
	class typeParserClassNode
}

func (t *typeParser) parse() (typeParserResult, bool, error) {
	// parse the AST
	ast, ok := t.parseClassNode()
	if !ok {
		return typeParserResult{}, false, nil
	}

	var err error
	// interpret the AST
	if strings.HasPrefix(ast.name, COMPOSITE_TYPE) {
		count := len(ast.params)

		// look for a collections param
		last := ast.params[count-1]
		collections := map[string]TypeInfo{}
		if strings.HasPrefix(last.class.name, COLLECTION_TYPE) {
			count--

			for _, param := range last.class.params {
				decoded, err := hex.DecodeString(*param.name)
				if err != nil {
					return typeParserResult{}, false, fmt.Errorf("type '%s' contains collection name '%s' with an invalid format: %w", t.input, *param.name, err)
				}
				collections[string(decoded)], err = param.class.asTypeInfo()
				if err != nil {
					return typeParserResult{}, false, err
				}
			}
		}

		types := make([]TypeInfo, count)
		reversed := make([]bool, count)

		for i, param := range ast.params[:count] {
			class := param.class
			reversed[i] = strings.HasPrefix(class.name, REVERSED_TYPE)
			if reversed[i] {
				class = class.params[0].class
			}
			types[i], err = class.asTypeInfo()
			if err != nil {
				return typeParserResult{}, false, err
			}
		}

		return typeParserResult{
			isComposite: true,
			types:       types,
			reversed:    reversed,
			collections: collections,
		}, true, nil
	} else {
		// not composite, so one type
		class := *ast
		reversed := strings.HasPrefix(class.name, REVERSED_TYPE)
		if reversed {
			class = class.params[0].class
		}
		typeInfo, err := class.asTypeInfo()
		if err != nil {
			return typeParserResult{}, false, err
		}

		return typeParserResult{
			isComposite: false,
			types:       []TypeInfo{typeInfo},
			reversed:    []bool{reversed},
		}, true, nil
	}
}

func (class *typeParserClassNode) asTypeInfo() (TypeInfo, error) {
	// TODO: should we just use types.typeInfoFromString(class.input) but then it
	// wouldn't be reversed
	t, ok := class.session.types.getType(class.name)
	if !ok {
		return unknownTypeInfo(class.input), nil
	}
	var params string
	if len(class.params) > 0 {
		// compile the params just like they are in 3.x
		for i, param := range class.params {
			if i > 0 {
				params += ", "
			}
			if param.name != nil {
				params += (*param.name) + ":"
			}
			params += param.class.name
		}
	}
	// we are returning the error here because we're failing to parse it, but if
	// this ends up unnecessarily breaking things we could do the same thing as
	// above and return unknownTypeInfo
	return t.TypeInfoFromString(class.session.cfg.ProtoVersion, params)
}

// CLASS := ID [ PARAMS ]
func (t *typeParser) parseClassNode() (node *typeParserClassNode, ok bool) {
	t.skipWhitespace()

	startIndex := t.index

	name, ok := t.nextIdentifier()
	if !ok {
		return nil, false
	}

	params, ok := t.parseParamNodes()
	if !ok {
		return nil, false
	}

	endIndex := t.index

	node = &typeParserClassNode{
		name:    name,
		params:  params,
		input:   t.input[startIndex:endIndex],
		session: t.session,
	}
	return node, true
}

// PARAMS := "(" PARAM { "," PARAM } ")"
// PARAM := [ PARAM_NAME ":" ] CLASS
// PARAM_NAME := ID
func (t *typeParser) parseParamNodes() (params []typeParserParamNode, ok bool) {
	t.skipWhitespace()

	// the params are optional
	if t.index == len(t.input) || t.input[t.index] != '(' {
		return nil, true
	}

	params = []typeParserParamNode{}

	// consume the '('
	t.index++

	t.skipWhitespace()

	for t.input[t.index] != ')' {
		// look for a named param, but if no colon, then we want to backup
		backupIndex := t.index

		// name will be a hex encoded version of a utf-8 string
		name, ok := t.nextIdentifier()
		if !ok {
			return nil, false
		}
		hasName := true

		// TODO handle '=>' used for DynamicCompositeType

		t.skipWhitespace()

		if t.input[t.index] == ':' {
			// there is a name for this parameter

			// consume the ':'
			t.index++

			t.skipWhitespace()
		} else {
			// no name, backup
			hasName = false
			t.index = backupIndex
		}

		// parse the next full parameter
		classNode, ok := t.parseClassNode()
		if !ok {
			return nil, false
		}

		if hasName {
			params = append(
				params,
				typeParserParamNode{name: &name, class: *classNode},
			)
		} else {
			params = append(
				params,
				typeParserParamNode{class: *classNode},
			)
		}

		t.skipWhitespace()

		if t.input[t.index] == ',' {
			// consume the comma
			t.index++

			t.skipWhitespace()
		}
	}

	// consume the ')'
	t.index++

	return params, true
}

func (t *typeParser) skipWhitespace() {
	for t.index < len(t.input) && isWhitespaceChar(t.input[t.index]) {
		t.index++
	}
}

func isWhitespaceChar(c byte) bool {
	return c == ' ' || c == '\n' || c == '\t'
}

// ID := LETTER { LETTER }
// LETTER := "0"..."9" | "a"..."z" | "A"..."Z" | "-" | "+" | "." | "_" | "&"
func (t *typeParser) nextIdentifier() (id string, found bool) {
	startIndex := t.index
	for t.index < len(t.input) && isIdentifierChar(t.input[t.index]) {
		t.index++
	}
	if startIndex == t.index {
		return "", false
	}
	return t.input[startIndex:t.index], true
}

func isIdentifierChar(c byte) bool {
	return (c >= '0' && c <= '9') ||
		(c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		c == '-' ||
		c == '+' ||
		c == '.' ||
		c == '_' ||
		c == '&'
}

// Handles the full schema changes including table, user type, function, aggregate.
func handleFullSchemaChanges(session *Session, oldKeyspaces, newKeyspaces map[string]*KeyspaceMetadata) {
	for _, oldKsMeta := range oldKeyspaces {
		newKsMeta, ok := newKeyspaces[oldKsMeta.Name]
		if !ok {
			// Skip, KeyspaceChangeListener is already notified in refreshSchemas()
			continue
		}

		if session.schemaListeners.hasTable() {
			handleSchemaTableChanges(session, oldKsMeta, newKsMeta)
		}
		if session.schemaListeners.hasUserType() {
			handleSchemaUserTypeChanges(session, oldKsMeta, newKsMeta)
		}
		if session.schemaListeners.hasFunction() {
			handleSchemaFunctionChanges(session, oldKsMeta, newKsMeta)
		}
		if session.schemaListeners.hasAggregate() {
			handleSchemaAggregateChanges(session, oldKsMeta, newKsMeta)
		}
	}
}

// Computes the schema table changes and notifies the event listener if it is set.
// It is expected that the TableChangeListener is set on the Session.
func handleSchemaTableChanges(session *Session, oldKeyspace, newKeyspace *KeyspaceMetadata) {
	var createdEvents []OnTableCreatedEvent
	var droppedEvents []OnTableDroppedEvent
	var updatedEvents []OnTableUpdatedEvent

	for _, oldTableMeta := range oldKeyspace.Tables {
		newTableMeta, ok := newKeyspace.Tables[oldTableMeta.Name]
		if !ok {
			droppedEvents = append(droppedEvents, OnTableDroppedEvent{Table: oldTableMeta.Clone()})
		} else {
			if !compareTablesMetadata(oldTableMeta, newTableMeta) {
				updatedEvents = append(updatedEvents, OnTableUpdatedEvent{Old: oldTableMeta.Clone(), New: newTableMeta.Clone()})
			}
		}
	}

	for _, newTableMeta := range newKeyspace.Tables {
		_, ok := oldKeyspace.Tables[newTableMeta.Name]
		if !ok {
			createdEvents = append(createdEvents, OnTableCreatedEvent{Table: newTableMeta.Clone()})
		}
	}

	session.logger.Debug("Computed schema table change events",
		NewLogFieldInt("created_table_events_count", len(createdEvents)),
		NewLogFieldInt("updated_table_events_count", len(updatedEvents)),
		NewLogFieldInt("dropped_table_events_count", len(droppedEvents)),
	)

	for _, updatedEvent := range updatedEvents {
		session.schemaListeners.OnTableUpdated(updatedEvent)
	}

	for _, createdEvent := range createdEvents {
		session.schemaListeners.OnTableCreated(createdEvent)
	}

	for _, droppedEvent := range droppedEvents {
		session.schemaListeners.OnTableDropped(droppedEvent)
	}
}

// Computes the schema aggregate changes and notifies the event listener if it is set.
// It is expected that the AggregateChangeListener is set on the Session.
func handleSchemaAggregateChanges(session *Session, oldKeyspace, newKeyspace *KeyspaceMetadata) {
	var createdEvents []OnAggregateCreatedEvent
	var droppedEvents []OnAggregateDroppedEvent
	var updatedEvents []OnAggregateUpdatedEvent

	for _, oldAggregateMeta := range oldKeyspace.Aggregates {
		newAggregateMeta, ok := newKeyspace.Aggregates[oldAggregateMeta.Name]
		if !ok {
			droppedEvents = append(droppedEvents, OnAggregateDroppedEvent{Aggregate: oldAggregateMeta.Clone()})
		} else {
			if !compareAggregateMetadata(oldAggregateMeta, newAggregateMeta) {
				updatedEvents = append(updatedEvents, OnAggregateUpdatedEvent{Old: oldAggregateMeta.Clone(), New: newAggregateMeta.Clone()})
			}
		}
	}

	for _, newAggregateMeta := range newKeyspace.Aggregates {
		_, ok := oldKeyspace.Aggregates[newAggregateMeta.Name]
		if !ok {
			createdEvents = append(createdEvents, OnAggregateCreatedEvent{Aggregate: newAggregateMeta.Clone()})
		}
	}

	session.logger.Debug("Computed schema aggregate change events",
		NewLogFieldInt("created_aggregate_events_count", len(createdEvents)),
		NewLogFieldInt("updated_aggregate_events_count", len(updatedEvents)),
		NewLogFieldInt("dropped_aggregate_events_count", len(droppedEvents)),
	)

	for _, updatedEvent := range updatedEvents {
		session.schemaListeners.OnAggregateUpdated(updatedEvent)
	}

	for _, createdEvent := range createdEvents {
		session.schemaListeners.OnAggregateCreated(createdEvent)
	}

	for _, droppedEvent := range droppedEvents {
		session.schemaListeners.OnAggregateDropped(droppedEvent)
	}
}

// Computes the schema user type changes and notifies the event listener if it is set.
// It is expected that the UserTypeChangeListener is set on the Session.
func handleSchemaUserTypeChanges(session *Session, oldKeyspace, newKeyspace *KeyspaceMetadata) {
	var createdEvents []OnUserTypeCreatedEvent
	var droppedEvents []OnUserTypeDroppedEvent
	var updatedEvents []OnUserTypeUpdatedEvent

	for _, oldUserTypeMeta := range oldKeyspace.UserTypes {
		newUserTypeMeta, ok := newKeyspace.UserTypes[oldUserTypeMeta.Name]
		if !ok {
			droppedEvents = append(droppedEvents, OnUserTypeDroppedEvent{UserType: oldUserTypeMeta.Clone()})
		} else {
			if !compareUserTypeMetadata(oldUserTypeMeta, newUserTypeMeta) {
				updatedEvents = append(updatedEvents, OnUserTypeUpdatedEvent{Old: oldUserTypeMeta.Clone(), New: newUserTypeMeta.Clone()})
			}
		}
	}

	for _, newUserTypeMeta := range newKeyspace.UserTypes {
		_, ok := oldKeyspace.UserTypes[newUserTypeMeta.Name]
		if !ok {
			createdEvents = append(createdEvents, OnUserTypeCreatedEvent{UserType: newUserTypeMeta.Clone()})
		}
	}

	session.logger.Debug("Computed schema user type change events",
		NewLogFieldInt("created_user_type_events_count", len(createdEvents)),
		NewLogFieldInt("updated_user_type_events_count", len(updatedEvents)),
		NewLogFieldInt("dropped_user_type_events_count", len(droppedEvents)),
	)

	for _, updatedEvent := range updatedEvents {
		session.schemaListeners.OnUserTypeUpdated(updatedEvent)
	}

	for _, createdEvent := range createdEvents {
		session.schemaListeners.OnUserTypeCreated(createdEvent)
	}

	for _, droppedEvent := range droppedEvents {
		session.schemaListeners.OnUserTypeDropped(droppedEvent)
	}
}

// Computes the schema function changes and notifies the event listener if it is set.
// It is expected that the FunctionChangeListener is set on the Session.
func handleSchemaFunctionChanges(session *Session, oldKeyspace, newKeyspace *KeyspaceMetadata) {
	var createdEvents []OnFunctionCreatedEvent
	var droppedEvents []OnFunctionDroppedEvent
	var updatedEvents []OnFunctionUpdatedEvent

	for _, oldFunctionMeta := range oldKeyspace.Functions {
		newFunctionMeta, ok := newKeyspace.Functions[oldFunctionMeta.Name]
		if !ok {
			droppedEvents = append(droppedEvents, OnFunctionDroppedEvent{Function: oldFunctionMeta.Clone()})
		} else {
			if !compareFunctionMetadata(oldFunctionMeta, newFunctionMeta) {
				updatedEvents = append(updatedEvents, OnFunctionUpdatedEvent{Old: oldFunctionMeta.Clone(), New: newFunctionMeta.Clone()})
			}
		}
	}

	for _, newFunctionMeta := range newKeyspace.Functions {
		_, ok := oldKeyspace.Functions[newFunctionMeta.Name]
		if !ok {
			createdEvents = append(createdEvents, OnFunctionCreatedEvent{Function: newFunctionMeta.Clone()})
		}
	}

	session.logger.Debug("Computed schema function change events",
		NewLogFieldInt("created_function_events_count", len(createdEvents)),
		NewLogFieldInt("updated_function_events_count", len(updatedEvents)),
		NewLogFieldInt("dropped_function_events_count", len(droppedEvents)),
	)

	for _, updatedEvent := range updatedEvents {
		session.schemaListeners.OnFunctionUpdated(updatedEvent)
	}

	for _, createdEvent := range createdEvents {
		session.schemaListeners.OnFunctionCreated(createdEvent)
	}

	for _, droppedEvent := range droppedEvents {
		session.schemaListeners.OnFunctionDropped(droppedEvent)
	}
}

func compareTablesMetadata(tableA, tableB *TableMetadata) bool {
	if tableA == tableB {
		return true
	}

	return tableA != nil && tableB != nil &&
		tableA.Name == tableB.Name &&
		tableA.KeyValidator == tableB.KeyValidator &&
		tableA.Comparator == tableB.Comparator &&
		tableA.DefaultValidator == tableB.DefaultValidator &&
		tableA.ValueAlias == tableB.ValueAlias &&
		stringsSlicesEqual(tableA.KeyAliases, tableB.KeyAliases) &&
		stringsSlicesEqual(tableA.ColumnAliases, tableB.ColumnAliases) &&
		stringsSlicesEqual(tableA.OrderedColumns, tableB.OrderedColumns) &&
		compareColumnMetadataSlices(tableA.PartitionKey, tableB.PartitionKey) &&
		compareColumnMetadataSlices(tableA.ClusteringColumns, tableB.ClusteringColumns) &&
		columnsEqual(tableA.Columns, tableB.Columns)
}

func compareColumnMetadata(columnA, columnB *ColumnMetadata) bool {
	if columnA == columnB {
		return true
	}
	return columnA != nil && columnB != nil &&
		columnA.Table == columnB.Table &&
		columnA.Name == columnB.Name &&
		columnA.ComponentIndex == columnB.ComponentIndex &&
		columnA.Kind == columnB.Kind &&
		columnA.Validator == columnB.Validator &&
		columnA.ClusteringOrder == columnB.ClusteringOrder &&
		columnA.Order == columnB.Order &&
		compareColumnIndexMetadata(&columnA.Index, &columnB.Index)
}

func compareColumnMetadataSlices(colsA, colsB []*ColumnMetadata) bool {
	if len(colsA) != len(colsB) {
		return false
	}
	for i := range colsA {
		if !compareColumnMetadata(colsA[i], colsB[i]) {
			return false
		}
	}
	return true
}

func columnsEqual(colsA, colsB map[string]*ColumnMetadata) bool {
	if len(colsA) != len(colsB) {
		return false
	}
	for k, v := range colsA {
		if !compareColumnMetadata(v, colsB[k]) {
			return false
		}
	}
	return true
}

func compareColumnIndexMetadata(indexA, indexB *ColumnIndexMetadata) bool {
	if indexA == indexB {
		return true
	}

	return indexA != nil && indexB != nil &&
		indexA.Name == indexB.Name &&
		indexA.Type == indexB.Type &&
		compareMapStringInterface(indexA.Options, indexB.Options)
}

func compareAggregateMetadata(aggregateA, aggregateB *AggregateMetadata) bool {
	if aggregateA == aggregateB {
		return true
	}

	return aggregateA != nil && aggregateB != nil &&
		aggregateA.finalFunc == aggregateB.finalFunc &&
		aggregateA.InitCond == aggregateB.InitCond &&
		aggregateA.returnTypeRaw == aggregateB.returnTypeRaw &&
		aggregateA.stateFunc == aggregateB.stateFunc &&
		aggregateA.stateTypeRaw == aggregateB.stateTypeRaw &&
		stringsSlicesEqual(aggregateA.argumentTypesRaw, aggregateB.argumentTypesRaw)
}

func compareFunctionMetadata(functionA, functionB *FunctionMetadata) bool {
	if functionA == functionB {
		return true
	}

	return functionA != nil && functionB != nil &&
		functionA.Body == functionB.Body &&
		functionA.CalledOnNullInput == functionB.CalledOnNullInput &&
		functionA.Language == functionB.Language &&
		stringsSlicesEqual(functionA.ArgumentNames, functionB.ArgumentNames) &&
		stringsSlicesEqual(functionA.argumentTypesRaw, functionB.argumentTypesRaw) &&
		functionA.returnTypeRaw == functionB.returnTypeRaw
}

func compareUserTypeMetadata(userTypeA, userTypeB *UserTypeMetadata) bool {
	if userTypeA == userTypeB {
		return true
	}

	return userTypeA != nil && userTypeB != nil &&
		userTypeA.Name == userTypeB.Name &&
		stringsSlicesEqual(userTypeA.FieldNames, userTypeB.FieldNames) &&
		stringsSlicesEqual(userTypeA.fieldTypesRaw, userTypeB.fieldTypesRaw)
}
