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

package gocql

// SessionReadyListener is notified when the session is ready to be used.
// This is useful for users who need to know when the session is ready to be used.
type SessionReadyListener interface {
	OnSessionReady(*Session)
}

// TopologyChangeListener receives topology change events.
//
// Thread Safety: Methods are called sequentially from a single goroutine.
// However, if a type implements both TopologyChangeListener and HostStatusChangeListener,
// it must be thread-safe as topology and status callbacks can run concurrently.
type TopologyChangeListener interface {
	OnNewHost(event NewHostEvent)
	OnRemovedHost(event RemovedHostEvent)
}

type NewHostEvent struct {
	Host *HostInfo
}

type RemovedHostEvent struct {
	Host *HostInfo
}

// HostStatusChangeListener receives host state change events.
//
// Thread Safety: Implementations must be thread-safe as methods can be called
// concurrently from multiple goroutines.
type HostStatusChangeListener interface {
	OnHostUp(event HostUpEvent)
	OnHostDown(event HostDownEvent)
}

type HostUpEvent struct {
	Host *HostInfo
}

type HostDownEvent struct {
	Host *HostInfo
}

// KeyspaceChangeListener receives keyspace change events.
type KeyspaceChangeListener interface {
	OnKeyspaceCreated(event OnKeyspaceCreatedEvent)
	OnKeyspaceUpdated(event OnKeyspaceUpdatedEvent)
	OnKeyspaceDropped(event OnKeyspaceDroppedEvent)
}

// TableChangeListener receives table change events.
type TableChangeListener interface {
	OnTableCreated(event OnTableCreatedEvent)
	OnTableUpdated(event OnTableUpdatedEvent)
	OnTableDropped(event OnTableDroppedEvent)
}

// UserTypeChangeListener receives user-defined type change events.
type UserTypeChangeListener interface {
	OnUserTypeCreated(event OnUserTypeCreatedEvent)
	OnUserTypeUpdated(event OnUserTypeUpdatedEvent)
	OnUserTypeDropped(event OnUserTypeDroppedEvent)
}

// FunctionChangeListener receives function change events.
type FunctionChangeListener interface {
	OnFunctionCreated(event OnFunctionCreatedEvent)
	OnFunctionUpdated(event OnFunctionUpdatedEvent)
	OnFunctionDropped(event OnFunctionDroppedEvent)
}

// AggregateChangeListener receives aggregate change events.
type AggregateChangeListener interface {
	OnAggregateCreated(event OnAggregateCreatedEvent)
	OnAggregateUpdated(event OnAggregateUpdatedEvent)
	OnAggregateDropped(event OnAggregateDroppedEvent)
}

type OnKeyspaceCreatedEvent struct {
	Keyspace *KeyspaceMetadata
}

type OnKeyspaceUpdatedEvent struct {
	Old *KeyspaceMetadata
	New *KeyspaceMetadata
}

type OnKeyspaceDroppedEvent struct {
	Keyspace *KeyspaceMetadata
}

type OnTableCreatedEvent struct {
	Table *TableMetadata
}

type OnTableUpdatedEvent struct {
	Old *TableMetadata
	New *TableMetadata
}

type OnTableDroppedEvent struct {
	Table *TableMetadata
}

type OnUserTypeCreatedEvent struct {
	UserType *UserTypeMetadata
}

type OnUserTypeUpdatedEvent struct {
	Old *UserTypeMetadata
	New *UserTypeMetadata
}

type OnUserTypeDroppedEvent struct {
	UserType *UserTypeMetadata
}

type OnFunctionCreatedEvent struct {
	Function *FunctionMetadata
}

type OnFunctionUpdatedEvent struct {
	Old *FunctionMetadata
	New *FunctionMetadata
}

type OnFunctionDroppedEvent struct {
	Function *FunctionMetadata
}

type OnAggregateCreatedEvent struct {
	Aggregate *AggregateMetadata
}

type OnAggregateUpdatedEvent struct {
	Old *AggregateMetadata
	New *AggregateMetadata
}

type OnAggregateDroppedEvent struct {
	Aggregate *AggregateMetadata
}

// SchemaListenersMux is a multiplexer for schema change listeners.
// Allows to register multiple listeners for the same type of schema change.
type SchemaListenersMux struct {
	Keyspaces  []KeyspaceChangeListener
	Tables     []TableChangeListener
	UserTypes  []UserTypeChangeListener
	Functions  []FunctionChangeListener
	Aggregates []AggregateChangeListener
}

func (mux SchemaListenersMux) OnKeyspaceCreated(event OnKeyspaceCreatedEvent) {
	for _, listener := range mux.Keyspaces {
		listener.OnKeyspaceCreated(event)
	}
}

func (mux SchemaListenersMux) OnKeyspaceUpdated(event OnKeyspaceUpdatedEvent) {
	for _, listener := range mux.Keyspaces {
		listener.OnKeyspaceUpdated(event)
	}
}

func (mux SchemaListenersMux) OnKeyspaceDropped(event OnKeyspaceDroppedEvent) {
	for _, listener := range mux.Keyspaces {
		listener.OnKeyspaceDropped(event)
	}
}

func (mux SchemaListenersMux) OnTableCreated(event OnTableCreatedEvent) {
	for _, listener := range mux.Tables {
		listener.OnTableCreated(event)
	}
}

func (mux SchemaListenersMux) OnTableUpdated(event OnTableUpdatedEvent) {
	for _, listener := range mux.Tables {
		listener.OnTableUpdated(event)
	}
}

func (mux SchemaListenersMux) OnTableDropped(event OnTableDroppedEvent) {
	for _, listener := range mux.Tables {
		listener.OnTableDropped(event)
	}
}

func (mux SchemaListenersMux) OnUserTypeCreated(event OnUserTypeCreatedEvent) {
	for _, listener := range mux.UserTypes {
		listener.OnUserTypeCreated(event)
	}
}

func (mux SchemaListenersMux) OnUserTypeUpdated(event OnUserTypeUpdatedEvent) {
	for _, listener := range mux.UserTypes {
		listener.OnUserTypeUpdated(event)
	}
}

func (mux SchemaListenersMux) OnUserTypeDropped(event OnUserTypeDroppedEvent) {
	for _, listener := range mux.UserTypes {
		listener.OnUserTypeDropped(event)
	}
}

func (mux SchemaListenersMux) OnFunctionCreated(event OnFunctionCreatedEvent) {
	for _, listener := range mux.Functions {
		listener.OnFunctionCreated(event)
	}
}

func (mux SchemaListenersMux) OnFunctionUpdated(event OnFunctionUpdatedEvent) {
	for _, listener := range mux.Functions {
		listener.OnFunctionUpdated(event)
	}
}

func (mux SchemaListenersMux) OnFunctionDropped(event OnFunctionDroppedEvent) {
	for _, listener := range mux.Functions {
		listener.OnFunctionDropped(event)
	}
}

func (mux SchemaListenersMux) OnAggregateCreated(event OnAggregateCreatedEvent) {
	for _, listener := range mux.Aggregates {
		listener.OnAggregateCreated(event)
	}
}

func (mux SchemaListenersMux) OnAggregateUpdated(event OnAggregateUpdatedEvent) {
	for _, listener := range mux.Aggregates {
		listener.OnAggregateUpdated(event)
	}
}

func (mux SchemaListenersMux) OnAggregateDropped(event OnAggregateDroppedEvent) {
	for _, listener := range mux.Aggregates {
		listener.OnAggregateDropped(event)
	}
}

// HostListenersMux is a multiplexer for host state and topology change listeners.
// Allows to register multiple listeners for the same type of host state and topology change.
type HostListenersMux struct {
	HostStateChangeListeners []HostStatusChangeListener
	TopologyChangeListeners  []TopologyChangeListener
}

func (mux HostListenersMux) OnHostUp(event HostUpEvent) {
	for _, listener := range mux.HostStateChangeListeners {
		listener.OnHostUp(event)
	}
}

func (mux HostListenersMux) OnHostDown(event HostDownEvent) {
	for _, listener := range mux.HostStateChangeListeners {
		listener.OnHostDown(event)
	}
}

func (mux HostListenersMux) OnNewHost(event NewHostEvent) {
	for _, listener := range mux.TopologyChangeListeners {
		listener.OnNewHost(event)
	}
}

func (mux HostListenersMux) OnRemovedHost(event RemovedHostEvent) {
	for _, listener := range mux.TopologyChangeListeners {
		listener.OnRemovedHost(event)
	}
}

// Wrapper around the host topology and state change listeners.
// Provides nil checks for the listeners and tracks if the session is initialized.
type internalHostListeners struct {
	hostStateChangeListener HostStatusChangeListener
	topologyChangeListener  TopologyChangeListener
	session                 *Session
}

func newInternalHostStateListeners(session *Session, hostStateChangeListener HostStatusChangeListener, topologyChangeListener TopologyChangeListener) internalHostListeners {
	return internalHostListeners{
		hostStateChangeListener: hostStateChangeListener,
		topologyChangeListener:  topologyChangeListener,
		session:                 session,
	}
}

func (l internalHostListeners) OnHostUp(event HostUpEvent) {
	if l.hostStateChangeListener != nil && l.session.initialized() {
		l.hostStateChangeListener.OnHostUp(event)
	}
}

func (l internalHostListeners) OnHostDown(event HostDownEvent) {
	if l.hostStateChangeListener != nil && l.session.initialized() {
		l.hostStateChangeListener.OnHostDown(event)
	}
}

func (l internalHostListeners) OnNewHost(event NewHostEvent) {
	if l.topologyChangeListener != nil && l.session.initialized() {
		l.topologyChangeListener.OnNewHost(event)
	}
}

func (l internalHostListeners) OnRemovedHost(event RemovedHostEvent) {
	if l.topologyChangeListener != nil && l.session.initialized() {
		l.topologyChangeListener.OnRemovedHost(event)
	}
}

// Wrapper around the session ready listener.
// Provides nil checks for the listener.
type internalSessionReadyListener struct {
	sessionReadyListener SessionReadyListener
}

func (l internalSessionReadyListener) OnSessionReady(session *Session) {
	if l.sessionReadyListener != nil {
		l.sessionReadyListener.OnSessionReady(session)
	}
}

func newInternalSessionReadyListener(sessionReadyListener SessionReadyListener) internalSessionReadyListener {
	return internalSessionReadyListener{
		sessionReadyListener: sessionReadyListener,
	}
}

// Wrapper around the schema change listeners.
// Provides nil checks for the listeners.
type internalSchemaListeners struct {
	keyspaceChangeListener  KeyspaceChangeListener
	tableChangeListener     TableChangeListener
	userTypeChangeListener  UserTypeChangeListener
	funcChangeListener      FunctionChangeListener
	aggregateChangeListener AggregateChangeListener
}

func newInternalSchemaChangeListeners(keyspaceChangeListener KeyspaceChangeListener, tableChangeListener TableChangeListener, userTypeChangeListener UserTypeChangeListener, funcChangeListener FunctionChangeListener, aggregateChangeListener AggregateChangeListener) internalSchemaListeners {
	return internalSchemaListeners{
		keyspaceChangeListener:  keyspaceChangeListener,
		tableChangeListener:     tableChangeListener,
		userTypeChangeListener:  userTypeChangeListener,
		funcChangeListener:      funcChangeListener,
		aggregateChangeListener: aggregateChangeListener,
	}
}

func (l internalSchemaListeners) OnKeyspaceCreated(event OnKeyspaceCreatedEvent) {
	if l.keyspaceChangeListener != nil {
		l.keyspaceChangeListener.OnKeyspaceCreated(event)
	}
}

func (l internalSchemaListeners) OnKeyspaceUpdated(event OnKeyspaceUpdatedEvent) {
	if l.keyspaceChangeListener != nil {
		l.keyspaceChangeListener.OnKeyspaceUpdated(event)
	}
}

func (l internalSchemaListeners) OnKeyspaceDropped(event OnKeyspaceDroppedEvent) {
	if l.keyspaceChangeListener != nil {
		l.keyspaceChangeListener.OnKeyspaceDropped(event)
	}
}

func (l internalSchemaListeners) OnTableCreated(event OnTableCreatedEvent) {
	if l.tableChangeListener != nil {
		l.tableChangeListener.OnTableCreated(event)
	}
}

func (l internalSchemaListeners) OnTableUpdated(event OnTableUpdatedEvent) {
	if l.tableChangeListener != nil {
		l.tableChangeListener.OnTableUpdated(event)
	}
}

func (l internalSchemaListeners) OnTableDropped(event OnTableDroppedEvent) {
	if l.tableChangeListener != nil {
		l.tableChangeListener.OnTableDropped(event)
	}
}

func (l internalSchemaListeners) OnUserTypeCreated(event OnUserTypeCreatedEvent) {
	if l.userTypeChangeListener != nil {
		l.userTypeChangeListener.OnUserTypeCreated(event)
	}
}

func (l internalSchemaListeners) OnUserTypeUpdated(event OnUserTypeUpdatedEvent) {
	if l.userTypeChangeListener != nil {
		l.userTypeChangeListener.OnUserTypeUpdated(event)
	}
}

func (l internalSchemaListeners) OnUserTypeDropped(event OnUserTypeDroppedEvent) {
	if l.userTypeChangeListener != nil {
		l.userTypeChangeListener.OnUserTypeDropped(event)
	}
}

func (l internalSchemaListeners) OnFunctionCreated(event OnFunctionCreatedEvent) {
	if l.funcChangeListener != nil {
		l.funcChangeListener.OnFunctionCreated(event)
	}
}

func (l internalSchemaListeners) OnFunctionUpdated(event OnFunctionUpdatedEvent) {
	if l.funcChangeListener != nil {
		l.funcChangeListener.OnFunctionUpdated(event)
	}
}

func (l internalSchemaListeners) OnFunctionDropped(event OnFunctionDroppedEvent) {
	if l.funcChangeListener != nil {
		l.funcChangeListener.OnFunctionDropped(event)
	}
}

func (l internalSchemaListeners) OnAggregateCreated(event OnAggregateCreatedEvent) {
	if l.aggregateChangeListener != nil {
		l.aggregateChangeListener.OnAggregateCreated(event)
	}
}

func (l internalSchemaListeners) OnAggregateUpdated(event OnAggregateUpdatedEvent) {
	if l.aggregateChangeListener != nil {
		l.aggregateChangeListener.OnAggregateUpdated(event)
	}
}

func (l internalSchemaListeners) OnAggregateDropped(event OnAggregateDroppedEvent) {
	if l.aggregateChangeListener != nil {
		l.aggregateChangeListener.OnAggregateDropped(event)
	}
}

func (l internalSchemaListeners) hasTable() bool {
	return l.tableChangeListener != nil
}

func (l internalSchemaListeners) hasUserType() bool {
	return l.userTypeChangeListener != nil
}

func (l internalSchemaListeners) hasFunction() bool {
	return l.funcChangeListener != nil
}

func (l internalSchemaListeners) hasAggregate() bool {
	return l.aggregateChangeListener != nil
}

func (l internalSchemaListeners) hasKeyspace() bool {
	return l.keyspaceChangeListener != nil
}

func (l internalSchemaListeners) hasSchemaChangeListeners() bool {
	return l.hasKeyspace() || l.hasNonKeyspaceSchemaChangeListeners()
}

func (l internalSchemaListeners) hasNonKeyspaceSchemaChangeListeners() bool {
	return l.hasTable() ||
		l.hasUserType() ||
		l.hasFunction() ||
		l.hasAggregate()
}

// SessionReadyListenersMux is a multiplexer for session ready listeners.
// Consider using this if you need to have multiple listeners for the same session ready event.
type SessionReadyListenersMux struct {
	SessionReady []SessionReadyListener
}

func (mux SessionReadyListenersMux) OnSessionReady(session *Session) {
	for _, listener := range mux.SessionReady {
		listener.OnSessionReady(session)
	}
}
