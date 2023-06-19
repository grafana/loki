// Copyright 2019-2022 The NATS Authors
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
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/minio/highwayhash"
	"github.com/nats-io/nats-server/v2/server/sysmem"
	"github.com/nats-io/nkeys"
	"github.com/nats-io/nuid"
)

// JetStreamConfig determines this server's configuration.
// MaxMemory and MaxStore are in bytes.
type JetStreamConfig struct {
	MaxMemory  int64  `json:"max_memory"`
	MaxStore   int64  `json:"max_storage"`
	StoreDir   string `json:"store_dir,omitempty"`
	Domain     string `json:"domain,omitempty"`
	CompressOK bool   `json:"compress_ok,omitempty"`
}

// Statistics about JetStream for this server.
type JetStreamStats struct {
	Memory         uint64            `json:"memory"`
	Store          uint64            `json:"storage"`
	ReservedMemory uint64            `json:"reserved_memory"`
	ReservedStore  uint64            `json:"reserved_storage"`
	Accounts       int               `json:"accounts"`
	HAAssets       int               `json:"ha_assets"`
	API            JetStreamAPIStats `json:"api"`
}

type JetStreamAccountLimits struct {
	MaxMemory            int64 `json:"max_memory"`
	MaxStore             int64 `json:"max_storage"`
	MaxStreams           int   `json:"max_streams"`
	MaxConsumers         int   `json:"max_consumers"`
	MaxAckPending        int   `json:"max_ack_pending"`
	MemoryMaxStreamBytes int64 `json:"memory_max_stream_bytes"`
	StoreMaxStreamBytes  int64 `json:"storage_max_stream_bytes"`
	MaxBytesRequired     bool  `json:"max_bytes_required"`
}

type JetStreamTier struct {
	Memory    uint64                 `json:"memory"`
	Store     uint64                 `json:"storage"`
	Streams   int                    `json:"streams"`
	Consumers int                    `json:"consumers"`
	Limits    JetStreamAccountLimits `json:"limits"`
}

// JetStreamAccountStats returns current statistics about the account's JetStream usage.
type JetStreamAccountStats struct {
	JetStreamTier                          // in case tiers are used, reflects totals with limits not set
	Domain        string                   `json:"domain,omitempty"`
	API           JetStreamAPIStats        `json:"api"`
	Tiers         map[string]JetStreamTier `json:"tiers,omitempty"` // indexed by tier name
}

type JetStreamAPIStats struct {
	Total    uint64 `json:"total"`
	Errors   uint64 `json:"errors"`
	Inflight uint64 `json:"inflight,omitempty"`
}

// This is for internal accounting for JetStream for this server.
type jetStream struct {
	// These are here first because of atomics on 32bit systems.
	apiInflight   int64
	apiTotal      int64
	apiErrors     int64
	memReserved   int64
	storeReserved int64
	memUsed       int64
	storeUsed     int64
	clustered     int32
	mu            sync.RWMutex
	srv           *Server
	config        JetStreamConfig
	cluster       *jetStreamCluster
	accounts      map[string]*jsAccount
	apiSubs       *Sublist
	started       time.Time

	// System level request to purge a stream move
	accountPurge   *subscription
	metaRecovering bool
	standAlone     bool
	disabled       bool
	oos            bool
}

type remoteUsage struct {
	tiers map[string]*jsaUsage // indexed by tier name
	api   uint64
	err   uint64
}

type jsaStorage struct {
	total jsaUsage
	local jsaUsage
}

// This represents a jetstream enabled account.
// Worth noting that we include the jetstream pointer, this is because
// in general we want to be very efficient when receiving messages on
// an internal sub for a stream, so we will direct link to the stream
// and walk backwards as needed vs multiple hash lookups and locks, etc.
type jsAccount struct {
	mu        sync.RWMutex
	js        *jetStream
	account   *Account
	storeDir  string
	inflight  sync.Map
	streams   map[string]*stream
	templates map[string]*streamTemplate
	store     TemplateStore

	// From server
	sendq *ipQueue[*pubMsg]

	// Usage/limits related fields that will be protected by usageMu
	usageMu    sync.RWMutex
	limits     map[string]JetStreamAccountLimits // indexed by tierName
	usage      map[string]*jsaStorage            // indexed by tierName
	rusage     map[string]*remoteUsage           // indexed by node id
	apiTotal   uint64
	apiErrors  uint64
	usageApi   uint64
	usageErr   uint64
	updatesPub string
	updatesSub *subscription
	lupdate    time.Time
	utimer     *time.Timer
}

// Track general usage for this account.
type jsaUsage struct {
	mem   int64
	store int64
}

// EnableJetStream will enable JetStream support on this server with the given configuration.
// A nil configuration will dynamically choose the limits and temporary file storage directory.
func (s *Server) EnableJetStream(config *JetStreamConfig) error {
	if s.JetStreamEnabled() {
		return fmt.Errorf("jetstream already enabled")
	}

	s.Noticef("Starting JetStream")
	if config == nil || config.MaxMemory <= 0 || config.MaxStore <= 0 {
		var storeDir, domain string
		var maxStore, maxMem int64
		if config != nil {
			storeDir, domain = config.StoreDir, config.Domain
			maxStore, maxMem = config.MaxStore, config.MaxMemory
		}
		config = s.dynJetStreamConfig(storeDir, maxStore, maxMem)
		if maxMem > 0 {
			config.MaxMemory = maxMem
		}
		if domain != _EMPTY_ {
			config.Domain = domain
		}
		s.Debugf("JetStream creating dynamic configuration - %s memory, %s disk", friendlyBytes(config.MaxMemory), friendlyBytes(config.MaxStore))
	} else if config.StoreDir != _EMPTY_ {
		config.StoreDir = filepath.Join(config.StoreDir, JetStreamStoreDir)
	}

	cfg := *config
	if cfg.StoreDir == _EMPTY_ {
		cfg.StoreDir = filepath.Join(os.TempDir(), JetStreamStoreDir)
	}

	// We will consistently place the 'jetstream' directory under the storedir that was handed to us. Prior to 2.2.3 though
	// we could have a directory on disk without the 'jetstream' directory. This will check and fix if needed.
	if err := s.checkStoreDir(&cfg); err != nil {
		return err
	}

	return s.enableJetStream(cfg)
}

// Function signature to generate a key encryption key.
type keyGen func(context []byte) ([]byte, error)

// Return a key generation function or nil if encryption not enabled.
// keyGen defined in filestore.go - keyGen func(iv, context []byte) []byte
func (s *Server) jsKeyGen(info string) keyGen {
	if ek := s.getOpts().JetStreamKey; ek != _EMPTY_ {
		return func(context []byte) ([]byte, error) {
			h := hmac.New(sha256.New, []byte(ek))
			if _, err := h.Write([]byte(info)); err != nil {
				return nil, err
			}
			if _, err := h.Write(context); err != nil {
				return nil, err
			}
			return h.Sum(nil), nil
		}
	}
	return nil
}

// Decode the encrypted metafile.
func (s *Server) decryptMeta(sc StoreCipher, ekey, buf []byte, acc, context string) ([]byte, error) {
	if len(ekey) < minMetaKeySize {
		return nil, errBadKeySize
	}
	prf := s.jsKeyGen(acc)
	if prf == nil {
		return nil, errNoEncryption
	}
	rb, err := prf([]byte(context))
	if err != nil {
		return nil, err
	}

	kek, err := genEncryptionKey(sc, rb)
	if err != nil {
		return nil, err
	}
	ns := kek.NonceSize()
	seed, err := kek.Open(nil, ekey[:ns], ekey[ns:], nil)
	if err != nil {
		return nil, err
	}
	aek, err := genEncryptionKey(sc, seed)
	if err != nil {
		return nil, err
	}
	plain, err := aek.Open(nil, buf[:ns], buf[ns:], nil)
	if err != nil {
		return nil, err
	}
	return plain, nil
}

// Check to make sure directory has the jetstream directory.
// We will have it properly configured here now regardless, so need to look inside.
func (s *Server) checkStoreDir(cfg *JetStreamConfig) error {
	fis, _ := os.ReadDir(cfg.StoreDir)
	// If we have nothing underneath us, could be just starting new, but if we see this we can check.
	if len(fis) != 0 {
		return nil
	}
	// Let's check the directory above. If it has us 'jetstream' but also other stuff that we can
	// identify as accounts then we can fix.
	fis, _ = os.ReadDir(filepath.Dir(cfg.StoreDir))
	// If just one that is us 'jetstream' and all is ok.
	if len(fis) == 1 {
		return nil
	}

	haveJetstreamDir := false
	for _, fi := range fis {
		if fi.Name() == JetStreamStoreDir {
			haveJetstreamDir = true
			break
		}
	}

	for _, fi := range fis {
		// Skip the 'jetstream' directory.
		if fi.Name() == JetStreamStoreDir {
			continue
		}
		// Let's see if this is an account.
		if accName := fi.Name(); accName != _EMPTY_ {
			_, ok := s.accounts.Load(accName)
			if !ok && s.AccountResolver() != nil && nkeys.IsValidPublicAccountKey(accName) {
				// Account is not local but matches the NKEY account public key,
				// this is enough indication to move this directory, no need to
				// fetch the account.
				ok = true
			}
			// If this seems to be an account go ahead and move the directory. This will include all assets
			// like streams and consumers.
			if ok {
				if !haveJetstreamDir {
					err := os.Mkdir(filepath.Join(filepath.Dir(cfg.StoreDir), JetStreamStoreDir), defaultDirPerms)
					if err != nil {
						return err
					}
					haveJetstreamDir = true
				}
				old := filepath.Join(filepath.Dir(cfg.StoreDir), fi.Name())
				new := filepath.Join(cfg.StoreDir, fi.Name())
				s.Noticef("JetStream relocated account %q to %q", old, new)
				if err := os.Rename(old, new); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// enableJetStream will start up the JetStream subsystem.
func (s *Server) enableJetStream(cfg JetStreamConfig) error {
	js := &jetStream{srv: s, config: cfg, accounts: make(map[string]*jsAccount), apiSubs: NewSublistNoCache()}
	s.gcbMu.Lock()
	if s.gcbOutMax = s.getOpts().JetStreamMaxCatchup; s.gcbOutMax == 0 {
		s.gcbOutMax = defaultMaxTotalCatchupOutBytes
	}
	s.gcbMu.Unlock()

	s.mu.Lock()
	s.js = js
	s.mu.Unlock()

	// FIXME(dlc) - Allow memory only operation?
	if stat, err := os.Stat(cfg.StoreDir); os.IsNotExist(err) {
		if err := os.MkdirAll(cfg.StoreDir, defaultDirPerms); err != nil {
			return fmt.Errorf("could not create storage directory - %v", err)
		}
	} else {
		// Make sure its a directory and that we can write to it.
		if stat == nil || !stat.IsDir() {
			return fmt.Errorf("storage directory is not a directory")
		}
		tmpfile, err := os.CreateTemp(cfg.StoreDir, "_test_")
		if err != nil {
			return fmt.Errorf("storage directory is not writable")
		}
		tmpfile.Close()
		os.Remove(tmpfile.Name())
	}

	// JetStream is an internal service so we need to make sure we have a system account.
	// This system account will export the JetStream service endpoints.
	if s.SystemAccount() == nil {
		s.SetDefaultSystemAccount()
	}

	s.Noticef("    _ ___ _____ ___ _____ ___ ___   _   __  __")
	s.Noticef(" _ | | __|_   _/ __|_   _| _ \\ __| /_\\ |  \\/  |")
	s.Noticef("| || | _|  | | \\__ \\ | | |   / _| / _ \\| |\\/| |")
	s.Noticef(" \\__/|___| |_| |___/ |_| |_|_\\___/_/ \\_\\_|  |_|")
	s.Noticef("")
	s.Noticef("         https://docs.nats.io/jetstream")
	s.Noticef("")
	s.Noticef("---------------- JETSTREAM ----------------")
	s.Noticef("  Max Memory:      %s", friendlyBytes(cfg.MaxMemory))
	s.Noticef("  Max Storage:     %s", friendlyBytes(cfg.MaxStore))
	s.Noticef("  Store Directory: \"%s\"", cfg.StoreDir)
	if cfg.Domain != _EMPTY_ {
		s.Noticef("  Domain:          %s", cfg.Domain)
	}
	opts := s.getOpts()
	if ek := opts.JetStreamKey; ek != _EMPTY_ {
		s.Noticef("  Encryption:      %s", opts.JetStreamCipher)
	}
	s.Noticef("-------------------------------------------")

	// Setup our internal subscriptions.
	if err := s.setJetStreamExportSubs(); err != nil {
		return fmt.Errorf("setting up internal jetstream subscriptions failed: %v", err)
	}

	// Setup our internal system exports.
	s.Debugf("  Exports:")
	s.Debugf("     %s", jsAllAPI)
	s.setupJetStreamExports()

	standAlone, canExtend := s.standAloneMode(), s.canExtendOtherDomain()
	if standAlone && canExtend && s.getOpts().JetStreamExtHint != jsWillExtend {
		canExtend = false
		s.Noticef("Standalone server started in clustered mode do not support extending domains")
		s.Noticef(`Manually disable standalone mode by setting the JetStream Option "extension_hint: %s"`, jsWillExtend)
	}

	// Indicate if we will be standalone for checking resource reservations, etc.
	js.setJetStreamStandAlone(standAlone && !canExtend)

	// Enable accounts and restore state before starting clustering.
	if err := s.enableJetStreamAccounts(); err != nil {
		return err
	}

	// If we are in clustered mode go ahead and start the meta controller.
	if !standAlone || canExtend {
		if err := s.enableJetStreamClustering(); err != nil {
			return err
		}
	}

	// Mark when we are up and running.
	js.setStarted()

	return nil
}

const jsNoExtend = "no_extend"
const jsWillExtend = "will_extend"

// This will check if we have a solicited leafnode that shares the system account
// and extension is not manually disabled
func (s *Server) canExtendOtherDomain() bool {
	opts := s.getOpts()
	sysAcc := s.SystemAccount().GetName()
	for _, r := range opts.LeafNode.Remotes {
		if r.LocalAccount == sysAcc {
			for _, denySub := range r.DenyImports {
				if subjectIsSubsetMatch(denySub, raftAllSubj) {
					return false
				}
			}
			return true
		}
	}
	return false
}

func (s *Server) updateJetStreamInfoStatus(enabled bool) {
	s.mu.Lock()
	s.info.JetStream = enabled
	s.mu.Unlock()
}

// restartJetStream will try to re-enable JetStream during a reload if it had been disabled during runtime.
func (s *Server) restartJetStream() error {
	opts := s.getOpts()
	cfg := JetStreamConfig{
		StoreDir:  opts.StoreDir,
		MaxMemory: opts.JetStreamMaxMemory,
		MaxStore:  opts.JetStreamMaxStore,
		Domain:    opts.JetStreamDomain,
	}
	s.Noticef("Restarting JetStream")
	err := s.EnableJetStream(&cfg)
	if err != nil {
		s.Warnf("Can't start JetStream: %v", err)
		return s.DisableJetStream()
	}
	s.updateJetStreamInfoStatus(true)
	return nil
}

// checkStreamExports will check if we have the JS exports setup
// on the system account, and if not go ahead and set them up.
func (s *Server) checkJetStreamExports() {
	if sacc := s.SystemAccount(); sacc != nil {
		sacc.mu.RLock()
		se := sacc.getServiceExport(jsAllAPI)
		sacc.mu.RUnlock()
		if se == nil {
			s.setupJetStreamExports()
		}
	}
}

func (s *Server) setupJetStreamExports() {
	// Setup our internal system export.
	if err := s.SystemAccount().AddServiceExport(jsAllAPI, nil); err != nil {
		s.Warnf("Error setting up jetstream service exports: %v", err)
	}
}

func (s *Server) jetStreamOOSPending() (wasPending bool) {
	s.mu.Lock()
	js := s.js
	s.mu.Unlock()
	if js != nil {
		js.mu.Lock()
		wasPending = js.oos
		js.oos = true
		js.mu.Unlock()
	}
	return wasPending
}

func (s *Server) setJetStreamDisabled() {
	s.mu.Lock()
	js := s.js
	s.mu.Unlock()
	if js != nil {
		js.mu.Lock()
		js.disabled = true
		js.mu.Unlock()
	}
}

func (s *Server) handleOutOfSpace(mset *stream) {
	if s.JetStreamEnabled() && !s.jetStreamOOSPending() {
		var stream string
		if mset != nil {
			stream = mset.name()
			s.Errorf("JetStream out of %s resources, will be DISABLED", mset.Store().Type())
		} else {
			s.Errorf("JetStream out of resources, will be DISABLED")
		}

		go s.DisableJetStream()

		adv := &JSServerOutOfSpaceAdvisory{
			TypedEvent: TypedEvent{
				Type: JSServerOutOfStorageAdvisoryType,
				ID:   nuid.Next(),
				Time: time.Now().UTC(),
			},
			Server:   s.Name(),
			ServerID: s.ID(),
			Stream:   stream,
			Cluster:  s.cachedClusterName(),
			Domain:   s.getOpts().JetStreamDomain,
		}
		s.publishAdvisory(nil, JSAdvisoryServerOutOfStorage, adv)
	}
}

// DisableJetStream will turn off JetStream and signals in clustered mode
// to have the metacontroller remove us from the peer list.
func (s *Server) DisableJetStream() error {
	if !s.JetStreamEnabled() {
		return nil
	}

	s.setJetStreamDisabled()

	if s.JetStreamIsClustered() {
		isLeader := s.JetStreamIsLeader()
		js, cc := s.getJetStreamCluster()
		if js == nil {
			s.shutdownJetStream()
			return nil
		}
		js.mu.RLock()
		meta := cc.meta
		js.mu.RUnlock()

		if meta != nil {
			if isLeader {
				s.Warnf("JetStream initiating meta leader transfer")
				meta.StepDown()
				select {
				case <-s.quitCh:
					return nil
				case <-time.After(2 * time.Second):
				}
				if !s.JetStreamIsCurrent() {
					s.Warnf("JetStream timeout waiting for meta leader transfer")
				}
			}
			meta.Delete()
		}
	}

	// Update our info status.
	s.updateJetStreamInfoStatus(false)

	// Normal shutdown.
	s.shutdownJetStream()

	// Shut down the RAFT groups.
	s.shutdownRaftNodes()

	return nil
}

func (s *Server) enableJetStreamAccounts() error {
	// If we have no configured accounts setup then setup imports on global account.
	if s.globalAccountOnly() {
		gacc := s.GlobalAccount()
		gacc.mu.Lock()
		if len(gacc.jsLimits) == 0 {
			gacc.jsLimits = defaultJSAccountTiers
		}
		gacc.mu.Unlock()
		if err := s.configJetStream(gacc); err != nil {
			return err
		}
	} else if err := s.configAllJetStreamAccounts(); err != nil {
		return fmt.Errorf("Error enabling jetstream on configured accounts: %v", err)
	}
	return nil
}

// enableAllJetStreamServiceImportsAndMappings turns on all service imports and mappings for jetstream for this account.
func (a *Account) enableAllJetStreamServiceImportsAndMappings() error {
	a.mu.RLock()
	s := a.srv
	a.mu.RUnlock()

	if s == nil {
		return fmt.Errorf("jetstream account not registered")
	}

	if !a.serviceImportExists(jsAllAPI) {
		if err := a.AddServiceImport(s.SystemAccount(), jsAllAPI, _EMPTY_); err != nil {
			return fmt.Errorf("Error setting up jetstream service imports for account: %v", err)
		}
	}

	// Check if we have a Domain specified.
	// If so add in a subject mapping that will allow local connected clients to reach us here as well.
	if opts := s.getOpts(); opts.JetStreamDomain != _EMPTY_ {
		mappings := generateJSMappingTable(opts.JetStreamDomain)
		a.mu.RLock()
		for _, m := range a.mappings {
			delete(mappings, m.src)
		}
		a.mu.RUnlock()
		for src, dest := range mappings {
			if err := a.AddMapping(src, dest); err != nil {
				s.Errorf("Error adding JetStream domain mapping: %v", err)
			}
		}
	}

	return nil
}

// enableJetStreamEnabledServiceImportOnly will enable the single service import responder.
// Should we do them all regardless?
func (a *Account) enableJetStreamInfoServiceImportOnly() error {
	// Check if this import would be overshadowed. This can happen when accounts
	// are importing from another account for JS access.
	if a.serviceImportShadowed(JSApiAccountInfo) {
		return nil
	}

	return a.enableAllJetStreamServiceImportsAndMappings()
}

func (s *Server) configJetStream(acc *Account) error {
	if acc == nil {
		return nil
	}
	acc.mu.RLock()
	jsLimits := acc.jsLimits
	acc.mu.RUnlock()
	if jsLimits != nil {
		// Check if already enabled. This can be during a reload.
		if acc.JetStreamEnabled() {
			if err := acc.enableAllJetStreamServiceImportsAndMappings(); err != nil {
				return err
			}
			if err := acc.UpdateJetStreamLimits(jsLimits); err != nil {
				return err
			}
		} else {
			if err := acc.EnableJetStream(jsLimits); err != nil {
				return err
			}
			if s.gateway.enabled {
				s.switchAccountToInterestMode(acc.GetName())
			}
		}
	} else if acc != s.SystemAccount() {
		if acc.JetStreamEnabled() {
			acc.DisableJetStream()
		}
		// We will setup basic service imports to respond to
		// requests if JS is enabled for this account.
		if err := acc.enableJetStreamInfoServiceImportOnly(); err != nil {
			return err
		}
	}
	return nil
}

// configAllJetStreamAccounts walk all configured accounts and turn on jetstream if requested.
func (s *Server) configAllJetStreamAccounts() error {
	// Check to see if system account has been enabled. We could arrive here via reload and
	// a non-default system account.
	s.checkJetStreamExports()

	// Snapshot into our own list. Might not be needed.
	s.mu.Lock()
	// Bail if server not enabled. If it was enabled and a reload turns it off
	// that will be handled elsewhere.
	js := s.js
	if js == nil {
		s.mu.Unlock()
		return nil
	}

	if s.sys != nil {
		// clustered stream removal will perform this cleanup as well
		// this is mainly for initial cleanup
		saccName := s.sys.account.Name
		accStoreDirs, _ := os.ReadDir(js.config.StoreDir)
		for _, acc := range accStoreDirs {
			if accName := acc.Name(); accName != saccName {
				// no op if not empty
				accDir := filepath.Join(js.config.StoreDir, accName)
				os.Remove(filepath.Join(accDir, streamsDir))
				os.Remove(accDir)
			}
		}
	}

	var jsAccounts []*Account
	s.accounts.Range(func(k, v interface{}) bool {
		jsAccounts = append(jsAccounts, v.(*Account))
		return true
	})
	accounts := &s.accounts
	s.mu.Unlock()

	// Process any jetstream enabled accounts here. These will be accounts we are
	// already aware of at startup etc.
	for _, acc := range jsAccounts {
		if err := s.configJetStream(acc); err != nil {
			return err
		}
	}

	// Now walk all the storage we have and resolve any accounts that we did not process already.
	// This is important in resolver/operator models.
	fis, _ := os.ReadDir(js.config.StoreDir)
	for _, fi := range fis {
		if accName := fi.Name(); accName != _EMPTY_ {
			// Only load up ones not already loaded since they are processed above.
			if _, ok := accounts.Load(accName); !ok {
				if acc, err := s.lookupAccount(accName); err != nil && acc != nil {
					if err := s.configJetStream(acc); err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}

// Mark our started time.
func (js *jetStream) setStarted() {
	js.mu.Lock()
	defer js.mu.Unlock()
	js.started = time.Now()
}

func (js *jetStream) isEnabled() bool {
	if js == nil {
		return false
	}
	js.mu.RLock()
	defer js.mu.RUnlock()
	return !js.disabled
}

// Mark that we will be in standlone mode.
func (js *jetStream) setJetStreamStandAlone(isStandAlone bool) {
	if js == nil {
		return
	}
	js.mu.Lock()
	defer js.mu.Unlock()
	js.standAlone = isStandAlone

	if isStandAlone {
		js.accountPurge, _ = js.srv.systemSubscribe(JSApiAccountPurge, _EMPTY_, false, nil, js.srv.jsLeaderAccountPurgeRequest)
	} else if js.accountPurge != nil {
		js.srv.sysUnsubscribe(js.accountPurge)
	}
}

// JetStreamEnabled reports if jetstream is enabled for this server.
func (s *Server) JetStreamEnabled() bool {
	var js *jetStream
	s.mu.RLock()
	js = s.js
	s.mu.RUnlock()
	return js.isEnabled()
}

// JetStreamEnabledForDomain will report if any servers have JetStream enabled within this domain.
func (s *Server) JetStreamEnabledForDomain() bool {
	if s.JetStreamEnabled() {
		return true
	}

	var jsFound bool
	// If we are here we do not have JetStream enabled for ourselves, but we need to check all connected servers.
	// TODO(dlc) - Could optimize and memoize this.
	s.nodeToInfo.Range(func(k, v interface{}) bool {
		// This should not be dependent on online status, so only check js.
		if v.(nodeInfo).js {
			jsFound = true
			return false
		}
		return true
	})

	return jsFound
}

// Will signal that all pull requests for consumers on this server are now invalid.
func (s *Server) signalPullConsumers() {
	js := s.getJetStream()
	if js == nil {
		return
	}

	js.mu.RLock()
	defer js.mu.RUnlock()

	// In case we have stale pending requests.
	const hdr = "NATS/1.0 409 Server Shutdown\r\n" + JSPullRequestPendingMsgs + ": %d\r\n" + JSPullRequestPendingBytes + ": %d\r\n\r\n"
	var didSend bool

	for _, jsa := range js.accounts {
		jsa.mu.RLock()
		for _, stream := range jsa.streams {
			stream.mu.RLock()
			for _, o := range stream.consumers {
				o.mu.RLock()
				// Only signal on R1.
				if o.cfg.Replicas <= 1 {
					for reply, wr := range o.pendingRequests() {
						shdr := fmt.Sprintf(hdr, wr.n, wr.b)
						o.outq.send(newJSPubMsg(reply, _EMPTY_, _EMPTY_, []byte(shdr), nil, nil, 0))
						didSend = true
					}
				}
				o.mu.RUnlock()
			}
			stream.mu.RUnlock()
		}
		jsa.mu.RUnlock()
	}
	// Give time for migration information to make it out of our server.
	if didSend {
		time.Sleep(50 * time.Millisecond)
	}
}

// Shutdown jetstream for this server.
func (s *Server) shutdownJetStream() {
	s.mu.RLock()
	js := s.js
	s.mu.RUnlock()

	if js == nil {
		return
	}

	s.Noticef("Initiating JetStream Shutdown...")
	defer s.Noticef("JetStream Shutdown")

	// If we have folks blocked on sync requests, unblock.
	// Send 1 is enough, but use select in case they were all present.
	select {
	case s.syncOutSem <- struct{}{}:
	default:
	}

	var _a [512]*Account
	accounts := _a[:0]

	js.mu.Lock()
	// Collect accounts.
	for _, jsa := range js.accounts {
		if a := jsa.acc(); a != nil {
			accounts = append(accounts, a)
		}
	}
	accPurgeSub := js.accountPurge
	js.accountPurge = nil
	js.mu.Unlock()

	if accPurgeSub != nil {
		s.sysUnsubscribe(accPurgeSub)
	}

	for _, a := range accounts {
		a.removeJetStream()
	}

	s.mu.Lock()
	s.js = nil
	s.mu.Unlock()

	js.mu.Lock()
	js.accounts = nil

	var qch chan struct{}

	if cc := js.cluster; cc != nil {
		if cc.qch != nil {
			qch = cc.qch
			cc.qch = nil
		}
		js.stopUpdatesSub()
		if cc.c != nil {
			cc.c.closeConnection(ClientClosed)
			cc.c = nil
		}
		cc.meta = nil
	}
	js.mu.Unlock()

	// If we were clustered signal the monitor cluster go routine.
	// We will wait for a bit for it to close.
	// Do this without the lock.
	if qch != nil {
		select {
		case qch <- struct{}{}:
			select {
			case <-qch:
			case <-time.After(2 * time.Second):
				s.Warnf("Did not receive signal for successful shutdown of cluster routine")
			}
		default:
		}
	}
}

// JetStreamConfig will return the current config. Useful if the system
// created a dynamic configuration. A copy is returned.
func (s *Server) JetStreamConfig() *JetStreamConfig {
	var c *JetStreamConfig
	s.mu.Lock()
	if s.js != nil {
		copy := s.js.config
		c = &(copy)
	}
	s.mu.Unlock()
	return c
}

func (s *Server) StoreDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.js == nil {
		return _EMPTY_
	}
	return s.js.config.StoreDir
}

// JetStreamNumAccounts returns the number of enabled accounts this server is tracking.
func (s *Server) JetStreamNumAccounts() int {
	js := s.getJetStream()
	if js == nil {
		return 0
	}
	js.mu.Lock()
	defer js.mu.Unlock()
	return len(js.accounts)
}

// JetStreamReservedResources returns the reserved resources if JetStream is enabled.
func (s *Server) JetStreamReservedResources() (int64, int64, error) {
	js := s.getJetStream()
	if js == nil {
		return -1, -1, NewJSNotEnabledForAccountError()
	}
	js.mu.RLock()
	defer js.mu.RUnlock()
	return js.memReserved, js.storeReserved, nil
}

func (s *Server) getJetStream() *jetStream {
	s.mu.RLock()
	js := s.js
	s.mu.RUnlock()
	return js
}

func (a *Account) assignJetStreamLimits(limits map[string]JetStreamAccountLimits) {
	a.mu.Lock()
	a.jsLimits = limits
	a.mu.Unlock()
}

// EnableJetStream will enable JetStream on this account with the defined limits.
// This is a helper for JetStreamEnableAccount.
func (a *Account) EnableJetStream(limits map[string]JetStreamAccountLimits) error {
	a.mu.RLock()
	s := a.srv
	a.mu.RUnlock()

	if s == nil {
		return fmt.Errorf("jetstream account not registered")
	}

	if s.SystemAccount() == a {
		return fmt.Errorf("jetstream can not be enabled on the system account")
	}

	s.mu.Lock()
	sendq := s.sys.sendq
	s.mu.Unlock()

	// No limits means we dynamically set up limits.
	// We also place limits here so we know that the account is configured for JetStream.
	if len(limits) == 0 {
		limits = defaultJSAccountTiers
	}

	a.assignJetStreamLimits(limits)

	js := s.getJetStream()
	if js == nil {
		return NewJSNotEnabledError()
	}

	js.mu.Lock()
	if _, ok := js.accounts[a.Name]; ok && a.JetStreamEnabled() {
		js.mu.Unlock()
		return fmt.Errorf("jetstream already enabled for account")
	}

	// Check the limits against existing reservations.
	if err := js.sufficientResources(limits); err != nil {
		js.mu.Unlock()
		return err
	}

	sysNode := s.Node()

	jsa := &jsAccount{js: js, account: a, limits: limits, streams: make(map[string]*stream), sendq: sendq, usage: make(map[string]*jsaStorage)}
	jsa.storeDir = filepath.Join(js.config.StoreDir, a.Name)

	jsa.usageMu.Lock()
	jsa.utimer = time.AfterFunc(usageTick, jsa.sendClusterUsageUpdateTimer)
	// Cluster mode updates to resource usage, but we always will turn on. System internal prevents echos.
	jsa.updatesPub = fmt.Sprintf(jsaUpdatesPubT, a.Name, sysNode)
	jsa.updatesSub, _ = s.sysSubscribe(fmt.Sprintf(jsaUpdatesSubT, a.Name), jsa.remoteUpdateUsage)
	jsa.usageMu.Unlock()

	js.accounts[a.Name] = jsa
	js.mu.Unlock()

	// Stamp inside account as well.
	a.mu.Lock()
	a.js = jsa
	a.mu.Unlock()

	// Create the proper imports here.
	if err := a.enableAllJetStreamServiceImportsAndMappings(); err != nil {
		return err
	}

	s.Debugf("Enabled JetStream for account %q", a.Name)
	if l, ok := limits[_EMPTY_]; ok {
		s.Debugf("  Max Memory:      %s", friendlyBytes(l.MaxMemory))
		s.Debugf("  Max Storage:     %s", friendlyBytes(l.MaxStore))
	} else {
		for t, l := range limits {
			s.Debugf("  Tier: %s", t)
			s.Debugf("    Max Memory:      %s", friendlyBytes(l.MaxMemory))
			s.Debugf("    Max Storage:     %s", friendlyBytes(l.MaxStore))
		}
	}

	// Clean up any old snapshots that were orphaned while staging.
	os.RemoveAll(filepath.Join(js.config.StoreDir, snapStagingDir))

	sdir := filepath.Join(jsa.storeDir, streamsDir)
	if _, err := os.Stat(sdir); os.IsNotExist(err) {
		if err := os.MkdirAll(sdir, defaultDirPerms); err != nil {
			return fmt.Errorf("could not create storage streams directory - %v", err)
		}
		// Just need to make sure we can write to the directory.
		// Remove the directory will create later if needed.
		os.RemoveAll(sdir)
		// when empty remove parent directory, which may have been created as well
		os.Remove(jsa.storeDir)
	} else {
		// Restore any state here.
		s.Debugf("Recovering JetStream state for account %q", a.Name)
	}

	// Check templates first since messsage sets will need proper ownership.
	// FIXME(dlc) - Make this consistent.
	tdir := filepath.Join(jsa.storeDir, tmplsDir)
	if stat, err := os.Stat(tdir); err == nil && stat.IsDir() {
		key := sha256.Sum256([]byte("templates"))
		hh, err := highwayhash.New64(key[:])
		if err != nil {
			return err
		}
		fis, _ := os.ReadDir(tdir)
		for _, fi := range fis {
			metafile := filepath.Join(tdir, fi.Name(), JetStreamMetaFile)
			metasum := filepath.Join(tdir, fi.Name(), JetStreamMetaFileSum)
			buf, err := os.ReadFile(metafile)
			if err != nil {
				s.Warnf("  Error reading StreamTemplate metafile %q: %v", metasum, err)
				continue
			}
			if _, err := os.Stat(metasum); os.IsNotExist(err) {
				s.Warnf("  Missing StreamTemplate checksum for %q", metasum)
				continue
			}
			sum, err := os.ReadFile(metasum)
			if err != nil {
				s.Warnf("  Error reading StreamTemplate checksum %q: %v", metasum, err)
				continue
			}
			hh.Reset()
			hh.Write(buf)
			checksum := hex.EncodeToString(hh.Sum(nil))
			if checksum != string(sum) {
				s.Warnf("  StreamTemplate checksums do not match %q vs %q", sum, checksum)
				continue
			}
			var cfg StreamTemplateConfig
			if err := json.Unmarshal(buf, &cfg); err != nil {
				s.Warnf("  Error unmarshalling StreamTemplate metafile: %v", err)
				continue
			}
			cfg.Config.Name = _EMPTY_
			if _, err := a.addStreamTemplate(&cfg); err != nil {
				s.Warnf("  Error recreating StreamTemplate %q: %v", cfg.Name, err)
				continue
			}
		}
	}

	// Collect consumers, do after all streams.
	type ce struct {
		mset *stream
		odir string
	}
	var consumers []*ce

	// Collect any interest policy streams to check for
	// https://github.com/nats-io/nats-server/issues/3612
	var ipstreams []*stream

	// Remember if we should be encrypted and what cipher we think we should use.
	encrypted := s.getOpts().JetStreamKey != _EMPTY_
	plaintext := true
	sc := s.getOpts().JetStreamCipher

	// Now recover the streams.
	fis, _ := os.ReadDir(sdir)
	for _, fi := range fis {
		mdir := filepath.Join(sdir, fi.Name())
		key := sha256.Sum256([]byte(fi.Name()))
		hh, err := highwayhash.New64(key[:])
		if err != nil {
			return err
		}
		metafile := filepath.Join(mdir, JetStreamMetaFile)
		metasum := filepath.Join(mdir, JetStreamMetaFileSum)
		if _, err := os.Stat(metafile); os.IsNotExist(err) {
			s.Warnf("  Missing stream metafile for %q", metafile)
			continue
		}
		buf, err := os.ReadFile(metafile)
		if err != nil {
			s.Warnf("  Error reading metafile %q: %v", metafile, err)
			continue
		}
		if _, err := os.Stat(metasum); os.IsNotExist(err) {
			s.Warnf("  Missing stream checksum file %q", metasum)
			continue
		}
		sum, err := os.ReadFile(metasum)
		if err != nil {
			s.Warnf("  Error reading Stream metafile checksum %q: %v", metasum, err)
			continue
		}
		hh.Write(buf)
		checksum := hex.EncodeToString(hh.Sum(nil))
		if checksum != string(sum) {
			s.Warnf("  Stream metafile %q: checksums do not match %q vs %q", metafile, sum, checksum)
			continue
		}

		// Track if we are converting ciphers.
		var osc StoreCipher
		var convertingCiphers bool

		// Check if we are encrypted.
		keyFile := filepath.Join(mdir, JetStreamMetaFileKey)
		if key, err := os.ReadFile(keyFile); err == nil {
			s.Debugf("  Stream metafile is encrypted, reading encrypted keyfile")
			if len(key) < minMetaKeySize {
				s.Warnf("  Bad stream encryption key length of %d", len(key))
				continue
			}
			// Decode the buffer before proceeding.
			nbuf, err := s.decryptMeta(sc, key, buf, a.Name, fi.Name())
			if err != nil {
				// See if we are changing ciphers.
				switch sc {
				case ChaCha:
					nbuf, err = s.decryptMeta(AES, key, buf, a.Name, fi.Name())
					osc, convertingCiphers = AES, true
				case AES:
					nbuf, err = s.decryptMeta(ChaCha, key, buf, a.Name, fi.Name())
					osc, convertingCiphers = ChaCha, true
				}
				if err != nil {
					s.Warnf("  Error decrypting our stream metafile: %v", err)
					continue
				}
			}
			buf = nbuf
			plaintext = false

			// Remove the key file to have system regenerate with the new cipher.
			os.Remove(keyFile)
		}

		var cfg FileStreamInfo
		if err := json.Unmarshal(buf, &cfg); err != nil {
			s.Warnf("  Error unmarshalling stream metafile %q: %v", metafile, err)
			continue
		}

		if cfg.Template != _EMPTY_ {
			if err := jsa.addStreamNameToTemplate(cfg.Template, cfg.Name); err != nil {
				s.Warnf("  Error adding stream %q to template %q: %v", cfg.Name, cfg.Template, err)
			}
		}

		// We had a bug that set a default de dupe window on mirror, despite that being not a valid config
		fixCfgMirrorWithDedupWindow(&cfg.StreamConfig)

		// We had a bug that could allow subjects in that had prefix or suffix spaces. We check for that here
		// and will patch them on the fly for now. We will warn about them.
		var hadSubjErr bool
		for i, subj := range cfg.StreamConfig.Subjects {
			if !IsValidSubject(subj) {
				s.Warnf("  Detected bad subject %q while adding stream %q, will attempt to repair", subj, cfg.Name)
				if nsubj := strings.TrimSpace(subj); IsValidSubject(nsubj) {
					s.Warnf("  Bad subject %q repaired to %q", subj, nsubj)
					cfg.StreamConfig.Subjects[i] = nsubj
				} else {
					s.Warnf("  Error recreating stream %q: %v", cfg.Name, "invalid subject")
					hadSubjErr = true
					break
				}
			}
		}
		if hadSubjErr {
			continue
		}

		// The other possible bug is assigning subjects to mirrors, so check for that and patch as well.
		if cfg.StreamConfig.Mirror != nil && len(cfg.StreamConfig.Subjects) > 0 {
			s.Warnf("  Detected subjects on a mirrored stream %q, will remove", cfg.Name)
			cfg.StreamConfig.Subjects = nil
		}

		s.Noticef("  Starting restore for stream '%s > %s'", a.Name, cfg.StreamConfig.Name)

		// Log if we are converting from plaintext to encrypted.
		if encrypted {
			if plaintext {
				s.Noticef("  Encrypting stream '%s > %s'", a.Name, cfg.StreamConfig.Name)
			} else if convertingCiphers {
				s.Noticef("  Converting from %s to %s for stream '%s > %s'", osc, sc, a.Name, cfg.StreamConfig.Name)
			}
		}

		// Add in the stream.
		mset, err := a.addStream(&cfg.StreamConfig)
		if err != nil {
			s.Warnf("  Error recreating stream %q: %v", cfg.Name, err)
			continue
		}
		if !cfg.Created.IsZero() {
			mset.setCreatedTime(cfg.Created)
		}

		state := mset.state()
		s.Noticef("  Restored %s messages for stream '%s > %s'", comma(int64(state.Msgs)), mset.accName(), mset.name())

		// Collect to check for dangling messages.
		// TODO(dlc) - Can be removed eventually.
		if cfg.StreamConfig.Retention == InterestPolicy {
			ipstreams = append(ipstreams, mset)
		}

		// Now do the consumers.
		odir := filepath.Join(sdir, fi.Name(), consumerDir)
		consumers = append(consumers, &ce{mset, odir})
	}

	for _, e := range consumers {
		ofis, _ := os.ReadDir(e.odir)
		if len(ofis) > 0 {
			s.Noticef("  Recovering %d consumers for stream - '%s > %s'", len(ofis), e.mset.accName(), e.mset.name())
		}
		for _, ofi := range ofis {
			metafile := filepath.Join(e.odir, ofi.Name(), JetStreamMetaFile)
			metasum := filepath.Join(e.odir, ofi.Name(), JetStreamMetaFileSum)
			if _, err := os.Stat(metafile); os.IsNotExist(err) {
				s.Warnf("    Missing consumer metafile %q", metafile)
				continue
			}
			buf, err := os.ReadFile(metafile)
			if err != nil {
				s.Warnf("    Error reading consumer metafile %q: %v", metafile, err)
				continue
			}
			if _, err := os.Stat(metasum); os.IsNotExist(err) {
				s.Warnf("    Missing consumer checksum for %q", metasum)
				continue
			}

			// Check if we are encrypted.
			if key, err := os.ReadFile(filepath.Join(e.odir, ofi.Name(), JetStreamMetaFileKey)); err == nil {
				s.Debugf("  Consumer metafile is encrypted, reading encrypted keyfile")
				// Decode the buffer before proceeding.
				ctxName := e.mset.name() + tsep + ofi.Name()
				nbuf, err := s.decryptMeta(sc, key, buf, a.Name, ctxName)
				if err != nil {
					// See if we are changing ciphers.
					switch sc {
					case ChaCha:
						nbuf, err = s.decryptMeta(AES, key, buf, a.Name, ctxName)
					case AES:
						nbuf, err = s.decryptMeta(ChaCha, key, buf, a.Name, ctxName)
					}
					if err != nil {
						s.Warnf("  Error decrypting our consumer metafile: %v", err)
						continue
					}
				}
				buf = nbuf
			}

			var cfg FileConsumerInfo
			if err := json.Unmarshal(buf, &cfg); err != nil {
				s.Warnf("    Error unmarshalling consumer metafile %q: %v", metafile, err)
				continue
			}
			isEphemeral := !isDurableConsumer(&cfg.ConsumerConfig)
			if isEphemeral {
				// This is an ephermal consumer and this could fail on restart until
				// the consumer can reconnect. We will create it as a durable and switch it.
				cfg.ConsumerConfig.Durable = ofi.Name()
			}
			obs, err := e.mset.addConsumerWithAssignment(&cfg.ConsumerConfig, _EMPTY_, nil, true)
			if err != nil {
				s.Warnf("    Error adding consumer %q: %v", cfg.Name, err)
				continue
			}
			if isEphemeral {
				obs.switchToEphemeral()
			}
			if !cfg.Created.IsZero() {
				obs.setCreatedTime(cfg.Created)
			}
			lseq := e.mset.lastSeq()
			obs.mu.Lock()
			err = obs.readStoredState(lseq)
			obs.mu.Unlock()
			if err != nil {
				s.Warnf("    Error restoring consumer %q state: %v", cfg.Name, err)
			}
		}
	}

	// Make sure to cleanup any old remaining snapshots.
	os.RemoveAll(filepath.Join(jsa.storeDir, snapsDir))

	// Check interest policy streams for auto cleanup.
	for _, mset := range ipstreams {
		mset.checkForOrphanMsgs()
		mset.checkConsumerReplication()
	}

	s.Debugf("JetStream state for account %q recovered", a.Name)

	return nil
}

// Return whether we require MaxBytes to be set and if > 0 an upper limit for stream size exists
// Both limits are independent of each other.
func (a *Account) maxBytesLimits(cfg *StreamConfig) (bool, int64) {
	a.mu.RLock()
	jsa := a.js
	a.mu.RUnlock()
	if jsa == nil {
		return false, 0
	}
	jsa.usageMu.RLock()
	selectedLimits, _, ok := jsa.selectLimits(cfg)
	jsa.usageMu.RUnlock()
	if !ok {
		return false, 0
	}
	maxStreamBytes := int64(0)
	if cfg.Storage == MemoryStorage {
		maxStreamBytes = selectedLimits.MemoryMaxStreamBytes
	} else {
		maxStreamBytes = selectedLimits.StoreMaxStreamBytes
	}
	return selectedLimits.MaxBytesRequired, maxStreamBytes
}

// NumStreams will return how many streams we have.
func (a *Account) numStreams() int {
	a.mu.RLock()
	jsa := a.js
	a.mu.RUnlock()
	if jsa == nil {
		return 0
	}
	jsa.mu.Lock()
	n := len(jsa.streams)
	jsa.mu.Unlock()
	return n
}

// Streams will return all known streams.
func (a *Account) streams() []*stream {
	return a.filteredStreams(_EMPTY_)
}

func (a *Account) filteredStreams(filter string) []*stream {
	a.mu.RLock()
	jsa := a.js
	a.mu.RUnlock()

	if jsa == nil {
		return nil
	}

	jsa.mu.Lock()
	defer jsa.mu.Unlock()

	var msets []*stream
	for _, mset := range jsa.streams {
		if filter != _EMPTY_ {
			for _, subj := range mset.cfg.Subjects {
				if SubjectsCollide(filter, subj) {
					msets = append(msets, mset)
					break
				}
			}
		} else {
			msets = append(msets, mset)
		}
	}

	return msets
}

// lookupStream will lookup a stream by name.
func (a *Account) lookupStream(name string) (*stream, error) {
	a.mu.RLock()
	jsa := a.js
	a.mu.RUnlock()

	if jsa == nil {
		return nil, NewJSNotEnabledForAccountError()
	}
	jsa.mu.Lock()
	defer jsa.mu.Unlock()

	mset, ok := jsa.streams[name]
	if !ok {
		return nil, NewJSStreamNotFoundError()
	}
	return mset, nil
}

// UpdateJetStreamLimits will update the account limits for a JetStream enabled account.
func (a *Account) UpdateJetStreamLimits(limits map[string]JetStreamAccountLimits) error {
	a.mu.RLock()
	s, jsa := a.srv, a.js
	a.mu.RUnlock()

	if s == nil {
		return fmt.Errorf("jetstream account not registered")
	}
	js := s.getJetStream()
	if js == nil {
		return NewJSNotEnabledError()
	}
	if jsa == nil {
		return NewJSNotEnabledForAccountError()
	}

	if len(limits) == 0 {
		limits = defaultJSAccountTiers
	}

	// Calculate the delta between what we have and what we want.
	jsa.usageMu.RLock()
	dl := diffCheckedLimits(jsa.limits, limits)
	jsa.usageMu.RUnlock()

	js.mu.Lock()
	// Check the limits against existing reservations.
	if err := js.sufficientResources(dl); err != nil {
		js.mu.Unlock()
		return err
	}
	js.mu.Unlock()

	// Update
	jsa.usageMu.Lock()
	jsa.limits = limits
	jsa.usageMu.Unlock()

	return nil
}

func diffCheckedLimits(a, b map[string]JetStreamAccountLimits) map[string]JetStreamAccountLimits {
	diff := map[string]JetStreamAccountLimits{}
	for t, la := range a {
		// in a, not in b will return 0
		lb := b[t]
		diff[t] = JetStreamAccountLimits{
			MaxMemory: lb.MaxMemory - la.MaxMemory,
			MaxStore:  lb.MaxStore - la.MaxStore,
		}
	}
	for t, lb := range b {
		if la, ok := a[t]; !ok {
			// only in b not in a. (in a and b already covered)
			diff[t] = JetStreamAccountLimits{
				MaxMemory: lb.MaxMemory - la.MaxMemory,
				MaxStore:  lb.MaxStore - la.MaxStore,
			}
		}
	}
	return diff
}

// JetStreamUsage reports on JetStream usage and limits for an account.
func (a *Account) JetStreamUsage() JetStreamAccountStats {
	a.mu.RLock()
	jsa, aname := a.js, a.Name
	accJsLimits := a.jsLimits
	a.mu.RUnlock()

	var stats JetStreamAccountStats
	if jsa != nil {
		js := jsa.js
		js.mu.RLock()
		jsa.mu.RLock()
		jsa.usageMu.RLock()
		stats.Memory, stats.Store = jsa.storageTotals()
		stats.Domain = js.config.Domain
		stats.API = JetStreamAPIStats{
			Total:  jsa.apiTotal,
			Errors: jsa.apiErrors,
		}
		l, defaultTier := jsa.limits[_EMPTY_]
		if defaultTier {
			stats.Limits = l
		} else {
			skipped := 0
			stats.Tiers = make(map[string]JetStreamTier)
			for t, total := range jsa.usage {
				if _, ok := jsa.limits[t]; !ok && (*total) == (jsaStorage{}) {
					// skip tiers not present that don't contain a count
					// In case this shows an empty stream, that tier will be added when iterating over streams
					skipped++
				} else {
					stats.Tiers[t] = JetStreamTier{
						Memory: uint64(total.total.mem),
						Store:  uint64(total.total.store),
						Limits: jsa.limits[t],
					}
				}
			}
			if len(accJsLimits) != len(jsa.usage)-skipped {
				// insert unused limits
				for t, lim := range accJsLimits {
					if _, ok := stats.Tiers[t]; !ok {
						stats.Tiers[t] = JetStreamTier{Limits: lim}
					}
				}
			}
		}
		jsa.usageMu.RUnlock()
		if cc := jsa.js.cluster; cc != nil {
			sas := cc.streams[aname]
			if defaultTier {
				stats.Streams = len(sas)
			}
			for _, sa := range sas {
				stats.Consumers += len(sa.consumers)
				if !defaultTier {
					tier := tierName(sa.Config)
					u, ok := stats.Tiers[tier]
					if !ok {
						u = JetStreamTier{}
					}
					u.Streams++
					stats.Streams++
					u.Consumers += len(sa.consumers)
					stats.Tiers[tier] = u
				}
			}
		} else {
			if defaultTier {
				stats.Streams = len(jsa.streams)
			}
			for _, mset := range jsa.streams {
				consCount := mset.numConsumers()
				stats.Consumers += consCount
				if !defaultTier {
					u, ok := stats.Tiers[mset.tier]
					if !ok {
						u = JetStreamTier{}
					}
					u.Streams++
					stats.Streams++
					u.Consumers += consCount
					stats.Tiers[mset.tier] = u
				}
			}
		}
		jsa.mu.RUnlock()
		js.mu.RUnlock()
	}
	return stats
}

// DisableJetStream will disable JetStream for this account.
func (a *Account) DisableJetStream() error {
	return a.removeJetStream()
}

// removeJetStream is called when JetStream has been disabled for this account.
func (a *Account) removeJetStream() error {
	a.mu.Lock()
	s := a.srv
	a.js = nil
	a.mu.Unlock()

	if s == nil {
		return fmt.Errorf("jetstream account not registered")
	}

	js := s.getJetStream()
	if js == nil {
		return NewJSNotEnabledForAccountError()
	}

	return js.disableJetStream(js.lookupAccount(a))
}

// Disable JetStream for the account.
func (js *jetStream) disableJetStream(jsa *jsAccount) error {
	if jsa == nil || jsa.account == nil {
		return NewJSNotEnabledForAccountError()
	}

	js.mu.Lock()
	delete(js.accounts, jsa.account.Name)
	js.mu.Unlock()

	jsa.delete()

	return nil
}

// jetStreamConfigured reports whether the account has JetStream configured, regardless of this
// servers JetStream status.
func (a *Account) jetStreamConfigured() bool {
	if a == nil {
		return false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.jsLimits) > 0
}

// JetStreamEnabled is a helper to determine if jetstream is enabled for an account.
func (a *Account) JetStreamEnabled() bool {
	if a == nil {
		return false
	}
	a.mu.RLock()
	enabled := a.js != nil
	a.mu.RUnlock()
	return enabled
}

func (jsa *jsAccount) remoteUpdateUsage(sub *subscription, c *client, _ *Account, subject, _ string, msg []byte) {
	const usageSize = 32

	// jsa.js.srv is immutable and guaranteed to no be nil, so no lock needed.
	s := jsa.js.srv

	jsa.usageMu.Lock()
	if len(msg) < usageSize {
		jsa.usageMu.Unlock()
		s.Warnf("Ignoring remote usage update with size too short")
		return
	}
	var rnode string
	if li := strings.LastIndexByte(subject, btsep); li > 0 && li < len(subject) {
		rnode = subject[li+1:]
	}
	if rnode == _EMPTY_ {
		jsa.usageMu.Unlock()
		s.Warnf("Received remote usage update with no remote node")
		return
	}
	rUsage, ok := jsa.rusage[rnode]
	if !ok {
		if jsa.rusage == nil {
			jsa.rusage = make(map[string]*remoteUsage)
		}
		rUsage = &remoteUsage{tiers: make(map[string]*jsaUsage)}
		jsa.rusage[rnode] = rUsage
	}
	updateTotal := func(tierName string, memUsed, storeUsed int64) {
		total, ok := jsa.usage[tierName]
		if !ok {
			total = &jsaStorage{}
			jsa.usage[tierName] = total
		}
		// Update the usage for this remote.
		if usage := rUsage.tiers[tierName]; usage != nil {
			// Decrement our old values.
			total.total.mem -= usage.mem
			total.total.store -= usage.store
			usage.mem, usage.store = memUsed, storeUsed
		} else {
			rUsage.tiers[tierName] = &jsaUsage{memUsed, storeUsed}
		}
		total.total.mem += memUsed
		total.total.store += storeUsed
	}

	var le = binary.LittleEndian
	apiTotal, apiErrors := le.Uint64(msg[16:]), le.Uint64(msg[24:])
	memUsed, storeUsed := int64(le.Uint64(msg[0:])), int64(le.Uint64(msg[8:]))

	// we later extended the data structure to support multiple tiers
	excessRecordCnt := uint32(0)
	tierName := _EMPTY_
	if len(msg) >= 44 {
		excessRecordCnt = le.Uint32(msg[32:])
		length := le.Uint64(msg[36:])
		tierName = string(msg[44 : 44+length])
		msg = msg[44+length:]
	}
	updateTotal(tierName, memUsed, storeUsed)
	for ; excessRecordCnt > 0 && len(msg) >= 24; excessRecordCnt-- {
		memUsed, storeUsed := int64(le.Uint64(msg[0:])), int64(le.Uint64(msg[8:]))
		length := le.Uint64(msg[16:])
		tierName = string(msg[24 : 24+length])
		msg = msg[24+length:]
		updateTotal(tierName, memUsed, storeUsed)
	}
	jsa.apiTotal -= rUsage.api
	jsa.apiErrors -= rUsage.err
	rUsage.api = apiTotal
	rUsage.err = apiErrors
	jsa.apiTotal += apiTotal
	jsa.apiErrors += apiErrors
	jsa.usageMu.Unlock()
}

// Updates accounting on in use memory and storage. This is called from locally
// by the lower storage layers.
func (jsa *jsAccount) updateUsage(tierName string, storeType StorageType, delta int64) {
	// jsa.js is immutable and cannot be nil, so ok w/o lock.
	js := jsa.js
	// updateUsage() may be invoked under the mset's lock, so we can't get
	// the js' lock to check if clustered. So use this function that make
	// use of an atomic to do the check without having data race reports.
	isClustered := js.isClusteredNoLock()

	jsa.usageMu.Lock()
	defer jsa.usageMu.Unlock()

	s, ok := jsa.usage[tierName]
	if !ok {
		s = &jsaStorage{}
		jsa.usage[tierName] = s
	}
	if storeType == MemoryStorage {
		s.local.mem += delta
		s.total.mem += delta
		atomic.AddInt64(&js.memUsed, delta)
	} else {
		s.local.store += delta
		s.total.store += delta
		atomic.AddInt64(&js.storeUsed, delta)
	}
	// Publish our local updates if in clustered mode.
	if isClustered {
		jsa.sendClusterUsageUpdate()
	}
}

var usageTick = 1500 * time.Millisecond

func (jsa *jsAccount) sendClusterUsageUpdateTimer() {
	jsa.usageMu.Lock()
	defer jsa.usageMu.Unlock()
	jsa.sendClusterUsageUpdate()
	if jsa.utimer != nil {
		jsa.utimer.Reset(usageTick)
	}
}

// Send updates to our account usage for this server.
// jsa.usageMu lock should be held.
func (jsa *jsAccount) sendClusterUsageUpdate() {
	// These values are absolute so we can limit send rates.
	now := time.Now()
	if now.Sub(jsa.lupdate) < 250*time.Millisecond {
		return
	}
	jsa.lupdate = now

	lenUsage := len(jsa.usage)
	if lenUsage == 0 {
		return
	}
	// every base record contains mem/store/len(tier) as well as the tier name
	l := 24 * lenUsage
	for tier := range jsa.usage {
		l += len(tier)
	}
	if lenUsage > 0 {
		// first record contains api/usage errors as well as count for extra base records
		l += 20
	}
	var le = binary.LittleEndian
	b := make([]byte, l)
	i := 0

	for tier, usage := range jsa.usage {
		le.PutUint64(b[i+0:], uint64(usage.local.mem))
		le.PutUint64(b[i+8:], uint64(usage.local.store))
		if i == 0 {
			le.PutUint64(b[i+16:], jsa.usageApi)
			le.PutUint64(b[i+24:], jsa.usageErr)
			le.PutUint32(b[i+32:], uint32(len(jsa.usage)-1))
			le.PutUint64(b[i+36:], uint64(len(tier)))
			copy(b[i+44:], tier)
			i += 44 + len(tier)
		} else {
			le.PutUint64(b[i+16:], uint64(len(tier)))
			copy(b[i+24:], tier)
			i += 24 + len(tier)
		}
	}
	jsa.sendq.push(newPubMsg(nil, jsa.updatesPub, _EMPTY_, nil, nil, b, noCompression, false, false))
}

func (js *jetStream) wouldExceedLimits(storeType StorageType, sz int) bool {
	var (
		total *int64
		max   int64
	)
	if storeType == MemoryStorage {
		total, max = &js.memUsed, js.config.MaxMemory
	} else {
		total, max = &js.storeUsed, js.config.MaxStore
	}
	return atomic.LoadInt64(total) > (max + int64(sz))
}

func (js *jetStream) limitsExceeded(storeType StorageType) bool {
	return js.wouldExceedLimits(storeType, 0)
}

func tierName(cfg *StreamConfig) string {
	// TODO (mh) this is where we could select based off a placement tag as well "qos:tier"
	return fmt.Sprintf("R%d", cfg.Replicas)
}

func isSameTier(cfgA, cfgB *StreamConfig) bool {
	// TODO (mh) this is where we could select based off a placement tag as well "qos:tier"
	return cfgA.Replicas == cfgB.Replicas
}

func (jsa *jsAccount) jetStreamAndClustered() (*jetStream, bool) {
	jsa.mu.RLock()
	js := jsa.js
	jsa.mu.RUnlock()
	return js, js.isClustered()
}

// jsa.usageMu read lock should be held.
func (jsa *jsAccount) selectLimits(cfg *StreamConfig) (JetStreamAccountLimits, string, bool) {
	if selectedLimits, ok := jsa.limits[_EMPTY_]; ok {
		return selectedLimits, _EMPTY_, true
	}
	tier := tierName(cfg)
	if selectedLimits, ok := jsa.limits[tier]; ok {
		return selectedLimits, tier, true
	}
	return JetStreamAccountLimits{}, _EMPTY_, false
}

// Lock should be held.
func (jsa *jsAccount) countStreams(tier string, cfg *StreamConfig) int {
	streams := len(jsa.streams)
	if tier != _EMPTY_ {
		streams = 0
		for _, sa := range jsa.streams {
			if isSameTier(&sa.cfg, cfg) {
				streams++
			}
		}
	}
	return streams
}

// jsa.usageMu read lock (at least) should be held.
func (jsa *jsAccount) storageTotals() (uint64, uint64) {
	mem := uint64(0)
	store := uint64(0)
	for _, sa := range jsa.usage {
		mem += uint64(sa.total.mem)
		store += uint64(sa.total.store)
	}
	return mem, store
}

func (jsa *jsAccount) limitsExceeded(storeType StorageType, tierName string) (bool, *ApiError) {
	jsa.usageMu.RLock()
	defer jsa.usageMu.RUnlock()

	selectedLimits, ok := jsa.limits[tierName]
	if !ok {
		return true, NewJSNoLimitsError()
	}
	inUse := jsa.usage[tierName]
	if inUse == nil {
		// Imply totals of 0
		return false, nil
	}
	if storeType == MemoryStorage {
		totalMem := inUse.total.mem
		if selectedLimits.MemoryMaxStreamBytes > 0 && totalMem > selectedLimits.MemoryMaxStreamBytes {
			return true, nil
		}
		if selectedLimits.MaxMemory >= 0 && totalMem > selectedLimits.MaxMemory {
			return true, nil
		}
	} else {
		totalStore := inUse.total.store
		if selectedLimits.StoreMaxStreamBytes > 0 && totalStore > selectedLimits.StoreMaxStreamBytes {
			return true, nil
		}
		if selectedLimits.MaxStore >= 0 && totalStore > selectedLimits.MaxStore {
			return true, nil
		}
	}

	return false, nil
}

// Check account limits.
// Read Lock should be held
func (js *jetStream) checkAccountLimits(selected *JetStreamAccountLimits, config *StreamConfig, currentRes int64) error {
	return js.checkLimits(selected, config, false, currentRes, 0)
}

// Check account and server limits.
// Read Lock should be held
func (js *jetStream) checkAllLimits(selected *JetStreamAccountLimits, config *StreamConfig, currentRes, maxBytesOffset int64) error {
	return js.checkLimits(selected, config, true, currentRes, maxBytesOffset)
}

// Check if a new proposed msg set while exceed our account limits.
// Lock should be held.
func (js *jetStream) checkLimits(selected *JetStreamAccountLimits, config *StreamConfig, checkServer bool, currentRes, maxBytesOffset int64) error {
	// Check MaxConsumers
	if config.MaxConsumers > 0 && selected.MaxConsumers > 0 && config.MaxConsumers > selected.MaxConsumers {
		return NewJSMaximumConsumersLimitError()
	}
	// stream limit is checked separately on stream create only!
	// Check storage, memory or disk.
	return js.checkBytesLimits(selected, config.MaxBytes, config.Storage, config.Replicas, checkServer, currentRes, maxBytesOffset)
}

// Check if additional bytes will exceed our account limits and optionally the server itself.
// This should account for replicas.
// Read Lock should be held.
func (js *jetStream) checkBytesLimits(selectedLimits *JetStreamAccountLimits, addBytes int64, storage StorageType, replicas int, checkServer bool, currentRes, maxBytesOffset int64) error {
	if replicas < 1 {
		replicas = 1
	}
	if addBytes < 0 {
		addBytes = 1
	}
	totalBytes := (addBytes * int64(replicas)) + maxBytesOffset

	switch storage {
	case MemoryStorage:
		// Account limits defined.
		if selectedLimits.MaxMemory >= 0 {
			if currentRes+totalBytes > selectedLimits.MaxMemory {
				return NewJSMemoryResourcesExceededError()
			}
		}
		// Check if this server can handle request.
		if checkServer && js.memReserved+addBytes > js.config.MaxMemory {
			return NewJSMemoryResourcesExceededError()
		}
	case FileStorage:
		// Account limits defined.
		if selectedLimits.MaxStore >= 0 {
			if currentRes+totalBytes > selectedLimits.MaxStore {
				return NewJSStorageResourcesExceededError()
			}
		}
		// Check if this server can handle request.
		if checkServer && js.storeReserved+addBytes > js.config.MaxStore {
			return NewJSStorageResourcesExceededError()
		}
	}

	return nil
}

func (jsa *jsAccount) acc() *Account {
	return jsa.account
}

// Delete the JetStream resources.
func (jsa *jsAccount) delete() {
	var streams []*stream
	var ts []string

	jsa.mu.Lock()
	// The update timer and subs need to be protected by usageMu lock
	jsa.usageMu.Lock()
	if jsa.utimer != nil {
		jsa.utimer.Stop()
		jsa.utimer = nil
	}
	if jsa.updatesSub != nil && jsa.js.srv != nil {
		s := jsa.js.srv
		s.sysUnsubscribe(jsa.updatesSub)
		jsa.updatesSub = nil
	}
	jsa.usageMu.Unlock()

	for _, ms := range jsa.streams {
		streams = append(streams, ms)
	}
	acc := jsa.account
	for _, t := range jsa.templates {
		ts = append(ts, t.Name)
	}
	jsa.templates = nil
	jsa.mu.Unlock()

	for _, ms := range streams {
		ms.stop(false, false)
	}

	for _, t := range ts {
		acc.deleteStreamTemplate(t)
	}
}

// Lookup the jetstream account for a given account.
func (js *jetStream) lookupAccount(a *Account) *jsAccount {
	if a == nil {
		return nil
	}
	js.mu.RLock()
	jsa := js.accounts[a.Name]
	js.mu.RUnlock()
	return jsa
}

// Report on JetStream stats and usage for this server.
func (js *jetStream) usageStats() *JetStreamStats {
	var stats JetStreamStats
	js.mu.RLock()
	stats.Accounts = len(js.accounts)
	stats.ReservedMemory = (uint64)(js.memReserved)
	stats.ReservedStore = (uint64)(js.storeReserved)
	s := js.srv
	js.mu.RUnlock()
	stats.API.Total = (uint64)(atomic.LoadInt64(&js.apiTotal))
	stats.API.Errors = (uint64)(atomic.LoadInt64(&js.apiErrors))
	stats.API.Inflight = (uint64)(atomic.LoadInt64(&js.apiInflight))
	stats.Memory = (uint64)(atomic.LoadInt64(&js.memUsed))
	stats.Store = (uint64)(atomic.LoadInt64(&js.storeUsed))
	stats.HAAssets = s.numRaftNodes()
	return &stats
}

// Check to see if we have enough system resources for this account.
// Lock should be held.
func (js *jetStream) sufficientResources(limits map[string]JetStreamAccountLimits) error {
	// If we are clustered we do not really know how many resources will be ultimately available.
	// This needs to be handled out of band.
	// If we are a single server, we can make decisions here.
	if limits == nil || !js.standAlone {
		return nil
	}

	totalMaxBytes := func(limits map[string]JetStreamAccountLimits) (int64, int64) {
		totalMaxMemory := int64(0)
		totalMaxStore := int64(0)
		for _, l := range limits {
			if l.MaxMemory > 0 {
				totalMaxMemory += l.MaxMemory
			}
			if l.MaxStore > 0 {
				totalMaxStore += l.MaxStore
			}
		}
		return totalMaxMemory, totalMaxStore
	}

	totalMaxMemory, totalMaxStore := totalMaxBytes(limits)

	// Reserved is now specific to the MaxBytes for streams.
	if js.memReserved+totalMaxMemory > js.config.MaxMemory {
		return NewJSMemoryResourcesExceededError()
	}
	if js.storeReserved+totalMaxStore > js.config.MaxStore {
		return NewJSStorageResourcesExceededError()
	}

	// Since we know if we are here we are single server mode, check the account reservations.
	var storeReserved, memReserved int64
	for _, jsa := range js.accounts {
		if jsa.account.IsExpired() {
			continue
		}
		jsa.usageMu.RLock()
		maxMemory, maxStore := totalMaxBytes(jsa.limits)
		jsa.usageMu.RUnlock()
		memReserved += maxMemory
		storeReserved += maxStore
	}

	if memReserved+totalMaxMemory > js.config.MaxMemory {
		return NewJSMemoryResourcesExceededError()
	}
	if storeReserved+totalMaxStore > js.config.MaxStore {
		return NewJSStorageResourcesExceededError()
	}

	return nil
}

// This will reserve the stream resources requested.
// This will spin off off of MaxBytes.
func (js *jetStream) reserveStreamResources(cfg *StreamConfig) {
	if cfg == nil || cfg.MaxBytes <= 0 {
		return
	}

	js.mu.Lock()
	switch cfg.Storage {
	case MemoryStorage:
		js.memReserved += cfg.MaxBytes
	case FileStorage:
		js.storeReserved += cfg.MaxBytes
	}
	s, clustered := js.srv, !js.standAlone
	js.mu.Unlock()
	// If clustered send an update to the system immediately.
	if clustered {
		s.sendStatszUpdate()
	}
}

// Release reserved resources held by a stream.
func (js *jetStream) releaseStreamResources(cfg *StreamConfig) {
	if cfg == nil || cfg.MaxBytes <= 0 {
		return
	}

	js.mu.Lock()
	switch cfg.Storage {
	case MemoryStorage:
		js.memReserved -= cfg.MaxBytes
	case FileStorage:
		js.storeReserved -= cfg.MaxBytes
	}
	s, clustered := js.srv, !js.standAlone
	js.mu.Unlock()
	// If clustered send an update to the system immediately.
	if clustered {
		s.sendStatszUpdate()
	}
}

const (
	// JetStreamStoreDir is the prefix we use.
	JetStreamStoreDir = "jetstream"
	// JetStreamMaxStoreDefault is the default disk storage limit. 1TB
	JetStreamMaxStoreDefault = 1024 * 1024 * 1024 * 1024
	// JetStreamMaxMemDefault is only used when we can't determine system memory. 256MB
	JetStreamMaxMemDefault = 1024 * 1024 * 256
	// snapshot staging for restores.
	snapStagingDir = ".snap-staging"
)

// Dynamically create a config with a tmp based directory (repeatable) and 75% of system memory.
func (s *Server) dynJetStreamConfig(storeDir string, maxStore, maxMem int64) *JetStreamConfig {
	jsc := &JetStreamConfig{}
	if storeDir != _EMPTY_ {
		jsc.StoreDir = filepath.Join(storeDir, JetStreamStoreDir)
	} else {
		// Create one in tmp directory, but make it consistent for restarts.
		jsc.StoreDir = filepath.Join(os.TempDir(), "nats", JetStreamStoreDir)
	}

	opts := s.getOpts()

	if opts.maxStoreSet && maxStore >= 0 {
		jsc.MaxStore = maxStore
	} else {
		jsc.MaxStore = diskAvailable(jsc.StoreDir)
	}

	if opts.maxMemSet && maxMem >= 0 {
		jsc.MaxMemory = maxMem
	} else {
		// Estimate to 75% of total memory if we can determine system memory.
		if sysMem := sysmem.Memory(); sysMem > 0 {
			jsc.MaxMemory = sysMem / 4 * 3
		} else {
			jsc.MaxMemory = JetStreamMaxMemDefault
		}
	}
	return jsc
}

// Helper function.
func (a *Account) checkForJetStream() (*Server, *jsAccount, error) {
	a.mu.RLock()
	s := a.srv
	jsa := a.js
	a.mu.RUnlock()

	if s == nil || jsa == nil {
		return nil, nil, NewJSNotEnabledForAccountError()
	}

	return s, jsa, nil
}

// StreamTemplateConfig allows a configuration to auto-create streams based on this template when a message
// is received that matches. Each new stream will use the config as the template config to create them.
type StreamTemplateConfig struct {
	Name       string        `json:"name"`
	Config     *StreamConfig `json:"config"`
	MaxStreams uint32        `json:"max_streams"`
}

// StreamTemplateInfo
type StreamTemplateInfo struct {
	Config  *StreamTemplateConfig `json:"config"`
	Streams []string              `json:"streams"`
}

// streamTemplate
type streamTemplate struct {
	mu  sync.Mutex
	tc  *client
	jsa *jsAccount
	*StreamTemplateConfig
	streams []string
}

func (t *StreamTemplateConfig) deepCopy() *StreamTemplateConfig {
	copy := *t
	cfg := *t.Config
	copy.Config = &cfg
	return &copy
}

// addStreamTemplate will add a stream template to this account that allows auto-creation of streams.
func (a *Account) addStreamTemplate(tc *StreamTemplateConfig) (*streamTemplate, error) {
	s, jsa, err := a.checkForJetStream()
	if err != nil {
		return nil, err
	}
	if tc.Config.Name != "" {
		return nil, fmt.Errorf("template config name should be empty")
	}
	if len(tc.Name) > JSMaxNameLen {
		return nil, fmt.Errorf("template name is too long, maximum allowed is %d", JSMaxNameLen)
	}

	// FIXME(dlc) - Hacky
	tcopy := tc.deepCopy()
	tcopy.Config.Name = "_"
	cfg, apiErr := s.checkStreamCfg(tcopy.Config, a)
	if apiErr != nil {
		return nil, apiErr
	}
	tcopy.Config = &cfg
	t := &streamTemplate{
		StreamTemplateConfig: tcopy,
		tc:                   s.createInternalJetStreamClient(),
		jsa:                  jsa,
	}
	t.tc.registerWithAccount(a)

	jsa.mu.Lock()
	if jsa.templates == nil {
		jsa.templates = make(map[string]*streamTemplate)
		// Create the appropriate store
		if cfg.Storage == FileStorage {
			jsa.store = newTemplateFileStore(jsa.storeDir)
		} else {
			jsa.store = newTemplateMemStore()
		}
	} else if _, ok := jsa.templates[tcopy.Name]; ok {
		jsa.mu.Unlock()
		return nil, fmt.Errorf("template with name %q already exists", tcopy.Name)
	}
	jsa.templates[tcopy.Name] = t
	jsa.mu.Unlock()

	// FIXME(dlc) - we can not overlap subjects between templates. Need to have test.

	// Setup the internal subscriptions to trap the messages.
	if err := t.createTemplateSubscriptions(); err != nil {
		return nil, err
	}
	if err := jsa.store.Store(t); err != nil {
		t.delete()
		return nil, err
	}
	return t, nil
}

func (t *streamTemplate) createTemplateSubscriptions() error {
	if t == nil {
		return fmt.Errorf("no template")
	}
	if t.tc == nil {
		return fmt.Errorf("template not enabled")
	}
	c := t.tc
	if !c.srv.EventsEnabled() {
		return ErrNoSysAccount
	}
	sid := 1
	for _, subject := range t.Config.Subjects {
		// Now create the subscription
		if _, err := c.processSub([]byte(subject), nil, []byte(strconv.Itoa(sid)), t.processInboundTemplateMsg, false); err != nil {
			c.acc.deleteStreamTemplate(t.Name)
			return err
		}
		sid++
	}
	return nil
}

func (t *streamTemplate) processInboundTemplateMsg(_ *subscription, pc *client, acc *Account, subject, reply string, msg []byte) {
	if t == nil || t.jsa == nil {
		return
	}
	jsa := t.jsa
	cn := canonicalName(subject)

	jsa.mu.Lock()
	// If we already are registered then we can just return here.
	if _, ok := jsa.streams[cn]; ok {
		jsa.mu.Unlock()
		return
	}
	jsa.mu.Unlock()

	// Check if we are at the maximum and grab some variables.
	t.mu.Lock()
	c := t.tc
	cfg := *t.Config
	cfg.Template = t.Name
	atLimit := len(t.streams) >= int(t.MaxStreams)
	if !atLimit {
		t.streams = append(t.streams, cn)
	}
	t.mu.Unlock()

	if atLimit {
		c.RateLimitWarnf("JetStream could not create stream for account %q on subject %q, at limit", acc.Name, subject)
		return
	}

	// We need to create the stream here.
	// Change the config from the template and only use literal subject.
	cfg.Name = cn
	cfg.Subjects = []string{subject}
	mset, err := acc.addStream(&cfg)
	if err != nil {
		acc.validateStreams(t)
		c.RateLimitWarnf("JetStream could not create stream for account %q on subject %q: %v", acc.Name, subject, err)
		return
	}

	// Process this message directly by invoking mset.
	mset.processInboundJetStreamMsg(nil, pc, acc, subject, reply, msg)
}

// lookupStreamTemplate looks up the names stream template.
func (a *Account) lookupStreamTemplate(name string) (*streamTemplate, error) {
	_, jsa, err := a.checkForJetStream()
	if err != nil {
		return nil, err
	}
	jsa.mu.Lock()
	defer jsa.mu.Unlock()
	if jsa.templates == nil {
		return nil, fmt.Errorf("template not found")
	}
	t, ok := jsa.templates[name]
	if !ok {
		return nil, fmt.Errorf("template not found")
	}
	return t, nil
}

// This function will check all named streams and make sure they are valid.
func (a *Account) validateStreams(t *streamTemplate) {
	t.mu.Lock()
	var vstreams []string
	for _, sname := range t.streams {
		if _, err := a.lookupStream(sname); err == nil {
			vstreams = append(vstreams, sname)
		}
	}
	t.streams = vstreams
	t.mu.Unlock()
}

func (t *streamTemplate) delete() error {
	if t == nil {
		return fmt.Errorf("nil stream template")
	}

	t.mu.Lock()
	jsa := t.jsa
	c := t.tc
	t.tc = nil
	defer func() {
		if c != nil {
			c.closeConnection(ClientClosed)
		}
	}()
	t.mu.Unlock()

	if jsa == nil {
		return NewJSNotEnabledForAccountError()
	}

	jsa.mu.Lock()
	if jsa.templates == nil {
		jsa.mu.Unlock()
		return fmt.Errorf("template not found")
	}
	if _, ok := jsa.templates[t.Name]; !ok {
		jsa.mu.Unlock()
		return fmt.Errorf("template not found")
	}
	delete(jsa.templates, t.Name)
	acc := jsa.account
	jsa.mu.Unlock()

	// Remove streams associated with this template.
	var streams []*stream
	t.mu.Lock()
	for _, name := range t.streams {
		if mset, err := acc.lookupStream(name); err == nil {
			streams = append(streams, mset)
		}
	}
	t.mu.Unlock()

	if jsa.store != nil {
		if err := jsa.store.Delete(t); err != nil {
			return fmt.Errorf("error deleting template from store: %v", err)
		}
	}

	var lastErr error
	for _, mset := range streams {
		if err := mset.delete(); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

func (a *Account) deleteStreamTemplate(name string) error {
	t, err := a.lookupStreamTemplate(name)
	if err != nil {
		return NewJSStreamTemplateNotFoundError()
	}
	return t.delete()
}

func (a *Account) templates() []*streamTemplate {
	var ts []*streamTemplate
	_, jsa, err := a.checkForJetStream()
	if err != nil {
		return nil
	}

	jsa.mu.Lock()
	for _, t := range jsa.templates {
		// FIXME(dlc) - Copy?
		ts = append(ts, t)
	}
	jsa.mu.Unlock()

	return ts
}

// Will add a stream to a template, this is for recovery.
func (jsa *jsAccount) addStreamNameToTemplate(tname, mname string) error {
	if jsa.templates == nil {
		return fmt.Errorf("template not found")
	}
	t, ok := jsa.templates[tname]
	if !ok {
		return fmt.Errorf("template not found")
	}
	// We found template.
	t.mu.Lock()
	t.streams = append(t.streams, mname)
	t.mu.Unlock()
	return nil
}

// This will check if a template owns this stream.
// jsAccount lock should be held
func (jsa *jsAccount) checkTemplateOwnership(tname, sname string) bool {
	if jsa.templates == nil {
		return false
	}
	t, ok := jsa.templates[tname]
	if !ok {
		return false
	}
	// We found template, make sure we are in streams.
	for _, streamName := range t.streams {
		if sname == streamName {
			return true
		}
	}
	return false
}

// friendlyBytes returns a string with the given bytes int64
// represented as a size, such as 1KB, 10MB, etc...
func friendlyBytes(bytes int64) string {
	fbytes := float64(bytes)
	base := 1024
	pre := []string{"K", "M", "G", "T", "P", "E"}
	if fbytes < float64(base) {
		return fmt.Sprintf("%v B", fbytes)
	}
	exp := int(math.Log(fbytes) / math.Log(float64(base)))
	index := exp - 1
	return fmt.Sprintf("%.2f %sB", fbytes/math.Pow(float64(base), float64(exp)), pre[index])
}

func isValidName(name string) bool {
	if name == "" {
		return false
	}
	return !strings.ContainsAny(name, " \t\r\n\f.*>")
}

// CanonicalName will replace all token separators '.' with '_'.
// This can be used when naming streams or consumers with multi-token subjects.
func canonicalName(name string) string {
	return strings.ReplaceAll(name, ".", "_")
}

// To throttle the out of resources errors.
func (s *Server) resourcesExeededError() {
	var didAlert bool

	s.rerrMu.Lock()
	if now := time.Now(); now.Sub(s.rerrLast) > 10*time.Second {
		s.Errorf("JetStream resource limits exceeded for server")
		s.rerrLast = now
		didAlert = true
	}
	s.rerrMu.Unlock()

	// If we are meta leader we should relinguish that here.
	if didAlert {
		if js := s.getJetStream(); js != nil {
			js.mu.RLock()
			if cc := js.cluster; cc != nil && cc.isLeader() {
				cc.meta.StepDown()
			}
			js.mu.RUnlock()
		}
	}
}

// For validating options.
func validateJetStreamOptions(o *Options) error {
	// in non operator mode, the account names need to be configured
	if len(o.JsAccDefaultDomain) > 0 {
		if len(o.TrustedOperators) == 0 {
			for a, domain := range o.JsAccDefaultDomain {
				found := false
				if isReservedAccount(a) {
					found = true
				} else {
					for _, acc := range o.Accounts {
						if a == acc.GetName() {
							if len(acc.jsLimits) > 0 && domain != _EMPTY_ {
								return fmt.Errorf("default_js_domain contains account name %q with enabled JetStream", a)
							}
							found = true
							break
						}
					}
				}
				if !found {
					return fmt.Errorf("in non operator mode, `default_js_domain` references non existing account %q", a)
				}
			}
		} else {
			for a := range o.JsAccDefaultDomain {
				if !nkeys.IsValidPublicAccountKey(a) {
					return fmt.Errorf("default_js_domain contains account name %q, which is not a valid public account nkey", a)
				}
			}
		}
		for a, d := range o.JsAccDefaultDomain {
			sacc := DEFAULT_SYSTEM_ACCOUNT
			if o.SystemAccount != _EMPTY_ {
				sacc = o.SystemAccount
			}
			if a == sacc {
				return fmt.Errorf("system account %q can not be in default_js_domain", a)
			}
			if d == _EMPTY_ {
				continue
			}
			if sub := fmt.Sprintf(jsDomainAPI, d); !IsValidSubject(sub) {
				return fmt.Errorf("default_js_domain contains account %q with invalid domain name %q", a, d)
			}
		}
	}
	if o.JetStreamDomain != _EMPTY_ {
		if subj := fmt.Sprintf(jsDomainAPI, o.JetStreamDomain); !IsValidSubject(subj) {
			return fmt.Errorf("invalid domain name: derived %q is not a valid subject", subj)
		}

		if !isValidName(o.JetStreamDomain) {
			return fmt.Errorf("invalid domain name: may not contain ., * or >")
		}
	}
	// If not clustered no checks needed past here.
	if !o.JetStream || o.Cluster.Port == 0 {
		return nil
	}
	if o.ServerName == _EMPTY_ {
		return fmt.Errorf("jetstream cluster requires `server_name` to be set")
	}
	if o.Cluster.Name == _EMPTY_ {
		return fmt.Errorf("jetstream cluster requires `cluster.name` to be set")
	}

	h := strings.ToLower(o.JetStreamExtHint)
	switch h {
	case jsWillExtend, jsNoExtend, _EMPTY_:
		o.JetStreamExtHint = h
	default:
		return fmt.Errorf("expected 'no_extend' for string value, got '%s'", h)
	}

	if o.JetStreamMaxCatchup < 0 {
		return fmt.Errorf("jetstream max catchup cannot be negative")
	}
	return nil
}

// We had a bug that set a default de dupe window on mirror, despite that being not a valid config
func fixCfgMirrorWithDedupWindow(cfg *StreamConfig) {
	if cfg == nil || cfg.Mirror == nil {
		return
	}
	if cfg.Duplicates != 0 {
		cfg.Duplicates = 0
	}
}
