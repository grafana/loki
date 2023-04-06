// Copyright 2017-2022 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nats-io/jwt/v2"
)

// FlagSnapshot captures the server options as specified by CLI flags at
// startup. This should not be modified once the server has started.
var FlagSnapshot *Options

type reloadContext struct {
	oldClusterPerms *RoutePermissions
}

// option is a hot-swappable configuration setting.
type option interface {
	// Apply the server option.
	Apply(server *Server)

	// IsLoggingChange indicates if this option requires reloading the logger.
	IsLoggingChange() bool

	// IsTraceLevelChange indicates if this option requires reloading cached trace level.
	// Clients store trace level separately.
	IsTraceLevelChange() bool

	// IsAuthChange indicates if this option requires reloading authorization.
	IsAuthChange() bool

	// IsTLSChange indicates if this option requires reloading TLS.
	IsTLSChange() bool

	// IsClusterPermsChange indicates if this option requires reloading
	// cluster permissions.
	IsClusterPermsChange() bool

	// IsJetStreamChange inidicates a change in the servers config for JetStream.
	// Account changes will be handled separately in reloadAuthorization.
	IsJetStreamChange() bool

	// Indicates a change in the server that requires publishing the server's statz
	IsStatszChange() bool
}

// noopOption is a base struct that provides default no-op behaviors.
type noopOption struct{}

func (n noopOption) IsLoggingChange() bool {
	return false
}

func (n noopOption) IsTraceLevelChange() bool {
	return false
}

func (n noopOption) IsAuthChange() bool {
	return false
}

func (n noopOption) IsTLSChange() bool {
	return false
}

func (n noopOption) IsClusterPermsChange() bool {
	return false
}

func (n noopOption) IsJetStreamChange() bool {
	return false
}

func (n noopOption) IsStatszChange() bool {
	return false
}

// loggingOption is a base struct that provides default option behaviors for
// logging-related options.
type loggingOption struct {
	noopOption
}

func (l loggingOption) IsLoggingChange() bool {
	return true
}

// traceLevelOption is a base struct that provides default option behaviors for
// tracelevel-related options.
type traceLevelOption struct {
	loggingOption
}

func (l traceLevelOption) IsTraceLevelChange() bool {
	return true
}

// traceOption implements the option interface for the `trace` setting.
type traceOption struct {
	traceLevelOption
	newValue bool
}

// Apply is a no-op because logging will be reloaded after options are applied.
func (t *traceOption) Apply(server *Server) {
	server.Noticef("Reloaded: trace = %v", t.newValue)
}

// traceOption implements the option interface for the `trace` setting.
type traceVerboseOption struct {
	traceLevelOption
	newValue bool
}

// Apply is a no-op because logging will be reloaded after options are applied.
func (t *traceVerboseOption) Apply(server *Server) {
	server.Noticef("Reloaded: trace_verbose = %v", t.newValue)
}

// debugOption implements the option interface for the `debug` setting.
type debugOption struct {
	loggingOption
	newValue bool
}

// Apply is mostly a no-op because logging will be reloaded after options are applied.
// However we will kick the raft nodes if they exist to reload.
func (d *debugOption) Apply(server *Server) {
	server.Noticef("Reloaded: debug = %v", d.newValue)
	server.reloadDebugRaftNodes()
}

// logtimeOption implements the option interface for the `logtime` setting.
type logtimeOption struct {
	loggingOption
	newValue bool
}

// Apply is a no-op because logging will be reloaded after options are applied.
func (l *logtimeOption) Apply(server *Server) {
	server.Noticef("Reloaded: logtime = %v", l.newValue)
}

// logfileOption implements the option interface for the `log_file` setting.
type logfileOption struct {
	loggingOption
	newValue string
}

// Apply is a no-op because logging will be reloaded after options are applied.
func (l *logfileOption) Apply(server *Server) {
	server.Noticef("Reloaded: log_file = %v", l.newValue)
}

// syslogOption implements the option interface for the `syslog` setting.
type syslogOption struct {
	loggingOption
	newValue bool
}

// Apply is a no-op because logging will be reloaded after options are applied.
func (s *syslogOption) Apply(server *Server) {
	server.Noticef("Reloaded: syslog = %v", s.newValue)
}

// remoteSyslogOption implements the option interface for the `remote_syslog`
// setting.
type remoteSyslogOption struct {
	loggingOption
	newValue string
}

// Apply is a no-op because logging will be reloaded after options are applied.
func (r *remoteSyslogOption) Apply(server *Server) {
	server.Noticef("Reloaded: remote_syslog = %v", r.newValue)
}

// tlsOption implements the option interface for the `tls` setting.
type tlsOption struct {
	noopOption
	newValue *tls.Config
}

// Apply the tls change.
func (t *tlsOption) Apply(server *Server) {
	server.mu.Lock()
	tlsRequired := t.newValue != nil
	server.info.TLSRequired = tlsRequired && !server.getOpts().AllowNonTLS
	message := "disabled"
	if tlsRequired {
		server.info.TLSVerify = (t.newValue.ClientAuth == tls.RequireAndVerifyClientCert)
		message = "enabled"
	}
	server.mu.Unlock()
	server.Noticef("Reloaded: tls = %s", message)
}

func (t *tlsOption) IsTLSChange() bool {
	return true
}

// tlsTimeoutOption implements the option interface for the tls `timeout`
// setting.
type tlsTimeoutOption struct {
	noopOption
	newValue float64
}

// Apply is a no-op because the timeout will be reloaded after options are
// applied.
func (t *tlsTimeoutOption) Apply(server *Server) {
	server.Noticef("Reloaded: tls timeout = %v", t.newValue)
}

// tlsPinnedCertOption implements the option interface for the tls `pinned_certs` setting.
type tlsPinnedCertOption struct {
	noopOption
	newValue PinnedCertSet
}

// Apply is a no-op because the pinned certs will be reloaded after options are  applied.
func (t *tlsPinnedCertOption) Apply(server *Server) {
	server.Noticef("Reloaded: %d pinned_certs", len(t.newValue))
}

// authOption is a base struct that provides default option behaviors.
type authOption struct {
	noopOption
}

func (o authOption) IsAuthChange() bool {
	return true
}

// usernameOption implements the option interface for the `username` setting.
type usernameOption struct {
	authOption
}

// Apply is a no-op because authorization will be reloaded after options are
// applied.
func (u *usernameOption) Apply(server *Server) {
	server.Noticef("Reloaded: authorization username")
}

// passwordOption implements the option interface for the `password` setting.
type passwordOption struct {
	authOption
}

// Apply is a no-op because authorization will be reloaded after options are
// applied.
func (p *passwordOption) Apply(server *Server) {
	server.Noticef("Reloaded: authorization password")
}

// authorizationOption implements the option interface for the `token`
// authorization setting.
type authorizationOption struct {
	authOption
}

// Apply is a no-op because authorization will be reloaded after options are
// applied.
func (a *authorizationOption) Apply(server *Server) {
	server.Noticef("Reloaded: authorization token")
}

// authTimeoutOption implements the option interface for the authorization
// `timeout` setting.
type authTimeoutOption struct {
	noopOption // Not authOption because this is a no-op; will be reloaded with options.
	newValue   float64
}

// Apply is a no-op because the timeout will be reloaded after options are
// applied.
func (a *authTimeoutOption) Apply(server *Server) {
	server.Noticef("Reloaded: authorization timeout = %v", a.newValue)
}

// tagsOption implements the option interface for the `tags` setting.
type tagsOption struct {
	noopOption // Not authOption because this is a no-op; will be reloaded with options.
}

func (u *tagsOption) Apply(server *Server) {
	server.Noticef("Reloaded: tags")
}

func (u *tagsOption) IsStatszChange() bool {
	return true
}

// usersOption implements the option interface for the authorization `users`
// setting.
type usersOption struct {
	authOption
}

func (u *usersOption) Apply(server *Server) {
	server.Noticef("Reloaded: authorization users")
}

// nkeysOption implements the option interface for the authorization `users`
// setting.
type nkeysOption struct {
	authOption
}

func (u *nkeysOption) Apply(server *Server) {
	server.Noticef("Reloaded: authorization nkey users")
}

// clusterOption implements the option interface for the `cluster` setting.
type clusterOption struct {
	authOption
	newValue     ClusterOpts
	permsChanged bool
}

// Apply the cluster change.
func (c *clusterOption) Apply(s *Server) {
	// TODO: support enabling/disabling clustering.
	s.mu.Lock()
	tlsRequired := c.newValue.TLSConfig != nil
	s.routeInfo.TLSRequired = tlsRequired
	s.routeInfo.TLSVerify = tlsRequired
	s.routeInfo.AuthRequired = c.newValue.Username != ""
	if c.newValue.NoAdvertise {
		s.routeInfo.ClientConnectURLs = nil
		s.routeInfo.WSConnectURLs = nil
	} else {
		s.routeInfo.ClientConnectURLs = s.clientConnectURLs
		s.routeInfo.WSConnectURLs = s.websocket.connectURLs
	}
	s.setRouteInfoHostPortAndIP()
	s.mu.Unlock()
	if c.newValue.Name != "" && c.newValue.Name != s.ClusterName() {
		s.setClusterName(c.newValue.Name)
	}
	s.Noticef("Reloaded: cluster")
	if tlsRequired && c.newValue.TLSConfig.InsecureSkipVerify {
		s.Warnf(clusterTLSInsecureWarning)
	}
}

func (c *clusterOption) IsClusterPermsChange() bool {
	return c.permsChanged
}

// routesOption implements the option interface for the cluster `routes`
// setting.
type routesOption struct {
	noopOption
	add    []*url.URL
	remove []*url.URL
}

// Apply the route changes by adding and removing the necessary routes.
func (r *routesOption) Apply(server *Server) {
	server.mu.Lock()
	routes := make([]*client, len(server.routes))
	i := 0
	for _, client := range server.routes {
		routes[i] = client
		i++
	}
	// If there was a change, notify monitoring code that it should
	// update the route URLs if /varz endpoint is inspected.
	if len(r.add)+len(r.remove) > 0 {
		server.varzUpdateRouteURLs = true
	}
	server.mu.Unlock()

	// Remove routes.
	for _, remove := range r.remove {
		for _, client := range routes {
			var url *url.URL
			client.mu.Lock()
			if client.route != nil {
				url = client.route.url
			}
			client.mu.Unlock()
			if url != nil && urlsAreEqual(url, remove) {
				// Do not attempt to reconnect when route is removed.
				client.setNoReconnect()
				client.closeConnection(RouteRemoved)
				server.Noticef("Removed route %v", remove)
			}
		}
	}

	// Add routes.
	server.mu.Lock()
	server.solicitRoutes(r.add)
	server.mu.Unlock()

	server.Noticef("Reloaded: cluster routes")
}

// maxConnOption implements the option interface for the `max_connections`
// setting.
type maxConnOption struct {
	noopOption
	newValue int
}

// Apply the max connections change by closing random connections til we are
// below the limit if necessary.
func (m *maxConnOption) Apply(server *Server) {
	server.mu.Lock()
	var (
		clients = make([]*client, len(server.clients))
		i       = 0
	)
	// Map iteration is random, which allows us to close random connections.
	for _, client := range server.clients {
		clients[i] = client
		i++
	}
	server.mu.Unlock()

	if m.newValue > 0 && len(clients) > m.newValue {
		// Close connections til we are within the limit.
		var (
			numClose = len(clients) - m.newValue
			closed   = 0
		)
		for _, client := range clients {
			client.maxConnExceeded()
			closed++
			if closed >= numClose {
				break
			}
		}
		server.Noticef("Closed %d connections to fall within max_connections", closed)
	}
	server.Noticef("Reloaded: max_connections = %v", m.newValue)
}

// pidFileOption implements the option interface for the `pid_file` setting.
type pidFileOption struct {
	noopOption
	newValue string
}

// Apply the setting by logging the pid to the new file.
func (p *pidFileOption) Apply(server *Server) {
	if p.newValue == "" {
		return
	}
	if err := server.logPid(); err != nil {
		server.Errorf("Failed to write pidfile: %v", err)
	}
	server.Noticef("Reloaded: pid_file = %v", p.newValue)
}

// portsFileDirOption implements the option interface for the `portFileDir` setting.
type portsFileDirOption struct {
	noopOption
	oldValue string
	newValue string
}

func (p *portsFileDirOption) Apply(server *Server) {
	server.deletePortsFile(p.oldValue)
	server.logPorts()
	server.Noticef("Reloaded: ports_file_dir = %v", p.newValue)
}

// maxControlLineOption implements the option interface for the
// `max_control_line` setting.
type maxControlLineOption struct {
	noopOption
	newValue int32
}

// Apply the setting by updating each client.
func (m *maxControlLineOption) Apply(server *Server) {
	mcl := int32(m.newValue)
	server.mu.Lock()
	for _, client := range server.clients {
		atomic.StoreInt32(&client.mcl, mcl)
	}
	server.mu.Unlock()
	server.Noticef("Reloaded: max_control_line = %d", mcl)
}

// maxPayloadOption implements the option interface for the `max_payload`
// setting.
type maxPayloadOption struct {
	noopOption
	newValue int32
}

// Apply the setting by updating the server info and each client.
func (m *maxPayloadOption) Apply(server *Server) {
	server.mu.Lock()
	server.info.MaxPayload = m.newValue
	for _, client := range server.clients {
		atomic.StoreInt32(&client.mpay, int32(m.newValue))
	}
	server.mu.Unlock()
	server.Noticef("Reloaded: max_payload = %d", m.newValue)
}

// pingIntervalOption implements the option interface for the `ping_interval`
// setting.
type pingIntervalOption struct {
	noopOption
	newValue time.Duration
}

// Apply is a no-op because the ping interval will be reloaded after options
// are applied.
func (p *pingIntervalOption) Apply(server *Server) {
	server.Noticef("Reloaded: ping_interval = %s", p.newValue)
}

// maxPingsOutOption implements the option interface for the `ping_max`
// setting.
type maxPingsOutOption struct {
	noopOption
	newValue int
}

// Apply is a no-op because the ping interval will be reloaded after options
// are applied.
func (m *maxPingsOutOption) Apply(server *Server) {
	server.Noticef("Reloaded: ping_max = %d", m.newValue)
}

// writeDeadlineOption implements the option interface for the `write_deadline`
// setting.
type writeDeadlineOption struct {
	noopOption
	newValue time.Duration
}

// Apply is a no-op because the write deadline will be reloaded after options
// are applied.
func (w *writeDeadlineOption) Apply(server *Server) {
	server.Noticef("Reloaded: write_deadline = %s", w.newValue)
}

// clientAdvertiseOption implements the option interface for the `client_advertise` setting.
type clientAdvertiseOption struct {
	noopOption
	newValue string
}

// Apply the setting by updating the server info and regenerate the infoJSON byte array.
func (c *clientAdvertiseOption) Apply(server *Server) {
	server.mu.Lock()
	server.setInfoHostPort()
	server.mu.Unlock()
	server.Noticef("Reload: client_advertise = %s", c.newValue)
}

// accountsOption implements the option interface.
// Ensure that authorization code is executed if any change in accounts
type accountsOption struct {
	authOption
}

// Apply is a no-op. Changes will be applied in reloadAuthorization
func (a *accountsOption) Apply(s *Server) {
	s.Noticef("Reloaded: accounts")
}

// For changes to a server's config.
type jetStreamOption struct {
	noopOption
	newValue bool
}

func (a *jetStreamOption) Apply(s *Server) {
	s.Noticef("Reloaded: JetStream")
}

func (jso jetStreamOption) IsJetStreamChange() bool {
	return true
}

func (jso jetStreamOption) IsStatszChange() bool {
	return true
}

type ocspOption struct {
	noopOption
	newValue *OCSPConfig
}

func (a *ocspOption) Apply(s *Server) {
	s.Noticef("Reloaded: OCSP")
}

// connectErrorReports implements the option interface for the `connect_error_reports`
// setting.
type connectErrorReports struct {
	noopOption
	newValue int
}

// Apply is a no-op because the value will be reloaded after options are applied.
func (c *connectErrorReports) Apply(s *Server) {
	s.Noticef("Reloaded: connect_error_reports = %v", c.newValue)
}

// connectErrorReports implements the option interface for the `connect_error_reports`
// setting.
type reconnectErrorReports struct {
	noopOption
	newValue int
}

// Apply is a no-op because the value will be reloaded after options are applied.
func (r *reconnectErrorReports) Apply(s *Server) {
	s.Noticef("Reloaded: reconnect_error_reports = %v", r.newValue)
}

// maxTracedMsgLenOption implements the option interface for the `max_traced_msg_len` setting.
type maxTracedMsgLenOption struct {
	noopOption
	newValue int
}

// Apply the setting by updating the maximum traced message length.
func (m *maxTracedMsgLenOption) Apply(server *Server) {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.opts.MaxTracedMsgLen = m.newValue
	server.Noticef("Reloaded: max_traced_msg_len = %d", m.newValue)
}

type mqttAckWaitReload struct {
	noopOption
	newValue time.Duration
}

func (o *mqttAckWaitReload) Apply(s *Server) {
	s.Noticef("Reloaded: MQTT ack_wait = %v", o.newValue)
}

type mqttMaxAckPendingReload struct {
	noopOption
	newValue uint16
}

func (o *mqttMaxAckPendingReload) Apply(s *Server) {
	s.mqttUpdateMaxAckPending(o.newValue)
	s.Noticef("Reloaded: MQTT max_ack_pending = %v", o.newValue)
}

type mqttStreamReplicasReload struct {
	noopOption
	newValue int
}

func (o *mqttStreamReplicasReload) Apply(s *Server) {
	s.Noticef("Reloaded: MQTT stream_replicas = %v", o.newValue)
}

type mqttConsumerReplicasReload struct {
	noopOption
	newValue int
}

func (o *mqttConsumerReplicasReload) Apply(s *Server) {
	s.Noticef("Reloaded: MQTT consumer_replicas = %v", o.newValue)
}

type mqttConsumerMemoryStorageReload struct {
	noopOption
	newValue bool
}

func (o *mqttConsumerMemoryStorageReload) Apply(s *Server) {
	s.Noticef("Reloaded: MQTT consumer_memory_storage = %v", o.newValue)
}

type mqttInactiveThresholdReload struct {
	noopOption
	newValue time.Duration
}

func (o *mqttInactiveThresholdReload) Apply(s *Server) {
	s.Noticef("Reloaded: MQTT consumer_inactive_threshold = %v", o.newValue)
}

// Compares options and disconnects clients that are no longer listed in pinned certs. Lock must not be held.
func (s *Server) recheckPinnedCerts(curOpts *Options, newOpts *Options) {
	s.mu.Lock()
	disconnectClients := []*client{}
	protoToPinned := map[int]PinnedCertSet{}
	if !reflect.DeepEqual(newOpts.TLSPinnedCerts, curOpts.TLSPinnedCerts) {
		protoToPinned[NATS] = curOpts.TLSPinnedCerts
	}
	if !reflect.DeepEqual(newOpts.MQTT.TLSPinnedCerts, curOpts.MQTT.TLSPinnedCerts) {
		protoToPinned[MQTT] = curOpts.MQTT.TLSPinnedCerts
	}
	if !reflect.DeepEqual(newOpts.Websocket.TLSPinnedCerts, curOpts.Websocket.TLSPinnedCerts) {
		protoToPinned[WS] = curOpts.Websocket.TLSPinnedCerts
	}
	for _, c := range s.clients {
		if c.kind != CLIENT {
			continue
		}
		if pinned, ok := protoToPinned[c.clientType()]; ok {
			if !c.matchesPinnedCert(pinned) {
				disconnectClients = append(disconnectClients, c)
			}
		}
	}
	checkClients := func(kind int, clients map[uint64]*client, set PinnedCertSet) {
		for _, c := range clients {
			if c.kind == kind && !c.matchesPinnedCert(set) {
				disconnectClients = append(disconnectClients, c)
			}
		}
	}
	if !reflect.DeepEqual(newOpts.LeafNode.TLSPinnedCerts, curOpts.LeafNode.TLSPinnedCerts) {
		checkClients(LEAF, s.leafs, newOpts.LeafNode.TLSPinnedCerts)
	}
	if !reflect.DeepEqual(newOpts.Cluster.TLSPinnedCerts, curOpts.Cluster.TLSPinnedCerts) {
		checkClients(ROUTER, s.routes, newOpts.Cluster.TLSPinnedCerts)
	}
	if reflect.DeepEqual(newOpts.Gateway.TLSPinnedCerts, curOpts.Gateway.TLSPinnedCerts) {
		for _, c := range s.remotes {
			if !c.matchesPinnedCert(newOpts.Gateway.TLSPinnedCerts) {
				disconnectClients = append(disconnectClients, c)
			}
		}
	}
	s.mu.Unlock()
	if len(disconnectClients) > 0 {
		s.Noticef("Disconnect %d clients due to pinned certs reload", len(disconnectClients))
		for _, c := range disconnectClients {
			c.closeConnection(TLSHandshakeError)
		}
	}
}

// Reload reads the current configuration file and calls out to ReloadOptions
// to apply the changes. This returns an error if the server was not started
// with a config file or an option which doesn't support hot-swapping was changed.
func (s *Server) Reload() error {
	s.mu.Lock()
	configFile := s.configFile
	s.mu.Unlock()
	if configFile == "" {
		return errors.New("can only reload config when a file is provided using -c or --config")
	}

	newOpts, err := ProcessConfigFile(configFile)
	if err != nil {
		// TODO: Dump previous good config to a .bak file?
		return err
	}
	return s.ReloadOptions(newOpts)
}

// ReloadOptions applies any supported options from the provided Option
// type. This returns an error if an option which doesn't support
// hot-swapping was changed.
func (s *Server) ReloadOptions(newOpts *Options) error {
	s.mu.Lock()

	s.reloading = true
	defer func() {
		s.mu.Lock()
		s.reloading = false
		s.mu.Unlock()
	}()

	curOpts := s.getOpts()

	// Wipe trusted keys if needed when we have an operator.
	if len(curOpts.TrustedOperators) > 0 && len(curOpts.TrustedKeys) > 0 {
		curOpts.TrustedKeys = nil
	}

	clientOrgPort := curOpts.Port
	clusterOrgPort := curOpts.Cluster.Port
	gatewayOrgPort := curOpts.Gateway.Port
	leafnodesOrgPort := curOpts.LeafNode.Port
	websocketOrgPort := curOpts.Websocket.Port
	mqttOrgPort := curOpts.MQTT.Port

	s.mu.Unlock()

	// Apply flags over config file settings.
	newOpts = MergeOptions(newOpts, FlagSnapshot)

	// Need more processing for boolean flags...
	if FlagSnapshot != nil {
		applyBoolFlags(newOpts, FlagSnapshot)
	}

	setBaselineOptions(newOpts)

	// setBaselineOptions sets Port to 0 if set to -1 (RANDOM port)
	// If that's the case, set it to the saved value when the accept loop was
	// created.
	if newOpts.Port == 0 {
		newOpts.Port = clientOrgPort
	}
	// We don't do that for cluster, so check against -1.
	if newOpts.Cluster.Port == -1 {
		newOpts.Cluster.Port = clusterOrgPort
	}
	if newOpts.Gateway.Port == -1 {
		newOpts.Gateway.Port = gatewayOrgPort
	}
	if newOpts.LeafNode.Port == -1 {
		newOpts.LeafNode.Port = leafnodesOrgPort
	}
	if newOpts.Websocket.Port == -1 {
		newOpts.Websocket.Port = websocketOrgPort
	}
	if newOpts.MQTT.Port == -1 {
		newOpts.MQTT.Port = mqttOrgPort
	}

	if err := s.reloadOptions(curOpts, newOpts); err != nil {
		return err
	}

	s.recheckPinnedCerts(curOpts, newOpts)

	s.mu.Lock()
	s.configTime = time.Now().UTC()
	s.updateVarzConfigReloadableFields(s.varz)
	s.mu.Unlock()
	return nil
}
func applyBoolFlags(newOpts, flagOpts *Options) {
	// Reset fields that may have been set to `true` in
	// MergeOptions() when some of the flags default to `true`
	// but have not been explicitly set and therefore value
	// from config file should take precedence.
	for name, val := range newOpts.inConfig {
		f := reflect.ValueOf(newOpts).Elem()
		names := strings.Split(name, ".")
		for _, name := range names {
			f = f.FieldByName(name)
		}
		f.SetBool(val)
	}
	// Now apply value (true or false) from flags that have
	// been explicitly set in command line
	for name, val := range flagOpts.inCmdLine {
		f := reflect.ValueOf(newOpts).Elem()
		names := strings.Split(name, ".")
		for _, name := range names {
			f = f.FieldByName(name)
		}
		f.SetBool(val)
	}
}

// reloadOptions reloads the server config with the provided options. If an
// option that doesn't support hot-swapping is changed, this returns an error.
func (s *Server) reloadOptions(curOpts, newOpts *Options) error {
	// Apply to the new options some of the options that may have been set
	// that can't be configured in the config file (this can happen in
	// applications starting NATS Server programmatically).
	newOpts.CustomClientAuthentication = curOpts.CustomClientAuthentication
	newOpts.CustomRouterAuthentication = curOpts.CustomRouterAuthentication

	changed, err := s.diffOptions(newOpts)
	if err != nil {
		return err
	}

	if len(changed) != 0 {
		if err := validateOptions(newOpts); err != nil {
			return err
		}
	}

	// Create a context that is used to pass special info that we may need
	// while applying the new options.
	ctx := reloadContext{oldClusterPerms: curOpts.Cluster.Permissions}
	s.setOpts(newOpts)
	s.applyOptions(&ctx, changed)
	return nil
}

// For the purpose of comparing, impose a order on slice data types where order does not matter
func imposeOrder(value interface{}) error {
	switch value := value.(type) {
	case []*Account:
		sort.Slice(value, func(i, j int) bool {
			return value[i].Name < value[j].Name
		})
		for _, a := range value {
			sort.Slice(a.imports.streams, func(i, j int) bool {
				return a.imports.streams[i].acc.Name < a.imports.streams[j].acc.Name
			})
		}
	case []*User:
		sort.Slice(value, func(i, j int) bool {
			return value[i].Username < value[j].Username
		})
	case []*NkeyUser:
		sort.Slice(value, func(i, j int) bool {
			return value[i].Nkey < value[j].Nkey
		})
	case []*url.URL:
		sort.Slice(value, func(i, j int) bool {
			return value[i].String() < value[j].String()
		})
	case []string:
		sort.Strings(value)
	case []*jwt.OperatorClaims:
		sort.Slice(value, func(i, j int) bool {
			return value[i].Issuer < value[j].Issuer
		})
	case GatewayOpts:
		sort.Slice(value.Gateways, func(i, j int) bool {
			return value.Gateways[i].Name < value.Gateways[j].Name
		})
	case WebsocketOpts:
		sort.Strings(value.AllowedOrigins)
	case string, bool, uint8, int, int32, int64, time.Duration, float64, nil, LeafNodeOpts, ClusterOpts, *tls.Config, PinnedCertSet,
		*URLAccResolver, *MemAccResolver, *DirAccResolver, *CacheDirAccResolver, Authentication, MQTTOpts, jwt.TagList,
		*OCSPConfig, map[string]string, JSLimitOpts, StoreCipher:
		// explicitly skipped types
	default:
		// this will fail during unit tests
		return fmt.Errorf("OnReload, sort or explicitly skip type: %s",
			reflect.TypeOf(value))
	}
	return nil
}

// diffOptions returns a slice containing options which have been changed. If
// an option that doesn't support hot-swapping is changed, this returns an
// error.
func (s *Server) diffOptions(newOpts *Options) ([]option, error) {
	var (
		oldConfig = reflect.ValueOf(s.getOpts()).Elem()
		newConfig = reflect.ValueOf(newOpts).Elem()
		diffOpts  = []option{}

		// Need to keep track of whether JS is being disabled
		// to prevent changing limits at runtime.
		jsEnabled           = s.JetStreamEnabled()
		disableJS           bool
		jsMemLimitsChanged  bool
		jsFileLimitsChanged bool
		jsStoreDirChanged   bool
	)
	for i := 0; i < oldConfig.NumField(); i++ {
		field := oldConfig.Type().Field(i)
		// field.PkgPath is empty for exported fields, and is not for unexported ones.
		// We skip the unexported fields.
		if field.PkgPath != _EMPTY_ {
			continue
		}
		var (
			oldValue = oldConfig.Field(i).Interface()
			newValue = newConfig.Field(i).Interface()
		)
		if err := imposeOrder(oldValue); err != nil {
			return nil, err
		}
		if err := imposeOrder(newValue); err != nil {
			return nil, err
		}

		optName := strings.ToLower(field.Name)
		// accounts and users (referencing accounts) will always differ as accounts
		// contain internal state, say locks etc..., so we don't bother here.
		// This also avoids races with atomic stats counters
		if optName != "accounts" && optName != "users" {
			if changed := !reflect.DeepEqual(oldValue, newValue); !changed {
				// Check to make sure we are running JetStream if we think we should be.
				if optName == "jetstream" && newValue.(bool) {
					if !jsEnabled {
						diffOpts = append(diffOpts, &jetStreamOption{newValue: true})
					}
				}
				continue
			}
		}
		switch optName {
		case "traceverbose":
			diffOpts = append(diffOpts, &traceVerboseOption{newValue: newValue.(bool)})
		case "trace":
			diffOpts = append(diffOpts, &traceOption{newValue: newValue.(bool)})
		case "debug":
			diffOpts = append(diffOpts, &debugOption{newValue: newValue.(bool)})
		case "logtime":
			diffOpts = append(diffOpts, &logtimeOption{newValue: newValue.(bool)})
		case "logfile":
			diffOpts = append(diffOpts, &logfileOption{newValue: newValue.(string)})
		case "syslog":
			diffOpts = append(diffOpts, &syslogOption{newValue: newValue.(bool)})
		case "remotesyslog":
			diffOpts = append(diffOpts, &remoteSyslogOption{newValue: newValue.(string)})
		case "tlsconfig":
			diffOpts = append(diffOpts, &tlsOption{newValue: newValue.(*tls.Config)})
		case "tlstimeout":
			diffOpts = append(diffOpts, &tlsTimeoutOption{newValue: newValue.(float64)})
		case "tlspinnedcerts":
			diffOpts = append(diffOpts, &tlsPinnedCertOption{newValue: newValue.(PinnedCertSet)})
		case "username":
			diffOpts = append(diffOpts, &usernameOption{})
		case "password":
			diffOpts = append(diffOpts, &passwordOption{})
		case "tags":
			diffOpts = append(diffOpts, &tagsOption{})
		case "authorization":
			diffOpts = append(diffOpts, &authorizationOption{})
		case "authtimeout":
			diffOpts = append(diffOpts, &authTimeoutOption{newValue: newValue.(float64)})
		case "users":
			diffOpts = append(diffOpts, &usersOption{})
		case "nkeys":
			diffOpts = append(diffOpts, &nkeysOption{})
		case "cluster":
			newClusterOpts := newValue.(ClusterOpts)
			oldClusterOpts := oldValue.(ClusterOpts)
			if err := validateClusterOpts(oldClusterOpts, newClusterOpts); err != nil {
				return nil, err
			}
			permsChanged := !reflect.DeepEqual(newClusterOpts.Permissions, oldClusterOpts.Permissions)
			diffOpts = append(diffOpts, &clusterOption{newValue: newClusterOpts, permsChanged: permsChanged})
		case "routes":
			add, remove := diffRoutes(oldValue.([]*url.URL), newValue.([]*url.URL))
			diffOpts = append(diffOpts, &routesOption{add: add, remove: remove})
		case "maxconn":
			diffOpts = append(diffOpts, &maxConnOption{newValue: newValue.(int)})
		case "pidfile":
			diffOpts = append(diffOpts, &pidFileOption{newValue: newValue.(string)})
		case "portsfiledir":
			diffOpts = append(diffOpts, &portsFileDirOption{newValue: newValue.(string), oldValue: oldValue.(string)})
		case "maxcontrolline":
			diffOpts = append(diffOpts, &maxControlLineOption{newValue: newValue.(int32)})
		case "maxpayload":
			diffOpts = append(diffOpts, &maxPayloadOption{newValue: newValue.(int32)})
		case "pinginterval":
			diffOpts = append(diffOpts, &pingIntervalOption{newValue: newValue.(time.Duration)})
		case "maxpingsout":
			diffOpts = append(diffOpts, &maxPingsOutOption{newValue: newValue.(int)})
		case "writedeadline":
			diffOpts = append(diffOpts, &writeDeadlineOption{newValue: newValue.(time.Duration)})
		case "clientadvertise":
			cliAdv := newValue.(string)
			if cliAdv != "" {
				// Validate ClientAdvertise syntax
				if _, _, err := parseHostPort(cliAdv, 0); err != nil {
					return nil, fmt.Errorf("invalid ClientAdvertise value of %s, err=%v", cliAdv, err)
				}
			}
			diffOpts = append(diffOpts, &clientAdvertiseOption{newValue: cliAdv})
		case "accounts":
			diffOpts = append(diffOpts, &accountsOption{})
		case "resolver", "accountresolver", "accountsresolver":
			// We can't move from no resolver to one. So check for that.
			if (oldValue == nil && newValue != nil) ||
				(oldValue != nil && newValue == nil) {
				return nil, fmt.Errorf("config reload does not support moving to or from an account resolver")
			}
			diffOpts = append(diffOpts, &accountsOption{})
		case "accountresolvertlsconfig":
			diffOpts = append(diffOpts, &accountsOption{})
		case "gateway":
			// Not supported for now, but report warning if configuration of gateway
			// is actually changed so that user knows that it won't take effect.

			// Any deep-equal is likely to fail for when there is a TLSConfig. so
			// remove for the test.
			tmpOld := oldValue.(GatewayOpts)
			tmpNew := newValue.(GatewayOpts)
			tmpOld.TLSConfig = nil
			tmpNew.TLSConfig = nil
			tmpOld.tlsConfigOpts = nil
			tmpNew.tlsConfigOpts = nil

			// Need to do the same for remote gateways' TLS configs.
			// But we can't just set remotes' TLSConfig to nil otherwise this
			// would lose the real TLS configuration.
			tmpOld.Gateways = copyRemoteGWConfigsWithoutTLSConfig(tmpOld.Gateways)
			tmpNew.Gateways = copyRemoteGWConfigsWithoutTLSConfig(tmpNew.Gateways)

			// If there is really a change prevents reload.
			if !reflect.DeepEqual(tmpOld, tmpNew) {
				// See TODO(ik) note below about printing old/new values.
				return nil, fmt.Errorf("config reload not supported for %s: old=%v, new=%v",
					field.Name, oldValue, newValue)
			}
		case "leafnode":
			// Similar to gateways
			tmpOld := oldValue.(LeafNodeOpts)
			tmpNew := newValue.(LeafNodeOpts)
			tmpOld.TLSConfig = nil
			tmpNew.TLSConfig = nil
			tmpOld.tlsConfigOpts = nil
			tmpNew.tlsConfigOpts = nil

			// Need to do the same for remote leafnodes' TLS configs.
			// But we can't just set remotes' TLSConfig to nil otherwise this
			// would lose the real TLS configuration.
			tmpOld.Remotes = copyRemoteLNConfigForReloadCompare(tmpOld.Remotes)
			tmpNew.Remotes = copyRemoteLNConfigForReloadCompare(tmpNew.Remotes)

			// Special check for leafnode remotes changes which are not supported right now.
			leafRemotesChanged := func(a, b LeafNodeOpts) bool {
				if len(a.Remotes) != len(b.Remotes) {
					return true
				}

				// Check whether all remotes URLs are still the same.
				for _, oldRemote := range a.Remotes {
					var found bool

					if oldRemote.LocalAccount == _EMPTY_ {
						oldRemote.LocalAccount = globalAccountName
					}

					for _, newRemote := range b.Remotes {
						// Bind to global account in case not defined.
						if newRemote.LocalAccount == _EMPTY_ {
							newRemote.LocalAccount = globalAccountName
						}

						if reflect.DeepEqual(oldRemote, newRemote) {
							found = true
							break
						}
					}
					if !found {
						return true
					}
				}

				return false
			}

			// First check whether remotes changed at all. If they did not,
			// skip them in the complete equal check.
			if !leafRemotesChanged(tmpOld, tmpNew) {
				tmpOld.Remotes = nil
				tmpNew.Remotes = nil
			}

			// Special check for auth users to detect changes.
			// If anything is off will fall through and fail below.
			// If we detect they are semantically the same we nil them out
			// to pass the check below.
			if tmpOld.Users != nil || tmpNew.Users != nil {
				if len(tmpOld.Users) == len(tmpNew.Users) {
					oua := make(map[string]*User, len(tmpOld.Users))
					nua := make(map[string]*User, len(tmpOld.Users))
					for _, u := range tmpOld.Users {
						oua[u.Username] = u
					}
					for _, u := range tmpNew.Users {
						nua[u.Username] = u
					}
					same := true
					for uname, u := range oua {
						// If we can not find new one with same name, drop through to fail.
						nu, ok := nua[uname]
						if !ok {
							same = false
							break
						}
						// If username or password or account different break.
						if u.Username != nu.Username || u.Password != nu.Password || u.Account.GetName() != nu.Account.GetName() {
							same = false
							break
						}
					}
					// We can nil out here.
					if same {
						tmpOld.Users, tmpNew.Users = nil, nil
					}
				}
			}

			// If there is really a change prevents reload.
			if !reflect.DeepEqual(tmpOld, tmpNew) {
				// See TODO(ik) note below about printing old/new values.
				return nil, fmt.Errorf("config reload not supported for %s: old=%v, new=%v",
					field.Name, oldValue, newValue)
			}
		case "jetstream":
			new := newValue.(bool)
			old := oldValue.(bool)
			if new != old {
				diffOpts = append(diffOpts, &jetStreamOption{newValue: new})
			}

			// Mark whether JS will be disabled.
			disableJS = !new
		case "storedir":
			new := newValue.(string)
			old := oldValue.(string)
			modified := new != old

			// Check whether JS is being disabled and/or storage dir attempted to change.
			if jsEnabled && modified {
				if new == _EMPTY_ {
					// This means that either JS is being disabled or it is using an temp dir.
					// Allow the change but error in case JS was not disabled.
					jsStoreDirChanged = true
				} else {
					return nil, fmt.Errorf("config reload not supported for jetstream storage directory")
				}
			}
		case "jetstreammaxmemory", "jetstreammaxstore":
			old := oldValue.(int64)
			new := newValue.(int64)

			// Check whether JS is being disabled and/or limits are being changed.
			var (
				modified  = new != old
				fromUnset = old == -1
				fromSet   = !fromUnset
				toUnset   = new == -1
				toSet     = !toUnset
			)
			if jsEnabled && modified {
				// Cannot change limits from dynamic storage at runtime.
				switch {
				case fromSet && toUnset:
					// Limits changed but it may mean that JS is being disabled,
					// keep track of the change and error in case it is not.
					switch optName {
					case "jetstreammaxmemory":
						jsMemLimitsChanged = true
					case "jetstreammaxstore":
						jsFileLimitsChanged = true
					default:
						return nil, fmt.Errorf("config reload not supported for jetstream max memory and store")
					}
				case fromUnset && toSet:
					// Prevent changing from dynamic max memory / file at runtime.
					return nil, fmt.Errorf("config reload not supported for jetstream dynamic max memory and store")
				default:
					return nil, fmt.Errorf("config reload not supported for jetstream max memory and store")
				}
			}
		case "websocket":
			// Similar to gateways
			tmpOld := oldValue.(WebsocketOpts)
			tmpNew := newValue.(WebsocketOpts)
			tmpOld.TLSConfig = nil
			tmpNew.TLSConfig = nil
			// If there is really a change prevents reload.
			if !reflect.DeepEqual(tmpOld, tmpNew) {
				// See TODO(ik) note below about printing old/new values.
				return nil, fmt.Errorf("config reload not supported for %s: old=%v, new=%v",
					field.Name, oldValue, newValue)
			}
		case "mqtt":
			diffOpts = append(diffOpts, &mqttAckWaitReload{newValue: newValue.(MQTTOpts).AckWait})
			diffOpts = append(diffOpts, &mqttMaxAckPendingReload{newValue: newValue.(MQTTOpts).MaxAckPending})
			diffOpts = append(diffOpts, &mqttStreamReplicasReload{newValue: newValue.(MQTTOpts).StreamReplicas})
			diffOpts = append(diffOpts, &mqttConsumerReplicasReload{newValue: newValue.(MQTTOpts).ConsumerReplicas})
			diffOpts = append(diffOpts, &mqttConsumerMemoryStorageReload{newValue: newValue.(MQTTOpts).ConsumerMemoryStorage})
			diffOpts = append(diffOpts, &mqttInactiveThresholdReload{newValue: newValue.(MQTTOpts).ConsumerInactiveThreshold})

			// Nil out/set to 0 the options that we allow to be reloaded so that
			// we only fail reload if some that we don't support are changed.
			tmpOld := oldValue.(MQTTOpts)
			tmpNew := newValue.(MQTTOpts)
			tmpOld.TLSConfig, tmpOld.AckWait, tmpOld.MaxAckPending, tmpOld.StreamReplicas, tmpOld.ConsumerReplicas, tmpOld.ConsumerMemoryStorage = nil, 0, 0, 0, 0, false
			tmpOld.ConsumerInactiveThreshold = 0
			tmpNew.TLSConfig, tmpNew.AckWait, tmpNew.MaxAckPending, tmpNew.StreamReplicas, tmpNew.ConsumerReplicas, tmpNew.ConsumerMemoryStorage = nil, 0, 0, 0, 0, false
			tmpNew.ConsumerInactiveThreshold = 0

			if !reflect.DeepEqual(tmpOld, tmpNew) {
				// See TODO(ik) note below about printing old/new values.
				return nil, fmt.Errorf("config reload not supported for %s: old=%v, new=%v",
					field.Name, oldValue, newValue)
			}
			tmpNew.AckWait = newValue.(MQTTOpts).AckWait
			tmpNew.MaxAckPending = newValue.(MQTTOpts).MaxAckPending
			tmpNew.StreamReplicas = newValue.(MQTTOpts).StreamReplicas
			tmpNew.ConsumerReplicas = newValue.(MQTTOpts).ConsumerReplicas
			tmpNew.ConsumerMemoryStorage = newValue.(MQTTOpts).ConsumerMemoryStorage
			tmpNew.ConsumerInactiveThreshold = newValue.(MQTTOpts).ConsumerInactiveThreshold
		case "connecterrorreports":
			diffOpts = append(diffOpts, &connectErrorReports{newValue: newValue.(int)})
		case "reconnecterrorreports":
			diffOpts = append(diffOpts, &reconnectErrorReports{newValue: newValue.(int)})
		case "nolog", "nosigs":
			// Ignore NoLog and NoSigs options since they are not parsed and only used in
			// testing.
			continue
		case "disableshortfirstping":
			newOpts.DisableShortFirstPing = oldValue.(bool)
			continue
		case "maxtracedmsglen":
			diffOpts = append(diffOpts, &maxTracedMsgLenOption{newValue: newValue.(int)})
		case "port":
			// check to see if newValue == 0 and continue if so.
			if newValue == 0 {
				// ignore RANDOM_PORT
				continue
			}
			fallthrough
		case "noauthuser":
			if oldValue != _EMPTY_ && newValue == _EMPTY_ {
				for _, user := range newOpts.Users {
					if user.Username == oldValue {
						return nil, fmt.Errorf("config reload not supported for %s: old=%v, new=%v",
							field.Name, oldValue, newValue)
					}
				}
			} else {
				return nil, fmt.Errorf("config reload not supported for %s: old=%v, new=%v",
					field.Name, oldValue, newValue)
			}
		case "systemaccount":
			if oldValue != DEFAULT_SYSTEM_ACCOUNT || newValue != _EMPTY_ {
				return nil, fmt.Errorf("config reload not supported for %s: old=%v, new=%v",
					field.Name, oldValue, newValue)
			}
		case "ocspconfig":
			diffOpts = append(diffOpts, &ocspOption{newValue: newValue.(*OCSPConfig)})
		default:
			// TODO(ik): Implement String() on those options to have a nice print.
			// %v is difficult to figure what's what, %+v print private fields and
			// would print passwords. Tried json.Marshal but it is too verbose for
			// the URL array.

			// Bail out if attempting to reload any unsupported options.
			return nil, fmt.Errorf("config reload not supported for %s: old=%v, new=%v",
				field.Name, oldValue, newValue)
		}
	}

	// If not disabling JS but limits have changed then it is an error.
	if !disableJS {
		if jsMemLimitsChanged || jsFileLimitsChanged {
			return nil, fmt.Errorf("config reload not supported for jetstream max memory and max store")
		}
		if jsStoreDirChanged {
			return nil, fmt.Errorf("config reload not supported for jetstream storage dir")
		}
	}

	return diffOpts, nil
}

func copyRemoteGWConfigsWithoutTLSConfig(current []*RemoteGatewayOpts) []*RemoteGatewayOpts {
	l := len(current)
	if l == 0 {
		return nil
	}
	rgws := make([]*RemoteGatewayOpts, 0, l)
	for _, rcfg := range current {
		cp := *rcfg
		cp.TLSConfig = nil
		cp.tlsConfigOpts = nil
		rgws = append(rgws, &cp)
	}
	return rgws
}

func copyRemoteLNConfigForReloadCompare(current []*RemoteLeafOpts) []*RemoteLeafOpts {
	l := len(current)
	if l == 0 {
		return nil
	}
	rlns := make([]*RemoteLeafOpts, 0, l)
	for _, rcfg := range current {
		cp := *rcfg
		cp.TLSConfig = nil
		cp.tlsConfigOpts = nil
		// This is set only when processing a CONNECT, so reset here so that we
		// don't fail the DeepEqual comparison.
		cp.TLS = false
		// For now, remove DenyImports/Exports since those get modified at runtime
		// to add JS APIs.
		cp.DenyImports, cp.DenyExports = nil, nil
		rlns = append(rlns, &cp)
	}
	return rlns
}

func (s *Server) applyOptions(ctx *reloadContext, opts []option) {
	var (
		reloadLogging      = false
		reloadAuth         = false
		reloadClusterPerms = false
		reloadClientTrcLvl = false
		reloadJetstream    = false
		jsEnabled          = false
		reloadTLS          = false
		isStatszChange     = false
	)
	for _, opt := range opts {
		opt.Apply(s)
		if opt.IsLoggingChange() {
			reloadLogging = true
		}
		if opt.IsTraceLevelChange() {
			reloadClientTrcLvl = true
		}
		if opt.IsAuthChange() {
			reloadAuth = true
		}
		if opt.IsTLSChange() {
			reloadTLS = true
		}
		if opt.IsClusterPermsChange() {
			reloadClusterPerms = true
		}
		if opt.IsJetStreamChange() {
			reloadJetstream = true
			jsEnabled = opt.(*jetStreamOption).newValue
		}
		if opt.IsStatszChange() {
			isStatszChange = true
		}
	}

	if reloadLogging {
		s.ConfigureLogger()
	}
	if reloadClientTrcLvl {
		s.reloadClientTraceLevel()
	}
	if reloadAuth {
		s.reloadAuthorization()
	}
	if reloadClusterPerms {
		s.reloadClusterPermissions(ctx.oldClusterPerms)
	}

	if reloadJetstream {
		if !jsEnabled {
			s.DisableJetStream()
		} else if !s.JetStreamEnabled() {
			if err := s.restartJetStream(); err != nil {
				s.Warnf("Can't start JetStream: %v", err)
			}
		}
		// Make sure to reset the internal loop's version of JS.
		s.resetInternalLoopInfo()
	}
	if isStatszChange {
		s.sendStatszUpdate()
	}

	// For remote gateways and leafnodes, make sure that their TLS configuration
	// is updated (since the config is "captured" early and changes would otherwise
	// not be visible).
	newOpts := s.getOpts()
	if s.gateway.enabled {
		s.gateway.updateRemotesTLSConfig(newOpts)
	}
	if len(newOpts.LeafNode.Remotes) > 0 {
		s.updateRemoteLeafNodesTLSConfig(newOpts)
	}

	if reloadTLS {
		// Restart OCSP monitoring.
		if err := s.reloadOCSP(); err != nil {
			s.Warnf("Can't restart OCSP Stapling: %v", err)
		}
	}

	s.Noticef("Reloaded server configuration")
}

// This will send a reset to the internal send loop.
func (s *Server) resetInternalLoopInfo() {
	var resetCh chan struct{}
	s.mu.Lock()
	if s.sys != nil {
		// can't hold the lock as go routine reading it may be waiting for lock as well
		resetCh = s.sys.resetCh
	}
	s.mu.Unlock()

	if resetCh != nil {
		resetCh <- struct{}{}
	}
}

// Update all cached debug and trace settings for every client
func (s *Server) reloadClientTraceLevel() {
	opts := s.getOpts()

	if opts.NoLog {
		return
	}

	// Create a list of all clients.
	// Update their trace level when not holding server or gateway lock

	s.mu.Lock()
	clientCnt := 1 + len(s.clients) + len(s.grTmpClients) + len(s.routes) + len(s.leafs)
	s.mu.Unlock()

	s.gateway.RLock()
	clientCnt += len(s.gateway.in) + len(s.gateway.outo)
	s.gateway.RUnlock()

	clients := make([]*client, 0, clientCnt)

	s.mu.Lock()
	if s.eventsEnabled() {
		clients = append(clients, s.sys.client)
	}

	cMaps := []map[uint64]*client{s.clients, s.grTmpClients, s.routes, s.leafs}
	for _, m := range cMaps {
		for _, c := range m {
			clients = append(clients, c)
		}
	}
	s.mu.Unlock()

	s.gateway.RLock()
	for _, c := range s.gateway.in {
		clients = append(clients, c)
	}
	clients = append(clients, s.gateway.outo...)
	s.gateway.RUnlock()

	for _, c := range clients {
		// client.trace is commonly read while holding the lock
		c.mu.Lock()
		c.setTraceLevel()
		c.mu.Unlock()
	}
}

// reloadAuthorization reconfigures the server authorization settings,
// disconnects any clients who are no longer authorized, and removes any
// unauthorized subscriptions.
func (s *Server) reloadAuthorization() {
	// This map will contain the names of accounts that have their streams
	// import configuration changed.
	awcsti := make(map[string]struct{})
	checkJetStream := false
	s.mu.Lock()

	// This can not be changed for now so ok to check server's trustedKeys unlocked.
	// If plain configured accounts, process here.
	if s.trustedKeys == nil {
		// We need to drain the old accounts here since we have something
		// new configured. We do not want s.accounts to change since that would
		// mean adding a lock to lookupAccount which is what we are trying to
		// optimize for with the change from a map to a sync.Map.
		oldAccounts := make(map[string]*Account)
		s.accounts.Range(func(k, v interface{}) bool {
			acc := v.(*Account)
			acc.mu.Lock()
			oldAccounts[acc.Name] = acc
			// Need to clear out eventing timers since they close over this account and not the new one.
			clearTimer(&acc.etmr)
			clearTimer(&acc.ctmr)
			acc.mu.Unlock()
			s.accounts.Delete(k)
			return true
		})
		s.gacc = nil
		s.configureAccounts()
		s.configureAuthorization()
		s.mu.Unlock()

		s.accounts.Range(func(k, v interface{}) bool {
			newAcc := v.(*Account)
			if acc, ok := oldAccounts[newAcc.Name]; ok {
				// If account exist in latest config, "transfer" the account's
				// sublist and client map to the new account.
				acc.mu.RLock()
				newAcc.mu.Lock()
				if len(acc.clients) > 0 {
					newAcc.clients = make(map[*client]struct{}, len(acc.clients))
					for c := range acc.clients {
						newAcc.clients[c] = struct{}{}
					}
				}
				// Same for leafnodes
				newAcc.lleafs = append([]*client(nil), acc.lleafs...)

				newAcc.sl = acc.sl
				if acc.rm != nil {
					newAcc.rm = make(map[string]int32)
				}
				for k, v := range acc.rm {
					newAcc.rm[k] = v
				}
				// Transfer internal client state. The configureAccounts call from above may have set up a new one.
				// We need to use the old one, and the isid to not confuse internal subs.
				newAcc.ic, newAcc.isid = acc.ic, acc.isid
				// Transfer any JetStream state.
				newAcc.js = acc.js
				// Also transfer any internal accounting on different client types. We copy over all clients
				// so need to copy this as well for proper accounting going forward.
				newAcc.nrclients = acc.nrclients
				newAcc.sysclients = acc.sysclients
				newAcc.nleafs = acc.nleafs
				newAcc.nrleafs = acc.nrleafs
				// Process any reverse map entries.
				if len(acc.imports.rrMap) > 0 {
					newAcc.imports.rrMap = make(map[string][]*serviceRespEntry)
					for k, v := range acc.imports.rrMap {
						newAcc.imports.rrMap[k] = v
					}
				}
				newAcc.mu.Unlock()
				acc.mu.RUnlock()

				// Check if current and new config of this account are same
				// in term of stream imports.
				if !acc.checkStreamImportsEqual(newAcc) {
					awcsti[newAcc.Name] = struct{}{}
				}

				// We need to remove all old service import subs.
				acc.removeAllServiceImportSubs()
				newAcc.addAllServiceImportSubs()
			}
			return true
		})
		s.mu.Lock()
		// Check if we had a default system account.
		if s.sys != nil && s.sys.account != nil && !s.opts.NoSystemAccount {
			s.accounts.Store(s.sys.account.Name, s.sys.account)
		}
		// Double check any JetStream configs.
		checkJetStream = s.js != nil
	} else if s.opts.AccountResolver != nil {
		s.configureResolver()
		if _, ok := s.accResolver.(*MemAccResolver); ok {
			// Check preloads so we can issue warnings etc if needed.
			s.checkResolvePreloads()
			// With a memory resolver we want to do something similar to configured accounts.
			// We will walk the accounts and delete them if they are no longer present via fetch.
			// If they are present we will force a claim update to process changes.
			s.accounts.Range(func(k, v interface{}) bool {
				acc := v.(*Account)
				// Skip global account.
				if acc == s.gacc {
					return true
				}
				accName := acc.GetName()
				// Release server lock for following actions
				s.mu.Unlock()
				accClaims, claimJWT, _ := s.fetchAccountClaims(accName)
				if accClaims != nil {
					if err := s.updateAccountWithClaimJWT(acc, claimJWT); err != nil {
						s.Noticef("Reloaded: deleting account [bad claims]: %q", accName)
						s.accounts.Delete(k)
					}
				} else {
					s.Noticef("Reloaded: deleting account [removed]: %q", accName)
					s.accounts.Delete(k)
				}
				// Regrab server lock.
				s.mu.Lock()
				return true
			})
		}
	}

	var (
		cclientsa [64]*client
		cclients  = cclientsa[:0]
		clientsa  [64]*client
		clients   = clientsa[:0]
		routesa   [64]*client
		routes    = routesa[:0]
	)

	// Gather clients that changed accounts. We will close them and they
	// will reconnect, doing the right thing.
	for _, client := range s.clients {
		if s.clientHasMovedToDifferentAccount(client) {
			cclients = append(cclients, client)
		} else {
			clients = append(clients, client)
		}
	}
	for _, route := range s.routes {
		routes = append(routes, route)
	}
	// Check here for any system/internal clients which will not be in the servers map of normal clients.
	if s.sys != nil && s.sys.account != nil && !s.opts.NoSystemAccount {
		s.accounts.Store(s.sys.account.Name, s.sys.account)
	}

	s.accounts.Range(func(k, v interface{}) bool {
		acc := v.(*Account)
		acc.mu.RLock()
		// Check for sysclients accounting, ignore the system account.
		if acc.sysclients > 0 && (s.sys == nil || s.sys.account != acc) {
			for c := range acc.clients {
				if c.kind != CLIENT && c.kind != LEAF {
					clients = append(clients, c)
				}
			}
		}
		acc.mu.RUnlock()
		return true
	})

	var resetCh chan struct{}
	if s.sys != nil {
		// can't hold the lock as go routine reading it may be waiting for lock as well
		resetCh = s.sys.resetCh
	}
	s.mu.Unlock()

	if resetCh != nil {
		resetCh <- struct{}{}
	}

	// Check that publish retained messages sources are still allowed to publish.
	s.mqttCheckPubRetainedPerms()

	// Close clients that have moved accounts
	for _, client := range cclients {
		client.closeConnection(ClientClosed)
	}

	for _, c := range clients {
		// Disconnect any unauthorized clients.
		// Ignore internal clients.
		if (c.kind == CLIENT || c.kind == LEAF) && !s.isClientAuthorized(c) {
			c.authViolation()
			continue
		}
		// Check to make sure account is correct.
		c.swapAccountAfterReload()
		// Remove any unauthorized subscriptions and check for account imports.
		c.processSubsOnConfigReload(awcsti)
	}

	for _, route := range routes {
		// Disconnect any unauthorized routes.
		// Do this only for routes that were accepted, not initiated
		// because in the later case, we don't have the user name/password
		// of the remote server.
		if !route.isSolicitedRoute() && !s.isRouterAuthorized(route) {
			route.setNoReconnect()
			route.authViolation()
		}
	}

	if res := s.AccountResolver(); res != nil {
		res.Reload()
	}

	// We will double check all JetStream configs on a reload.
	if checkJetStream {
		if err := s.enableJetStreamAccounts(); err != nil {
			s.Errorf(err.Error())
		}
	}
}

// Returns true if given client current account has changed (or user
// no longer exist) in the new config, false if the user did not
// change accounts.
// Server lock is held on entry.
func (s *Server) clientHasMovedToDifferentAccount(c *client) bool {
	var (
		nu *NkeyUser
		u  *User
	)
	if c.opts.Nkey != "" {
		if s.nkeys != nil {
			nu = s.nkeys[c.opts.Nkey]
		}
	} else if c.opts.Username != "" {
		if s.users != nil {
			u = s.users[c.opts.Username]
		}
	} else {
		return false
	}
	// Get the current account name
	c.mu.Lock()
	var curAccName string
	if c.acc != nil {
		curAccName = c.acc.Name
	}
	c.mu.Unlock()
	if nu != nil && nu.Account != nil {
		return curAccName != nu.Account.Name
	} else if u != nil && u.Account != nil {
		return curAccName != u.Account.Name
	}
	// user/nkey no longer exists.
	return true
}

// reloadClusterPermissions reconfigures the cluster's permssions
// and set the permissions to all existing routes, sending an
// update INFO protocol so that remote can resend their local
// subs if needed, and sending local subs matching cluster's
// import subjects.
func (s *Server) reloadClusterPermissions(oldPerms *RoutePermissions) {
	s.mu.Lock()
	var (
		infoJSON     []byte
		newPerms     = s.opts.Cluster.Permissions
		routes       = make(map[uint64]*client, len(s.routes))
		withNewProto int
	)
	// Get all connected routes
	for i, route := range s.routes {
		// Count the number of routes that can understand receiving INFO updates.
		route.mu.Lock()
		if route.opts.Protocol >= RouteProtoInfo {
			withNewProto++
		}
		route.mu.Unlock()
		routes[i] = route
	}
	// If new permissions is nil, then clear routeInfo import/export
	if newPerms == nil {
		s.routeInfo.Import = nil
		s.routeInfo.Export = nil
	} else {
		s.routeInfo.Import = newPerms.Import
		s.routeInfo.Export = newPerms.Export
	}
	// Regenerate route INFO
	s.generateRouteInfoJSON()
	infoJSON = s.routeInfoJSON
	gacc := s.gacc
	s.mu.Unlock()

	// If there were no route, we are done
	if len(routes) == 0 {
		return
	}

	// If only older servers, simply close all routes and they will do the right
	// thing on reconnect.
	if withNewProto == 0 {
		for _, route := range routes {
			route.closeConnection(RouteRemoved)
		}
		return
	}

	// Fake clients to test cluster permissions
	oldPermsTester := &client{}
	oldPermsTester.setRoutePermissions(oldPerms)
	newPermsTester := &client{}
	newPermsTester.setRoutePermissions(newPerms)

	var (
		_localSubs       [4096]*subscription
		localSubs        = _localSubs[:0]
		subsNeedSUB      []*subscription
		subsNeedUNSUB    []*subscription
		deleteRoutedSubs []*subscription
	)
	// FIXME(dlc) - Change for accounts.
	gacc.sl.localSubs(&localSubs, false)

	// Go through all local subscriptions
	for _, sub := range localSubs {
		// Get all subs that can now be imported
		subj := string(sub.subject)
		couldImportThen := oldPermsTester.canImport(subj)
		canImportNow := newPermsTester.canImport(subj)
		if canImportNow {
			// If we could not before, then will need to send a SUB protocol.
			if !couldImportThen {
				subsNeedSUB = append(subsNeedSUB, sub)
			}
		} else if couldImportThen {
			// We were previously able to import this sub, but now
			// we can't so we need to send an UNSUB protocol
			subsNeedUNSUB = append(subsNeedUNSUB, sub)
		}
	}

	for _, route := range routes {
		route.mu.Lock()
		// If route is to older server, simply close connection.
		if route.opts.Protocol < RouteProtoInfo {
			route.mu.Unlock()
			route.closeConnection(RouteRemoved)
			continue
		}
		route.setRoutePermissions(newPerms)
		for _, sub := range route.subs {
			// If we can't export, we need to drop the subscriptions that
			// we have on behalf of this route.
			subj := string(sub.subject)
			if !route.canExport(subj) {
				delete(route.subs, string(sub.sid))
				deleteRoutedSubs = append(deleteRoutedSubs, sub)
			}
		}
		// Send an update INFO, which will allow remote server to show
		// our current route config in monitoring and resend subscriptions
		// that we now possibly allow with a change of Export permissions.
		route.enqueueProto(infoJSON)
		// Now send SUB and UNSUB protocols as needed.
		route.sendRouteSubProtos(subsNeedSUB, false, nil)
		route.sendRouteUnSubProtos(subsNeedUNSUB, false, nil)
		route.mu.Unlock()
	}
	// Remove as a batch all the subs that we have removed from each route.
	// FIXME(dlc) - Change for accounts.
	gacc.sl.RemoveBatch(deleteRoutedSubs)
}

// validateClusterOpts ensures the new ClusterOpts does not change host or
// port, which do not support reload.
func validateClusterOpts(old, new ClusterOpts) error {
	if old.Host != new.Host {
		return fmt.Errorf("config reload not supported for cluster host: old=%s, new=%s",
			old.Host, new.Host)
	}
	if old.Port != new.Port {
		return fmt.Errorf("config reload not supported for cluster port: old=%d, new=%d",
			old.Port, new.Port)
	}
	// Validate Cluster.Advertise syntax
	if new.Advertise != "" {
		if _, _, err := parseHostPort(new.Advertise, 0); err != nil {
			return fmt.Errorf("invalid Cluster.Advertise value of %s, err=%v", new.Advertise, err)
		}
	}
	return nil
}

// diffRoutes diffs the old routes and the new routes and returns the ones that
// should be added and removed from the server.
func diffRoutes(old, new []*url.URL) (add, remove []*url.URL) {
	// Find routes to remove.
removeLoop:
	for _, oldRoute := range old {
		for _, newRoute := range new {
			if urlsAreEqual(oldRoute, newRoute) {
				continue removeLoop
			}
		}
		remove = append(remove, oldRoute)
	}

	// Find routes to add.
addLoop:
	for _, newRoute := range new {
		for _, oldRoute := range old {
			if urlsAreEqual(oldRoute, newRoute) {
				continue addLoop
			}
		}
		add = append(add, newRoute)
	}

	return add, remove
}
