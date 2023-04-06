// Copyright 2019-2023 The NATS Authors
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
	"archive/tar"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	mrand "math/rand"

	"github.com/klauspost/compress/s2"
	"github.com/minio/highwayhash"
	"golang.org/x/crypto/chacha20"
	"golang.org/x/crypto/chacha20poly1305"
)

type FileStoreConfig struct {
	// Where the parent directory for all storage will be located.
	StoreDir string
	// BlockSize is the file block size. This also represents the maximum overhead size.
	BlockSize uint64
	// CacheExpire is how long with no activity until we expire the cache.
	CacheExpire time.Duration
	// SyncInterval is how often we sync to disk in the background.
	SyncInterval time.Duration
	// AsyncFlush allows async flush to batch write operations.
	AsyncFlush bool
	// Cipher is the cipher to use when encrypting.
	Cipher StoreCipher
}

// FileStreamInfo allows us to remember created time.
type FileStreamInfo struct {
	Created time.Time
	StreamConfig
}

type StoreCipher int

const (
	ChaCha StoreCipher = iota
	AES
	NoCipher
)

func (cipher StoreCipher) String() string {
	switch cipher {
	case ChaCha:
		return "ChaCha20-Poly1305"
	case AES:
		return "AES-GCM"
	case NoCipher:
		return "None"
	default:
		return "Unknown StoreCipher"
	}
}

// File ConsumerInfo is used for creating consumer stores.
type FileConsumerInfo struct {
	Created time.Time
	Name    string
	ConsumerConfig
}

// Default file and directory permissions.
const (
	defaultDirPerms  = os.FileMode(0750)
	defaultFilePerms = os.FileMode(0640)
)

type psi struct {
	total uint64
	fblk  uint32
	lblk  uint32
}

type fileStore struct {
	srv         *Server
	mu          sync.RWMutex
	state       StreamState
	ld          *LostStreamData
	scb         StorageUpdateHandler
	ageChk      *time.Timer
	syncTmr     *time.Timer
	cfg         FileStreamInfo
	fcfg        FileStoreConfig
	prf         keyGen
	aek         cipher.AEAD
	lmb         *msgBlock
	blks        []*msgBlock
	bim         map[uint32]*msgBlock
	psim        map[string]*psi
	hh          hash.Hash64
	qch         chan struct{}
	cfs         []ConsumerStore
	sips        int
	closed      bool
	fip         bool
	receivedAny bool
}

// Represents a message store block and its data.
type msgBlock struct {
	// Here for 32bit systems and atomic.
	first   msgId
	last    msgId
	mu      sync.RWMutex
	fs      *fileStore
	aek     cipher.AEAD
	bek     cipher.Stream
	seed    []byte
	nonce   []byte
	mfn     string
	mfd     *os.File
	ifn     string
	ifd     *os.File
	liwsz   int64
	index   uint32
	bytes   uint64 // User visible bytes count.
	rbytes  uint64 // Total bytes (raw) including deleted. Used for rolling to new blk.
	msgs    uint64 // User visible message count.
	fss     map[string]*SimpleState
	sfn     string
	kfn     string
	lwits   int64
	lwts    int64
	llts    int64
	lrts    int64
	llseq   uint64
	hh      hash.Hash64
	cache   *cache
	cloads  uint64
	cexp    time.Duration
	ctmr    *time.Timer
	werr    error
	dmap    map[uint64]struct{}
	fch     chan struct{}
	qch     chan struct{}
	lchk    [8]byte
	loading bool
	flusher bool
	noTrack bool
	closed  bool

	// To avoid excessive writes when expiring cache.
	// These can be big.
	fssNeedsWrite bool

	// Used to mock write failures.
	mockWriteErr bool
}

// Write through caching layer that is also used on loading messages.
type cache struct {
	buf  []byte
	off  int
	wp   int
	idx  []uint32
	lrl  uint32
	fseq uint64
	nra  bool
}

type msgId struct {
	seq uint64
	ts  int64
}

const (
	// Magic is used to identify the file store files.
	magic = uint8(22)
	// Version
	version = uint8(1)
	// hdrLen
	hdrLen = 2
	// This is where we keep the streams.
	streamsDir = "streams"
	// This is where we keep the message store blocks.
	msgDir = "msgs"
	// This is where we temporarily move the messages dir.
	purgeDir = "__msgs__"
	// used to scan blk file names.
	blkScan = "%d.blk"
	// used for compacted blocks that are staged.
	newScan = "%d.new"
	// used to scan index file names.
	indexScan = "%d.idx"
	// used to load per subject meta information.
	fssScan = "%d.fss"
	// used to store our block encryption key.
	keyScan = "%d.key"
	// to look for orphans
	keyScanAll = "*.key"
	// This is where we keep state on consumers.
	consumerDir = "obs"
	// Index file for a consumer.
	consumerState = "o.dat"
	// This is where we keep state on templates.
	tmplsDir = "templates"
	// Maximum size of a write buffer we may consider for re-use.
	maxBufReuse = 2 * 1024 * 1024
	// default cache buffer expiration
	defaultCacheBufferExpiration = 5 * time.Second
	// default sync interval
	defaultSyncInterval = 60 * time.Second
	// default idle timeout to close FDs.
	closeFDsIdle = 30 * time.Second
	// coalesceMinimum
	coalesceMinimum = 16 * 1024
	// maxFlushWait is maximum we will wait to gather messages to flush.
	maxFlushWait = 8 * time.Millisecond

	// Metafiles for streams and consumers.
	JetStreamMetaFile    = "meta.inf"
	JetStreamMetaFileSum = "meta.sum"
	JetStreamMetaFileKey = "meta.key"

	// AEK key sizes
	minMetaKeySize = 64
	minBlkKeySize  = 64

	// Default stream block size.
	defaultLargeBlockSize = 8 * 1024 * 1024 // 8MB
	// Default for workqueue or interest based.
	defaultMediumBlockSize = 4 * 1024 * 1024 // 4MB
	// For smaller reuse buffers. Usually being generated during contention on the lead write buffer.
	// E.g. mirrors/sources etc.
	defaultSmallBlockSize = 1 * 1024 * 1024 // 1MB
	// Default for KV based
	defaultKVBlockSize = defaultMediumBlockSize
	// max block size for now.
	maxBlockSize = defaultLargeBlockSize
	// Compact minimum threshold.
	compactMinimum = 2 * 1024 * 1024 // 2MB
	// FileStoreMinBlkSize is minimum size we will do for a blk size.
	FileStoreMinBlkSize = 32 * 1000 // 32kib
	// FileStoreMaxBlkSize is maximum size we will do for a blk size.
	FileStoreMaxBlkSize = maxBlockSize
	// Check for bad record length value due to corrupt data.
	rlBadThresh = 32 * 1024 * 1024
	// Time threshold to write index info.
	wiThresh = int64(30 * time.Second)
	// Time threshold to write index info for non FIFO cases
	winfThresh = int64(2 * time.Second)
)

func newFileStore(fcfg FileStoreConfig, cfg StreamConfig) (*fileStore, error) {
	return newFileStoreWithCreated(fcfg, cfg, time.Now().UTC(), nil)
}

func newFileStoreWithCreated(fcfg FileStoreConfig, cfg StreamConfig, created time.Time, prf keyGen) (*fileStore, error) {
	if cfg.Name == _EMPTY_ {
		return nil, fmt.Errorf("name required")
	}
	if cfg.Storage != FileStorage {
		return nil, fmt.Errorf("fileStore requires file storage type in config")
	}
	// Default values.
	if fcfg.BlockSize == 0 {
		fcfg.BlockSize = dynBlkSize(cfg.Retention, cfg.MaxBytes)
	}
	if fcfg.BlockSize > maxBlockSize {
		return nil, fmt.Errorf("filestore max block size is %s", friendlyBytes(maxBlockSize))
	}
	if fcfg.CacheExpire == 0 {
		fcfg.CacheExpire = defaultCacheBufferExpiration
	}
	if fcfg.SyncInterval == 0 {
		fcfg.SyncInterval = defaultSyncInterval
	}

	// Check the directory
	if stat, err := os.Stat(fcfg.StoreDir); os.IsNotExist(err) {
		if err := os.MkdirAll(fcfg.StoreDir, defaultDirPerms); err != nil {
			return nil, fmt.Errorf("could not create storage directory - %v", err)
		}
	} else if stat == nil || !stat.IsDir() {
		return nil, fmt.Errorf("storage directory is not a directory")
	}
	tmpfile, err := os.CreateTemp(fcfg.StoreDir, "_test_")
	if err != nil {
		return nil, fmt.Errorf("storage directory is not writable")
	}

	tmpfile.Close()
	<-dios
	os.Remove(tmpfile.Name())
	dios <- struct{}{}

	fs := &fileStore{
		fcfg: fcfg,
		psim: make(map[string]*psi),
		bim:  make(map[uint32]*msgBlock),
		cfg:  FileStreamInfo{Created: created, StreamConfig: cfg},
		prf:  prf,
		qch:  make(chan struct{}),
	}

	// Set flush in place to AsyncFlush which by default is false.
	fs.fip = !fcfg.AsyncFlush

	// Check if this is a new setup.
	mdir := filepath.Join(fcfg.StoreDir, msgDir)
	odir := filepath.Join(fcfg.StoreDir, consumerDir)
	if err := os.MkdirAll(mdir, defaultDirPerms); err != nil {
		return nil, fmt.Errorf("could not create message storage directory - %v", err)
	}
	if err := os.MkdirAll(odir, defaultDirPerms); err != nil {
		return nil, fmt.Errorf("could not create consumer storage directory - %v", err)
	}

	// Create highway hash for message blocks. Use sha256 of directory as key.
	key := sha256.Sum256([]byte(cfg.Name))
	fs.hh, err = highwayhash.New64(key[:])
	if err != nil {
		return nil, fmt.Errorf("could not create hash: %v", err)
	}

	// Recover our message state.
	if err := fs.recoverMsgs(); err != nil {
		return nil, err
	}

	// Write our meta data if it does not exist or is zero'd out.
	meta := filepath.Join(fcfg.StoreDir, JetStreamMetaFile)
	fi, err := os.Stat(meta)
	if err != nil && os.IsNotExist(err) || fi != nil && fi.Size() == 0 {
		if err := fs.writeStreamMeta(); err != nil {
			return nil, err
		}
	}

	// If we expect to be encrypted check that what we are restoring is not plaintext.
	// This can happen on snapshot restores or conversions.
	if fs.prf != nil {
		keyFile := filepath.Join(fs.fcfg.StoreDir, JetStreamMetaFileKey)
		if _, err := os.Stat(keyFile); err != nil && os.IsNotExist(err) {
			if err := fs.writeStreamMeta(); err != nil {
				return nil, err
			}
		}
	}

	fs.syncTmr = time.AfterFunc(fs.fcfg.SyncInterval, fs.syncBlocks)

	return fs, nil
}

func (fs *fileStore) registerServer(s *Server) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.srv = s
}

// Lock all existing message blocks.
// Lock held on entry.
func (fs *fileStore) lockAllMsgBlocks() {
	for _, mb := range fs.blks {
		mb.mu.Lock()
	}
}

// Unlock all existing message blocks.
// Lock held on entry.
func (fs *fileStore) unlockAllMsgBlocks() {
	for _, mb := range fs.blks {
		mb.mu.Unlock()
	}
}

func (fs *fileStore) UpdateConfig(cfg *StreamConfig) error {
	if fs.isClosed() {
		return ErrStoreClosed
	}
	if cfg.Name == _EMPTY_ {
		return fmt.Errorf("name required")
	}
	if cfg.Storage != FileStorage {
		return fmt.Errorf("fileStore requires file storage type in config")
	}

	fs.mu.Lock()
	new_cfg := FileStreamInfo{Created: fs.cfg.Created, StreamConfig: *cfg}
	old_cfg := fs.cfg
	// Messages block reference fs.cfg.Subjects (in subjString) under the
	// mb's lock, not fs' lock. So do the switch here under all existing
	// message blocks' lock in order to silence the DATA RACE detector.
	fs.lockAllMsgBlocks()
	fs.cfg = new_cfg
	fs.unlockAllMsgBlocks()
	if err := fs.writeStreamMeta(); err != nil {
		fs.lockAllMsgBlocks()
		fs.cfg = old_cfg
		fs.unlockAllMsgBlocks()
		fs.mu.Unlock()
		return err
	}

	// Limits checks and enforcement.
	fs.enforceMsgLimit()
	fs.enforceBytesLimit()

	// Do age timers.
	if fs.ageChk == nil && fs.cfg.MaxAge != 0 {
		fs.startAgeChk()
	}
	if fs.ageChk != nil && fs.cfg.MaxAge == 0 {
		fs.ageChk.Stop()
		fs.ageChk = nil
	}

	if cfg.MaxMsgsPer > 0 && cfg.MaxMsgsPer < old_cfg.MaxMsgsPer {
		fs.enforceMsgPerSubjectLimit()
	}
	fs.mu.Unlock()

	if cfg.MaxAge != 0 {
		fs.expireMsgs()
	}
	return nil
}

func dynBlkSize(retention RetentionPolicy, maxBytes int64) uint64 {
	if maxBytes > 0 {
		blkSize := (maxBytes / 4) + 1 // (25% overhead)
		// Round up to nearest 100
		if m := blkSize % 100; m != 0 {
			blkSize += 100 - m
		}
		if blkSize <= FileStoreMinBlkSize {
			blkSize = FileStoreMinBlkSize
		} else if blkSize >= FileStoreMaxBlkSize {
			blkSize = FileStoreMaxBlkSize
		} else {
			blkSize = defaultMediumBlockSize
		}
		return uint64(blkSize)
	}

	if retention == LimitsPolicy {
		// TODO(dlc) - Make the blocksize relative to this if set.
		return defaultLargeBlockSize
	} else {
		// TODO(dlc) - Make the blocksize relative to this if set.
		return defaultMediumBlockSize
	}
}

func genEncryptionKey(sc StoreCipher, seed []byte) (ek cipher.AEAD, err error) {
	if sc == ChaCha {
		ek, err = chacha20poly1305.NewX(seed)
	} else if sc == AES {
		block, e := aes.NewCipher(seed)
		if e != nil {
			return nil, err
		}
		ek, err = cipher.NewGCMWithNonceSize(block, block.BlockSize())
	} else {
		err = errUnknownCipher
	}
	return ek, err
}

// Generate an asset encryption key from the context and server PRF.
func (fs *fileStore) genEncryptionKeys(context string) (aek cipher.AEAD, bek cipher.Stream, seed, encrypted []byte, err error) {
	if fs.prf == nil {
		return nil, nil, nil, nil, errNoEncryption
	}
	// Generate key encryption key.
	rb, err := fs.prf([]byte(context))
	if err != nil {
		return nil, nil, nil, nil, err
	}

	sc := fs.fcfg.Cipher

	kek, err := genEncryptionKey(sc, rb)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	// Generate random asset encryption key seed.

	const seedSize = 32
	seed = make([]byte, seedSize)
	if n, err := rand.Read(seed); err != nil || n != seedSize {
		return nil, nil, nil, nil, err
	}

	aek, err = genEncryptionKey(sc, seed)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	// Generate our nonce. Use same buffer to hold encrypted seed.
	nonce := make([]byte, kek.NonceSize(), kek.NonceSize()+len(seed)+kek.Overhead())
	mrand.Read(nonce)

	bek, err = genBlockEncryptionKey(sc, seed[:], nonce)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	return aek, bek, seed, kek.Seal(nonce, nonce, seed, nil), nil
}

// Will generate the block encryption key.
func genBlockEncryptionKey(sc StoreCipher, seed, nonce []byte) (cipher.Stream, error) {
	if sc == ChaCha {
		return chacha20.NewUnauthenticatedCipher(seed, nonce)
	} else if sc == AES {
		block, err := aes.NewCipher(seed)
		if err != nil {
			return nil, err
		}
		return cipher.NewCTR(block, nonce), nil
	}
	return nil, errUnknownCipher
}

// Write out meta and the checksum.
// Lock should be held.
func (fs *fileStore) writeStreamMeta() error {
	if fs.prf != nil && fs.aek == nil {
		key, _, _, encrypted, err := fs.genEncryptionKeys(fs.cfg.Name)
		if err != nil {
			return err
		}
		keyFile := filepath.Join(fs.fcfg.StoreDir, JetStreamMetaFileKey)
		if _, err := os.Stat(keyFile); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.WriteFile(keyFile, encrypted, defaultFilePerms); err != nil {
			return err
		}
		// Set our aek.
		fs.aek = key
	}

	meta := filepath.Join(fs.fcfg.StoreDir, JetStreamMetaFile)
	if _, err := os.Stat(meta); err != nil && !os.IsNotExist(err) {
		return err
	}
	b, err := json.Marshal(fs.cfg)
	if err != nil {
		return err
	}
	// Encrypt if needed.
	if fs.aek != nil {
		nonce := make([]byte, fs.aek.NonceSize(), fs.aek.NonceSize()+len(b)+fs.aek.Overhead())
		mrand.Read(nonce)
		b = fs.aek.Seal(nonce, nonce, b, nil)
	}

	if err := os.WriteFile(meta, b, defaultFilePerms); err != nil {
		return err
	}
	fs.hh.Reset()
	fs.hh.Write(b)
	checksum := hex.EncodeToString(fs.hh.Sum(nil))
	sum := filepath.Join(fs.fcfg.StoreDir, JetStreamMetaFileSum)
	if err := os.WriteFile(sum, []byte(checksum), defaultFilePerms); err != nil {
		return err
	}
	return nil
}

// Pools to recycle the blocks to help with memory pressure.
var blkPoolBig sync.Pool    // 16MB
var blkPoolMedium sync.Pool // 8MB
var blkPoolSmall sync.Pool  // 2MB

// Get a new msg block based on sz estimate.
func getMsgBlockBuf(sz int) (buf []byte) {
	var pb interface{}
	if sz <= defaultSmallBlockSize {
		pb = blkPoolSmall.Get()
	} else if sz <= defaultMediumBlockSize {
		pb = blkPoolMedium.Get()
	} else {
		pb = blkPoolBig.Get()
	}
	if pb != nil {
		buf = *(pb.(*[]byte))
	} else {
		// Here we need to make a new blk.
		// If small leave as is..
		if sz > defaultSmallBlockSize && sz <= defaultMediumBlockSize {
			sz = defaultMediumBlockSize
		} else if sz > defaultMediumBlockSize {
			sz = defaultLargeBlockSize
		}
		buf = make([]byte, sz)
	}
	return buf[:0]
}

// Recycle the msg block.
func recycleMsgBlockBuf(buf []byte) {
	if buf == nil || cap(buf) < defaultSmallBlockSize {
		return
	}
	// Make sure to reset before placing back into pool.
	buf = buf[:0]

	// We need to make sure the load code gets a block that can fit the maximum for a size block.
	// E.g. 8, 16 etc. otherwise we thrash and actually make things worse by pulling it out, and putting
	// it right back in and making a new []byte.
	// From above we know its already >= defaultSmallBlockSize
	if sz := cap(buf); sz < defaultMediumBlockSize {
		blkPoolSmall.Put(&buf)
	} else if sz < defaultLargeBlockSize {
		blkPoolMedium.Put(&buf)
	} else {
		blkPoolBig.Put(&buf)
	}
}

const (
	msgHdrSize     = 22
	checksumSize   = 8
	emptyRecordLen = msgHdrSize + checksumSize
)

// This is the max room needed for index header.
const indexHdrSize = 7*binary.MaxVarintLen64 + hdrLen + checksumSize

// Lock should be held.
func (fs *fileStore) noTrackSubjects() bool {
	return !(len(fs.psim) > 0 || len(fs.cfg.Subjects) > 0 || fs.cfg.Mirror != nil || len(fs.cfg.Sources) > 0)
}

// Lock held on entry
func (fs *fileStore) recoverMsgBlock(fi os.FileInfo, index uint32) (*msgBlock, error) {
	mb := &msgBlock{fs: fs, index: index, cexp: fs.fcfg.CacheExpire, noTrack: fs.noTrackSubjects()}

	mdir := filepath.Join(fs.fcfg.StoreDir, msgDir)
	mb.mfn = filepath.Join(mdir, fi.Name())
	mb.ifn = filepath.Join(mdir, fmt.Sprintf(indexScan, index))
	mb.sfn = filepath.Join(mdir, fmt.Sprintf(fssScan, index))

	if mb.hh == nil {
		key := sha256.Sum256(fs.hashKeyForBlock(index))
		mb.hh, _ = highwayhash.New64(key[:])
	}

	var createdKeys bool

	// Check if encryption is enabled.
	if fs.prf != nil {
		ekey, err := os.ReadFile(filepath.Join(mdir, fmt.Sprintf(keyScan, mb.index)))
		if err != nil {
			// We do not seem to have keys even though we should. Could be a plaintext conversion.
			// Create the keys and we will double check below.
			if err := fs.genEncryptionKeysForBlock(mb); err != nil {
				return nil, err
			}
			createdKeys = true
		} else {
			if len(ekey) < minBlkKeySize {
				return nil, errBadKeySize
			}
			// Recover key encryption key.
			rb, err := fs.prf([]byte(fmt.Sprintf("%s:%d", fs.cfg.Name, mb.index)))
			if err != nil {
				return nil, err
			}

			sc := fs.fcfg.Cipher
			kek, err := genEncryptionKey(sc, rb)
			if err != nil {
				return nil, err
			}
			ns := kek.NonceSize()
			seed, err := kek.Open(nil, ekey[:ns], ekey[ns:], nil)
			if err != nil {
				// We may be here on a cipher conversion, so attempt to convert.
				if err = mb.convertCipher(); err != nil {
					return nil, err
				}
			} else {
				mb.seed, mb.nonce = seed, ekey[:ns]
			}
			mb.aek, err = genEncryptionKey(sc, mb.seed)
			if err != nil {
				return nil, err
			}
			if mb.bek, err = genBlockEncryptionKey(sc, mb.seed, mb.nonce); err != nil {
				return nil, err
			}
		}
	}

	// If we created keys here, let's check the data and if it is plaintext convert here.
	if createdKeys {
		if err := mb.convertToEncrypted(); err != nil {
			return nil, err
		}
	}

	// Open up the message file, but we will try to recover from the index file.
	// We will check that the last checksums match.
	file, err := os.Open(mb.mfn)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	if fi, err := file.Stat(); fi != nil {
		mb.rbytes = uint64(fi.Size())
	} else {
		return nil, err
	}
	// Grab last checksum from main block file.
	var lchk [8]byte
	if mb.rbytes >= checksumSize {
		if mb.bek != nil {
			if buf, _ := mb.loadBlock(nil); len(buf) >= checksumSize {
				mb.bek.XORKeyStream(buf, buf)
				copy(lchk[0:], buf[len(buf)-checksumSize:])
			}
		} else {
			file.ReadAt(lchk[:], fi.Size()-checksumSize)
		}
	}

	file.Close()

	// Read our index file. Use this as source of truth if possible.
	if err := mb.readIndexInfo(); err == nil {
		// Quick sanity check here.
		// Note this only checks that the message blk file is not newer then this file, or is empty and we expect empty.
		if (mb.rbytes == 0 && mb.msgs == 0) || bytes.Equal(lchk[:], mb.lchk[:]) {
			if mb.msgs > 0 && !mb.noTrack && fs.psim != nil {
				fs.populateGlobalPerSubjectInfo(mb)
				// Try to dump any state we needed on recovery.
				mb.tryForceExpireCacheLocked()
			}
			fs.addMsgBlock(mb)
			return mb, nil
		}
	}

	// If we get data loss rebuilding the message block state record that with the fs itself.
	if ld, _ := mb.rebuildState(); ld != nil {
		fs.addLostData(ld)
	}

	if mb.msgs > 0 && !mb.noTrack && fs.psim != nil {
		fs.populateGlobalPerSubjectInfo(mb)
		// Try to dump any state we needed on recovery.
		mb.tryForceExpireCacheLocked()
	}

	// Rewrite this to make sure we are sync'd.
	mb.writeIndexInfo()
	mb.closeFDs()
	fs.addMsgBlock(mb)
	return mb, nil
}

func (fs *fileStore) lostData() *LostStreamData {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	if fs.ld == nil {
		return nil
	}
	nld := *fs.ld
	return &nld
}

func (fs *fileStore) rebuildState(ld *LostStreamData) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.rebuildStateLocked(ld)
}

// Lock should be held.
func (fs *fileStore) addLostData(ld *LostStreamData) {
	if ld == nil {
		return
	}
	if fs.ld != nil {
		fs.ld.Msgs = append(fs.ld.Msgs, ld.Msgs...)
		msgs := fs.ld.Msgs
		sort.Slice(msgs, func(i, j int) bool { return msgs[i] < msgs[j] })
		fs.ld.Bytes += ld.Bytes
	} else {
		fs.ld = ld
	}
}

// Lock should be held.
func (fs *fileStore) rebuildStateLocked(ld *LostStreamData) {
	fs.addLostData(ld)

	fs.state.Msgs, fs.state.Bytes = 0, 0
	fs.state.FirstSeq, fs.state.LastSeq = 0, 0

	for _, mb := range fs.blks {
		mb.mu.RLock()
		fs.state.Msgs += mb.msgs
		fs.state.Bytes += mb.bytes
		if fs.state.FirstSeq == 0 || mb.first.seq < fs.state.FirstSeq {
			fs.state.FirstSeq = mb.first.seq
			fs.state.FirstTime = time.Unix(0, mb.first.ts).UTC()
		}
		fs.state.LastSeq = mb.last.seq
		fs.state.LastTime = time.Unix(0, mb.last.ts).UTC()
		mb.mu.RUnlock()
	}
}

// Attempt to convert the cipher used for this message block.
func (mb *msgBlock) convertCipher() error {
	fs := mb.fs
	sc := fs.fcfg.Cipher

	var osc StoreCipher
	switch sc {
	case ChaCha:
		osc = AES
	case AES:
		osc = ChaCha
	}

	mdir := filepath.Join(fs.fcfg.StoreDir, msgDir)
	ekey, err := os.ReadFile(filepath.Join(mdir, fmt.Sprintf(keyScan, mb.index)))
	if err != nil {
		return err
	}
	if len(ekey) < minBlkKeySize {
		return errBadKeySize
	}
	// Recover key encryption key.
	rb, err := fs.prf([]byte(fmt.Sprintf("%s:%d", fs.cfg.Name, mb.index)))
	if err != nil {
		return err
	}
	kek, err := genEncryptionKey(osc, rb)
	if err != nil {
		return err
	}
	ns := kek.NonceSize()
	seed, err := kek.Open(nil, ekey[:ns], ekey[ns:], nil)
	if err != nil {
		return err
	}
	nonce := ekey[:ns]

	bek, err := genBlockEncryptionKey(osc, seed, nonce)
	if err != nil {
		return err
	}

	buf, _ := mb.loadBlock(nil)
	bek.XORKeyStream(buf, buf)
	// Make sure we can parse with old cipher and key file.
	if err = mb.indexCacheBuf(buf); err != nil {
		return err
	}
	// Reset the cache since we just read everything in.
	mb.cache = nil

	// Generate new keys based on our
	if err := fs.genEncryptionKeysForBlock(mb); err != nil {
		// Put the old keyfile back.
		keyFile := filepath.Join(mdir, fmt.Sprintf(keyScan, mb.index))
		os.WriteFile(keyFile, ekey, defaultFilePerms)
		return err
	}
	mb.bek.XORKeyStream(buf, buf)
	if err := os.WriteFile(mb.mfn, buf, defaultFilePerms); err != nil {
		return err
	}
	// If we are here we want to delete other meta, e.g. idx, fss.
	os.Remove(mb.ifn)
	os.Remove(mb.sfn)

	return nil
}

// Convert a plaintext block to encrypted.
func (mb *msgBlock) convertToEncrypted() error {
	if mb.bek == nil {
		return nil
	}
	buf, err := mb.loadBlock(nil)
	if err != nil {
		return err
	}
	if err := mb.indexCacheBuf(buf); err != nil {
		// This likely indicates this was already encrypted or corrupt.
		mb.cache = nil
		return err
	}
	// Undo cache from above for later.
	mb.cache = nil
	mb.bek.XORKeyStream(buf, buf)
	if err := os.WriteFile(mb.mfn, buf, defaultFilePerms); err != nil {
		return err
	}
	if buf, err = os.ReadFile(mb.ifn); err == nil && len(buf) > 0 {
		if err := checkHeader(buf); err != nil {
			return err
		}
		buf = mb.aek.Seal(buf[:0], mb.nonce, buf, nil)
		if err := os.WriteFile(mb.ifn, buf, defaultFilePerms); err != nil {
			return err
		}
	}
	return nil
}

func (mb *msgBlock) rebuildState() (*LostStreamData, error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	return mb.rebuildStateLocked()
}

func (mb *msgBlock) rebuildStateLocked() (*LostStreamData, error) {
	startLastSeq := mb.last.seq

	buf, err := mb.loadBlock(nil)
	if err != nil || len(buf) == 0 {
		var ld *LostStreamData
		// No data to rebuild from here.
		if mb.msgs > 0 {
			// We need to declare lost data here.
			ld = &LostStreamData{Msgs: make([]uint64, 0, mb.msgs), Bytes: mb.bytes}
			for seq := mb.first.seq; seq <= mb.last.seq; seq++ {
				if _, ok := mb.dmap[seq]; !ok {
					ld.Msgs = append(ld.Msgs, seq)
				}
			}
			// Clear invalid state. We will let this blk be added in here.
			mb.msgs, mb.bytes, mb.rbytes, mb.fss = 0, 0, 0, nil
			mb.dmap = nil
			mb.first.seq = mb.last.seq + 1
		}
		return ld, err
	}

	// Clear state we need to rebuild.
	mb.msgs, mb.bytes, mb.rbytes, mb.fss = 0, 0, 0, nil
	mb.last.seq, mb.last.ts = 0, 0
	firstNeedsSet := true

	// Check if we need to decrypt.
	if mb.bek != nil && len(buf) > 0 {
		// Recreate to reset counter.
		mb.bek, err = genBlockEncryptionKey(mb.fs.fcfg.Cipher, mb.seed, mb.nonce)
		if err != nil {
			return nil, err
		}
		mb.bek.XORKeyStream(buf, buf)
	}

	mb.rbytes = uint64(len(buf))

	addToDmap := func(seq uint64) {
		if seq == 0 {
			return
		}
		if mb.dmap == nil {
			mb.dmap = make(map[uint64]struct{})
		}
		mb.dmap[seq] = struct{}{}
	}

	var le = binary.LittleEndian

	truncate := func(index uint32) {
		var fd *os.File
		if mb.mfd != nil {
			fd = mb.mfd
		} else {
			fd, err = os.OpenFile(mb.mfn, os.O_RDWR, defaultFilePerms)
			if err != nil {
				defer fd.Close()
			}
		}
		if fd == nil {
			return
		}
		if err := fd.Truncate(int64(index)); err == nil {
			// Update our checksum.
			if index >= 8 {
				var lchk [8]byte
				fd.ReadAt(lchk[:], int64(index-8))
				copy(mb.lchk[0:], lchk[:])
			}
			fd.Sync()
		}
	}

	gatherLost := func(lb uint32) *LostStreamData {
		var ld LostStreamData
		for seq := mb.last.seq + 1; seq <= startLastSeq; seq++ {
			ld.Msgs = append(ld.Msgs, seq)
		}
		ld.Bytes = uint64(lb)
		return &ld
	}

	for index, lbuf := uint32(0), uint32(len(buf)); index < lbuf; {
		if index+msgHdrSize > lbuf {
			truncate(index)
			return gatherLost(lbuf - index), nil
		}

		hdr := buf[index : index+msgHdrSize]
		rl, slen := le.Uint32(hdr[0:]), le.Uint16(hdr[20:])

		hasHeaders := rl&hbit != 0
		// Clear any headers bit that could be set.
		rl &^= hbit
		dlen := int(rl) - msgHdrSize
		// Do some quick sanity checks here.
		if dlen < 0 || int(slen) > (dlen-8) || dlen > int(rl) || rl > rlBadThresh {
			truncate(index)
			return gatherLost(lbuf - index), errBadMsg
		}

		if index+rl > lbuf {
			truncate(index)
			return gatherLost(lbuf - index), errBadMsg
		}

		seq := le.Uint64(hdr[4:])
		ts := int64(le.Uint64(hdr[12:]))

		// This is an old erased message, or a new one that we can track.
		if seq == 0 || seq&ebit != 0 || seq < mb.first.seq {
			seq = seq &^ ebit
			// Only add to dmap if past recorded first seq and non-zero.
			if seq != 0 && seq >= mb.first.seq {
				addToDmap(seq)
			}
			index += rl
			mb.last.seq = seq
			mb.last.ts = ts
			continue
		}

		// This is for when we have index info that adjusts for deleted messages
		// at the head. So the first.seq will be already set here. If this is larger
		// replace what we have with this seq.
		if firstNeedsSet && seq > mb.first.seq {
			firstNeedsSet, mb.first.seq, mb.first.ts = false, seq, ts
		}

		var deleted bool
		if mb.dmap != nil {
			_, deleted = mb.dmap[seq]
		}

		// Always set last.
		mb.last.seq = seq
		mb.last.ts = ts

		if !deleted {
			data := buf[index+msgHdrSize : index+rl]
			if hh := mb.hh; hh != nil {
				hh.Reset()
				hh.Write(hdr[4:20])
				hh.Write(data[:slen])
				if hasHeaders {
					hh.Write(data[slen+4 : dlen-8])
				} else {
					hh.Write(data[slen : dlen-8])
				}
				checksum := hh.Sum(nil)
				if !bytes.Equal(checksum, data[len(data)-8:]) {
					truncate(index)
					return gatherLost(lbuf - index), errBadMsg
				}
				copy(mb.lchk[0:], checksum)
			}

			if firstNeedsSet {
				firstNeedsSet, mb.first.seq, mb.first.ts = false, seq, ts
			}

			mb.msgs++
			mb.bytes += uint64(rl)

			// Rebuild per subject info if needed.
			if slen > 0 {
				if mb.fss == nil {
					mb.fss = make(map[string]*SimpleState)
				}
				// For the lookup, we cast the byte slice and there won't be any copy
				if ss := mb.fss[string(data[:slen])]; ss != nil {
					ss.Msgs++
					ss.Last = seq
				} else {
					// This will either use a subject from the config, or make a copy
					// so we don't reference the underlying buffer.
					subj := mb.subjString(data[:slen])
					mb.fss[subj] = &SimpleState{Msgs: 1, First: seq, Last: seq}
				}
				mb.fssNeedsWrite = true
			}
		}
		// Advance to next record.
		index += rl
	}

	// For empty msg blocks make sure we recover last seq correctly based off of first.
	if mb.msgs == 0 && mb.first.seq > 0 {
		mb.last.seq = mb.first.seq - 1
	}

	return nil, nil
}

func (fs *fileStore) recoverMsgs() error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Check for any left over purged messages.
	pdir := filepath.Join(fs.fcfg.StoreDir, purgeDir)
	<-dios
	if _, err := os.Stat(pdir); err == nil {
		os.RemoveAll(pdir)
	}
	dios <- struct{}{}

	mdir := filepath.Join(fs.fcfg.StoreDir, msgDir)
	fis, err := os.ReadDir(mdir)
	if err != nil {
		return errNotReadable
	}

	// Recover all of the msg blocks.
	// These can come in a random order, so account for that.
	for _, fi := range fis {
		var index uint32
		if n, err := fmt.Sscanf(fi.Name(), blkScan, &index); err == nil && n == 1 {
			finfo, err := fi.Info()
			if err != nil {
				return err
			}
			if mb, err := fs.recoverMsgBlock(finfo, index); err == nil && mb != nil {
				if fs.state.FirstSeq == 0 || mb.first.seq < fs.state.FirstSeq {
					fs.state.FirstSeq = mb.first.seq
					fs.state.FirstTime = time.Unix(0, mb.first.ts).UTC()
				}
				if mb.last.seq > fs.state.LastSeq {
					fs.state.LastSeq = mb.last.seq
					fs.state.LastTime = time.Unix(0, mb.last.ts).UTC()
				}
				fs.state.Msgs += mb.msgs
				fs.state.Bytes += mb.bytes
			} else {
				return err
			}
		}
	}

	// Now make sure to sort blks for efficient lookup later with selectMsgBlock().
	if len(fs.blks) > 0 {
		sort.Slice(fs.blks, func(i, j int) bool { return fs.blks[i].index < fs.blks[j].index })
		fs.lmb = fs.blks[len(fs.blks)-1]
	} else {
		_, err = fs.newMsgBlockForWrite()
	}

	// Check if we encountered any lost data.
	if fs.ld != nil {
		var emptyBlks []*msgBlock
		for _, mb := range fs.blks {
			if mb.msgs == 0 && mb.rbytes == 0 {
				if mb == fs.lmb {
					mb.first.seq, mb.first.ts = mb.last.seq+1, 0
					mb.closeAndKeepIndex(false)
				} else {
					emptyBlks = append(emptyBlks, mb)
				}
			}
		}
		for _, mb := range emptyBlks {
			fs.removeMsgBlock(mb)
		}
	}

	if err != nil {
		return err
	}

	// Check for keyfiles orphans.
	if kms, err := filepath.Glob(filepath.Join(mdir, keyScanAll)); err == nil && len(kms) > 0 {
		valid := make(map[uint32]bool)
		for _, mb := range fs.blks {
			valid[mb.index] = true
		}
		for _, fn := range kms {
			var index uint32
			shouldRemove := true
			if n, err := fmt.Sscanf(filepath.Base(fn), keyScan, &index); err == nil && n == 1 && valid[index] {
				shouldRemove = false
			}
			if shouldRemove {
				os.Remove(fn)
			}
		}
	}

	// Limits checks and enforcement.
	fs.enforceMsgLimit()
	fs.enforceBytesLimit()

	// Do age checks too, make sure to call in place.
	if fs.cfg.MaxAge != 0 {
		fs.expireMsgsOnRecover()
		fs.startAgeChk()
	}

	// If we have max msgs per subject make sure the is also enforced.
	if fs.cfg.MaxMsgsPer > 0 {
		fs.enforceMsgPerSubjectLimit()
	}

	return nil
}

// Will expire msgs that have aged out on restart.
// We will treat this differently in case we have a recovery
// that will expire alot of messages on startup.
// Should only be called on startup.
// Lock should be held.
func (fs *fileStore) expireMsgsOnRecover() {
	if fs.state.Msgs == 0 {
		return
	}

	var minAge = time.Now().UnixNano() - int64(fs.cfg.MaxAge)
	var purged, bytes uint64
	var deleted int
	var nts int64

	deleteEmptyBlock := func(mb *msgBlock) bool {
		// If we are the last keep state to remember first sequence.
		if mb == fs.lmb {
			// Do this part by hand since not deleting one by one.
			mb.first.seq, mb.first.ts = mb.last.seq+1, 0
			mb.closeAndKeepIndex(false)
			// Clear any global subject state.
			fs.psim = make(map[string]*psi)
			return false
		}
		mb.dirtyCloseWithRemove(true)
		deleted++
		return true
	}

	for _, mb := range fs.blks {
		mb.mu.Lock()
		if minAge < mb.first.ts {
			nts = mb.first.ts
			mb.mu.Unlock()
			break
		}
		// Can we remove whole block here?
		if mb.last.ts <= minAge {
			purged += mb.msgs
			bytes += mb.bytes
			didRemove := deleteEmptyBlock(mb)
			mb.mu.Unlock()
			if !didRemove {
				mb.writeIndexInfo()
			}
			continue
		}

		// If we are here we have to process the interior messages of this blk.
		if err := mb.loadMsgsWithLock(); err != nil {
			mb.mu.Unlock()
			break
		}

		var smv StoreMsg

		// Walk messages and remove if expired.
		mb.ensurePerSubjectInfoLoaded()
		for seq := mb.first.seq; seq <= mb.last.seq; seq++ {
			sm, err := mb.cacheLookup(seq, &smv)
			// Process interior deleted msgs.
			if err == errDeletedMsg {
				// Update dmap.
				if len(mb.dmap) > 0 {
					delete(mb.dmap, seq)
					if len(mb.dmap) == 0 {
						mb.dmap = nil
					}
				}
				// Keep this update just in case since we are removing dmap entries.
				mb.first.seq = seq
				continue
			}
			// Break on other errors.
			if err != nil || sm == nil {
				// Keep this update just in case since we could have removed dmap entries.
				mb.first.seq = seq
				break
			}

			// No error and sm != nil from here onward.

			// Check for done.
			if minAge < sm.ts {
				mb.first.seq = sm.seq
				mb.first.ts = sm.ts
				nts = sm.ts
				break
			}

			// Delete the message here.
			if mb.msgs > 0 {
				sz := fileStoreMsgSize(sm.subj, sm.hdr, sm.msg)
				mb.bytes -= sz
				bytes += sz
				mb.msgs--
				purged++
			}
			// Update fss
			// Make sure we have fss loaded.
			mb.removeSeqPerSubject(sm.subj, seq, nil)
			fs.removePerSubject(sm.subj)
		}

		// Check if empty after processing, could happen if tail of messages are all deleted.
		needWriteIndex := true
		if mb.msgs == 0 {
			needWriteIndex = !deleteEmptyBlock(mb)
		}
		mb.mu.Unlock()
		if needWriteIndex {
			mb.writeIndexInfo()
		}
		break
	}

	if nts > 0 {
		// Make sure to set age check based on this value.
		fs.resetAgeChk(nts - minAge)
	}

	if deleted > 0 {
		// Update block map.
		if fs.bim != nil {
			for _, mb := range fs.blks[:deleted] {
				delete(fs.bim, mb.index)
			}
		}
		// Update blks slice.
		fs.blks = copyMsgBlocks(fs.blks[deleted:])
		if lb := len(fs.blks); lb == 0 {
			fs.lmb = nil
		} else {
			fs.lmb = fs.blks[lb-1]
		}
	}
	// Update top level accounting.
	fs.state.Msgs -= purged
	fs.state.Bytes -= bytes
	// Make sure to we properly set the fs first sequence and timestamp.
	fs.selectNextFirst()
}

func copyMsgBlocks(src []*msgBlock) []*msgBlock {
	if src == nil {
		return nil
	}
	dst := make([]*msgBlock, len(src))
	copy(dst, src)
	return dst
}

// GetSeqFromTime looks for the first sequence number that has
// the message with >= timestamp.
// FIXME(dlc) - inefficient, and dumb really. Make this better.
func (fs *fileStore) GetSeqFromTime(t time.Time) uint64 {
	fs.mu.RLock()
	lastSeq := fs.state.LastSeq
	closed := fs.closed
	fs.mu.RUnlock()

	if closed {
		return 0
	}

	mb := fs.selectMsgBlockForStart(t)
	if mb == nil {
		return lastSeq + 1
	}

	mb.mu.RLock()
	fseq := mb.first.seq
	lseq := mb.last.seq
	mb.mu.RUnlock()

	var smv StoreMsg

	// Linear search, hence the dumb part..
	ts := t.UnixNano()
	for seq := fseq; seq <= lseq; seq++ {
		sm, _, _ := mb.fetchMsg(seq, &smv)
		if sm != nil && sm.ts >= ts {
			return sm.seq
		}
	}
	return 0
}

// Find the first matching message.
func (mb *msgBlock) firstMatching(filter string, wc bool, start uint64, sm *StoreMsg) (*StoreMsg, bool, error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if err := mb.ensurePerSubjectInfoLoaded(); err != nil {
		return nil, false, err
	}

	fseq, isAll, subs := start, filter == _EMPTY_ || filter == fwcs, []string{filter}

	// If we only have 1 subject currently and it matches our filter we can also set isAll.
	if !isAll && len(mb.fss) == 1 {
		_, isAll = mb.fss[filter]
	}
	// Skip scan of mb.fss if number of messages in the block are less than
	// 1/2 the number of subjects in mb.fss. Or we have a wc and lots of fss entries.
	const linearScanMaxFSS = 32
	doLinearScan := isAll || 2*int(mb.last.seq-start) < len(mb.fss) || (wc && len(mb.fss) > linearScanMaxFSS)

	if !doLinearScan {
		// If we have a wildcard match against all tracked subjects we know about.
		if wc {
			subs = subs[:0]
			for subj := range mb.fss {
				if subjectIsSubsetMatch(subj, filter) {
					subs = append(subs, subj)
				}
			}
		}
		fseq = mb.last.seq + 1
		for _, subj := range subs {
			ss := mb.fss[subj]
			if ss == nil || start > ss.Last || ss.First >= fseq {
				continue
			}
			if ss.First < start {
				fseq = start
			} else {
				fseq = ss.First
			}
		}
	}

	if fseq > mb.last.seq {
		return nil, false, ErrStoreMsgNotFound
	}

	if mb.cacheNotLoaded() {
		if err := mb.loadMsgsWithLock(); err != nil {
			return nil, false, err
		}
	}

	if sm == nil {
		sm = new(StoreMsg)
	}

	for seq := fseq; seq <= mb.last.seq; seq++ {
		llseq := mb.llseq
		fsm, err := mb.cacheLookup(seq, sm)
		if err != nil {
			continue
		}
		expireOk := seq == mb.last.seq && mb.llseq == seq
		if doLinearScan {
			if isAll {
				return fsm, expireOk, nil
			}
			if wc && subjectIsSubsetMatch(fsm.subj, filter) {
				return fsm, expireOk, nil
			} else if !wc && fsm.subj == filter {
				return fsm, expireOk, nil
			}
		} else {
			for _, subj := range subs {
				if fsm.subj == subj {
					return fsm, expireOk, nil
				}
			}
		}
		// If we are here we did not match, so put the llseq back.
		mb.llseq = llseq
	}

	return nil, false, ErrStoreMsgNotFound
}

// This will traverse a message block and generate the filtered pending.
func (mb *msgBlock) filteredPending(subj string, wc bool, seq uint64) (total, first, last uint64) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	return mb.filteredPendingLocked(subj, wc, seq)
}

// This will traverse a message block and generate the filtered pending.
// Lock should be held.
func (mb *msgBlock) filteredPendingLocked(filter string, wc bool, sseq uint64) (total, first, last uint64) {
	isAll := filter == _EMPTY_ || filter == fwcs

	// First check if we can optimize this part.
	// This means we want all and the starting sequence was before this block.
	if isAll && sseq <= mb.first.seq {
		return mb.msgs, mb.first.seq, mb.last.seq
	}

	update := func(ss *SimpleState) {
		total += ss.Msgs
		if first == 0 || ss.First < first {
			first = ss.First
		}
		if ss.Last > last {
			last = ss.Last
		}
	}

	// Make sure we have fss loaded.
	mb.ensurePerSubjectInfoLoaded()

	tsa := [32]string{}
	fsa := [32]string{}
	fts := tokenizeSubjectIntoSlice(fsa[:0], filter)

	// 1. See if we match any subs from fss.
	// 2. If we match and the sseq is past ss.Last then we can use meta only.
	// 3. If we match and we need to do a partial, break and clear any totals and do a full scan like num pending.

	isMatch := func(subj string) bool {
		if !wc {
			return subj == filter
		}
		tts := tokenizeSubjectIntoSlice(tsa[:0], subj)
		return isSubsetMatchTokenized(tts, fts)
	}

	var havePartial bool
	for subj, ss := range mb.fss {
		if isAll || isMatch(subj) {
			if sseq <= ss.First {
				update(ss)
			} else if sseq <= ss.Last {
				// We matched but its a partial.
				havePartial = true
				break
			}
		}
	}

	// If we did not encounter any partials we can return here.
	if !havePartial {
		return total, first, last
	}

	// If we are here we need to scan the msgs.
	// Clear what we had.
	total, first, last = 0, 0, 0

	// If we load the cache for a linear scan we want to expire that cache upon exit.
	var shouldExpire bool
	if mb.cacheNotLoaded() {
		mb.loadMsgsWithLock()
		shouldExpire = true
	}

	var smv StoreMsg
	for seq := sseq; seq <= mb.last.seq; seq++ {
		sm, _ := mb.cacheLookup(seq, &smv)
		if sm == nil {
			continue
		}
		if isAll || isMatch(sm.subj) {
			total++
			if first == 0 || seq < first {
				first = seq
			}
			if seq > last {
				last = seq
			}
		}
	}
	// If we loaded this block for this operation go ahead and expire it here.
	if shouldExpire {
		mb.tryForceExpireCacheLocked()
	}

	return total, first, last
}

// FilteredState will return the SimpleState associated with the filtered subject and a proposed starting sequence.
func (fs *fileStore) FilteredState(sseq uint64, subj string) SimpleState {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	lseq := fs.state.LastSeq
	if sseq < fs.state.FirstSeq {
		sseq = fs.state.FirstSeq
	}

	// Returned state.
	var ss SimpleState

	// If past the end no results.
	if sseq > lseq {
		return ss
	}

	// If we want all msgs that match we can shortcircuit.
	// TODO(dlc) - This can be extended for all cases but would
	// need to be careful on total msgs calculations etc.
	if sseq == fs.state.FirstSeq {
		fs.numFilteredPending(subj, &ss)
	} else {
		wc := subjectHasWildcard(subj)
		// Tracking subject state.
		// TODO(dlc) - Optimize for 2.10 with avl tree and no atomics per block.
		for _, mb := range fs.blks {
			// Skip blocks that are less than our starting sequence.
			if sseq > atomic.LoadUint64(&mb.last.seq) {
				continue
			}
			t, f, l := mb.filteredPending(subj, wc, sseq)
			ss.Msgs += t
			if ss.First == 0 || (f > 0 && f < ss.First) {
				ss.First = f
			}
			if l > ss.Last {
				ss.Last = l
			}
		}
	}

	return ss
}

// Optimized way for getting all num pending matching a filter subject.
// Lock should be held.
func (fs *fileStore) numFilteredPending(filter string, ss *SimpleState) {
	isAll := filter == _EMPTY_ || filter == fwcs

	// If isAll we do not need to do anything special to calculate the first and last and total.
	if isAll {
		ss.First = fs.state.FirstSeq
		ss.Last = fs.state.LastSeq
		ss.Msgs = fs.state.Msgs
		return
	}

	tsa := [32]string{}
	fsa := [32]string{}
	fts := tokenizeSubjectIntoSlice(fsa[:0], filter)

	start, stop := uint32(math.MaxUint32), uint32(0)
	for subj, psi := range fs.psim {
		if isAll {
			ss.Msgs += psi.total
		} else {
			tts := tokenizeSubjectIntoSlice(tsa[:0], subj)
			if isSubsetMatchTokenized(tts, fts) {
				ss.Msgs += psi.total
				// Keep track of start and stop indexes for this subject.
				if psi.fblk < start {
					start = psi.fblk
				}
				if psi.lblk > stop {
					stop = psi.lblk
				}
			}
		}
	}
	// If not collecting all we do need to figure out the first and last sequences.
	if !isAll {
		wc := subjectHasWildcard(filter)
		// Do start
		mb := fs.bim[start]
		if mb != nil {
			_, f, _ := mb.filteredPending(filter, wc, 0)
			ss.First = f
		}
		if ss.First == 0 {
			// This is a miss. This can happen since psi.fblk is lazy, but should be very rare.
			for i := start + 1; i <= stop; i++ {
				mb := fs.bim[i]
				if mb == nil {
					continue
				}
				if _, f, _ := mb.filteredPending(filter, wc, 0); f > 0 {
					ss.First = f
					break
				}
			}
		}
		// Now last
		if mb = fs.bim[stop]; mb != nil {
			_, _, l := mb.filteredPending(filter, wc, 0)
			ss.Last = l
		}
	}
}

// SubjectsState returns a map of SimpleState for all matching subjects.
func (fs *fileStore) SubjectsState(subject string) map[string]SimpleState {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	if fs.state.Msgs == 0 {
		return nil
	}

	start, stop := fs.blks[0], fs.lmb
	// We can short circuit if not a wildcard using psim for start and stop.
	if !subjectHasWildcard(subject) {
		info := fs.psim[subject]
		if info == nil {
			return nil
		}
		start, stop = fs.bim[info.fblk], fs.bim[info.lblk]
	}

	// Aggregate fss.
	fss := make(map[string]SimpleState)
	var startFound bool

	for _, mb := range fs.blks {
		if !startFound {
			if mb != start {
				continue
			}
			startFound = true
		}

		mb.mu.Lock()
		// Make sure we have fss loaded.
		mb.ensurePerSubjectInfoLoaded()
		for subj, ss := range mb.fss {
			if subject == _EMPTY_ || subject == fwcs || subjectIsSubsetMatch(subj, subject) {
				oss := fss[subj]
				if oss.First == 0 { // New
					fss[subj] = *ss
				} else {
					// Merge here.
					oss.Last, oss.Msgs = ss.Last, oss.Msgs+ss.Msgs
					fss[subj] = oss
				}
			}
		}
		mb.mu.Unlock()

		if mb == stop {
			break
		}
	}

	return fss
}

// NumPending will return the number of pending messages matching the filter subject starting at sequence.
// Optimized for stream num pending calculations for consumers.
func (fs *fileStore) NumPending(sseq uint64, filter string, lastPerSubject bool) (total, validThrough uint64) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	// This can always be last for these purposes.
	validThrough = fs.state.LastSeq

	if fs.state.Msgs == 0 || sseq > fs.state.LastSeq {
		return 0, validThrough
	}

	// Track starting for both block for the sseq and staring block that matches any subject.
	var seqStart, subjStart int

	// See if we need to figure out starting block per sseq.
	if sseq > fs.state.FirstSeq {
		seqStart, _ = fs.selectMsgBlockWithIndex(sseq)
	}

	tsa := [32]string{}
	fsa := [32]string{}
	fts := tokenizeSubjectIntoSlice(fsa[:0], filter)
	isAll := filter == _EMPTY_ || filter == fwcs
	wc := subjectHasWildcard(filter)

	isMatch := func(subj string) bool {
		if isAll {
			return true
		}
		if !wc {
			return subj == filter
		}
		tts := tokenizeSubjectIntoSlice(tsa[:0], subj)
		return isSubsetMatchTokenized(tts, fts)
	}

	// If we would need to scan more from the beginning, revert back to calculating directly here.
	// TODO(dlc) - Redo properly with sublists etc for subject-based filtering.
	if lastPerSubject || seqStart >= (len(fs.blks)/2) {
		// If we need to track seen for last per subject.
		var seen map[string]bool
		if lastPerSubject {
			seen = make(map[string]bool)
		}

		for i := seqStart; i < len(fs.blks); i++ {
			mb := fs.blks[i]
			mb.mu.Lock()
			var t uint64
			if isAll && sseq <= mb.first.seq {
				if lastPerSubject {
					for subj := range mb.fss {
						if !seen[subj] {
							total++
							seen[subj] = true
						}
					}
				} else {
					total += mb.msgs
				}
				mb.mu.Unlock()
				continue
			}

			// If we are here we need to at least scan the subject fss.
			// Make sure we have fss loaded.
			mb.ensurePerSubjectInfoLoaded()
			var havePartial bool
			for subj, ss := range mb.fss {
				if !seen[subj] && isMatch(subj) {
					if lastPerSubject {
						// Can't have a partials with last by subject.
						if sseq <= ss.Last {
							t++
							seen[subj] = true
						}
					} else {
						if sseq <= ss.First {
							t += ss.Msgs
						} else if sseq <= ss.Last {
							// We matched but its a partial.
							havePartial = true
							break
						}
					}
				}
			}
			// See if we need to scan msgs here.
			if havePartial {
				// Clear on partial.
				t = 0
				// If we load the cache for a linear scan we want to expire that cache upon exit.
				var shouldExpire bool
				if mb.cacheNotLoaded() {
					mb.loadMsgsWithLock()
					shouldExpire = true
				}
				var smv StoreMsg
				for seq := sseq; seq <= mb.last.seq; seq++ {
					if sm, _ := mb.cacheLookup(seq, &smv); sm != nil && (isAll || isMatch(sm.subj)) {
						t++
					}
				}
				// If we loaded this block for this operation go ahead and expire it here.
				if shouldExpire {
					mb.tryForceExpireCacheLocked()
				}
			}
			mb.mu.Unlock()
			total += t
		}
		return total, validThrough
	}

	// If we are here its better to calculate totals from psim and adjust downward by scanning less blocks.
	// TODO(dlc) - Eventually when sublist uses generics, make this sublist driven instead.
	start := uint32(math.MaxUint32)
	for subj, psi := range fs.psim {
		if isMatch(subj) {
			if lastPerSubject {
				total++
				// Keep track of start index for this subject.
				// Use last block in this case.
				if psi.lblk < start {
					start = psi.lblk
				}
			} else {
				total += psi.total
				// Keep track of start index for this subject.
				if psi.fblk < start {
					start = psi.fblk
				}
			}
		}
	}
	// See if we were asked for all, if so we are done.
	if sseq <= fs.state.FirstSeq {
		return total, validThrough
	}

	// If we are here we need to calculate partials for the first blocks.
	subjStart = int(start)
	firstSubjBlk := fs.bim[uint32(subjStart)]
	var firstSubjBlkFound bool
	var smv StoreMsg

	// Adjust in case not found.
	if firstSubjBlk == nil {
		firstSubjBlkFound = true
	}

	// Track how many we need to adjust against the total.
	var adjust uint64

	for i := 0; i <= seqStart; i++ {
		mb := fs.blks[i]

		// We can skip blks if we know they are below the first one that has any subject matches.
		if !firstSubjBlkFound {
			if mb == firstSubjBlk {
				firstSubjBlkFound = true
			} else {
				continue
			}
		}

		// We need to scan this block.
		var shouldExpire bool
		mb.mu.Lock()
		// Check if we should include all of this block in adjusting. If so work with metadata.
		if sseq > mb.last.seq {
			// We need to adjust for all matches in this block.
			// We will scan fss state vs messages themselves.
			// Make sure we have fss loaded.
			mb.ensurePerSubjectInfoLoaded()
			for subj, ss := range mb.fss {
				if isMatch(subj) {
					if lastPerSubject {
						adjust++
					} else {
						adjust += ss.Msgs
					}
				}
			}
		} else {
			// This is the last block. We need to scan per message here.
			if mb.cacheNotLoaded() {
				if err := mb.loadMsgsWithLock(); err != nil {
					mb.mu.Unlock()
					return 0, 0
				}
				shouldExpire = true
			}

			var last = mb.last.seq
			if sseq < last {
				last = sseq
			}
			for seq := mb.first.seq; seq < last; seq++ {
				sm, _ := mb.cacheLookup(seq, &smv)
				if sm == nil {
					continue
				}
				// Check if it matches our filter.
				if isMatch(sm.subj) && sm.seq < sseq {
					adjust++
				}
			}
		}
		// If we loaded the block try to force expire.
		if shouldExpire {
			mb.tryForceExpireCacheLocked()
		}
		mb.mu.Unlock()
	}
	// Make final adjustment.
	total -= adjust

	return total, validThrough
}

// SubjectsTotal return message totals per subject.
func (fs *fileStore) SubjectsTotals(filter string) map[string]uint64 {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	if len(fs.psim) == 0 {
		return nil
	}

	tsa := [32]string{}
	fsa := [32]string{}
	fts := tokenizeSubjectIntoSlice(fsa[:0], filter)
	isAll := filter == _EMPTY_ || filter == fwcs
	wc := subjectHasWildcard(filter)

	isMatch := func(subj string) bool {
		if !wc {
			return subj == filter
		}
		tts := tokenizeSubjectIntoSlice(tsa[:0], subj)
		return isSubsetMatchTokenized(tts, fts)
	}

	fst := make(map[string]uint64)
	for subj, psi := range fs.psim {
		if isAll || isMatch(subj) {
			fst[subj] = psi.total
		}
	}
	return fst
}

// RegisterStorageUpdates registers a callback for updates to storage changes.
// It will present number of messages and bytes as a signed integer and an
// optional sequence number of the message if a single.
func (fs *fileStore) RegisterStorageUpdates(cb StorageUpdateHandler) {
	fs.mu.Lock()
	fs.scb = cb
	bsz := fs.state.Bytes
	fs.mu.Unlock()
	if cb != nil && bsz > 0 {
		cb(0, int64(bsz), 0, _EMPTY_)
	}
}

// Helper to get hash key for specific message block.
// Lock should be held
func (fs *fileStore) hashKeyForBlock(index uint32) []byte {
	return []byte(fmt.Sprintf("%s-%d", fs.cfg.Name, index))
}

func (mb *msgBlock) setupWriteCache(buf []byte) {
	// Make sure we have a cache setup.
	if mb.cache != nil {
		return
	}

	// Setup simple cache.
	mb.cache = &cache{buf: buf}
	// Make sure we set the proper cache offset if we have existing data.
	var fi os.FileInfo
	if mb.mfd != nil {
		fi, _ = mb.mfd.Stat()
	} else if mb.mfn != _EMPTY_ {
		fi, _ = os.Stat(mb.mfn)
	}
	if fi != nil {
		mb.cache.off = int(fi.Size())
	}
	mb.llts = time.Now().UnixNano()
	mb.startCacheExpireTimer()
}

// This rolls to a new append msg block.
// Lock should be held.
func (fs *fileStore) newMsgBlockForWrite() (*msgBlock, error) {
	index := uint32(1)
	var rbuf []byte

	if lmb := fs.lmb; lmb != nil {
		index = lmb.index + 1

		// Make sure to write out our index file if needed.
		if lmb.indexNeedsUpdate() {
			lmb.writeIndexInfo()
		}

		// Determine if we can reclaim any resources here.
		if fs.fip {
			lmb.mu.Lock()
			lmb.closeFDsLocked()
			if lmb.cache != nil {
				// Reset write timestamp and see if we can expire this cache.
				rbuf = lmb.tryExpireWriteCache()
			}
			lmb.mu.Unlock()
		}
	}

	mb := &msgBlock{fs: fs, index: index, cexp: fs.fcfg.CacheExpire, noTrack: fs.noTrackSubjects()}

	// Lock should be held to quiet race detector.
	mb.mu.Lock()
	mb.setupWriteCache(rbuf)
	mb.fss = make(map[string]*SimpleState)
	mb.mu.Unlock()

	// Now do local hash.
	key := sha256.Sum256(fs.hashKeyForBlock(index))
	hh, err := highwayhash.New64(key[:])
	if err != nil {
		return nil, fmt.Errorf("could not create hash: %v", err)
	}
	mb.hh = hh

	mdir := filepath.Join(fs.fcfg.StoreDir, msgDir)
	mb.mfn = filepath.Join(mdir, fmt.Sprintf(blkScan, mb.index))
	mfd, err := os.OpenFile(mb.mfn, os.O_CREATE|os.O_RDWR, defaultFilePerms)
	if err != nil {
		mb.dirtyCloseWithRemove(true)
		return nil, fmt.Errorf("Error creating msg block file [%q]: %v", mb.mfn, err)
	}
	mb.mfd = mfd

	mb.ifn = filepath.Join(mdir, fmt.Sprintf(indexScan, mb.index))
	ifd, err := os.OpenFile(mb.ifn, os.O_CREATE|os.O_RDWR, defaultFilePerms)
	if err != nil {
		mb.dirtyCloseWithRemove(true)
		return nil, fmt.Errorf("Error creating msg index file [%q]: %v", mb.mfn, err)
	}
	mb.ifd = ifd

	// For subject based info.
	mb.sfn = filepath.Join(mdir, fmt.Sprintf(fssScan, mb.index))

	// Check if encryption is enabled.
	if fs.prf != nil {
		if err := fs.genEncryptionKeysForBlock(mb); err != nil {
			return nil, err
		}
	}

	// Set cache time to creation time to start.
	ts := time.Now().UnixNano()
	// Race detector wants these protected.
	mb.mu.Lock()
	mb.llts, mb.lwts = 0, ts
	// Remember our last sequence number.
	mb.first.seq = fs.state.LastSeq + 1
	mb.last.seq = fs.state.LastSeq
	mb.mu.Unlock()

	// If we know we will need this so go ahead and spin up.
	if !fs.fip {
		mb.spinUpFlushLoop()
	}

	// Add to our list of blocks and mark as last.
	fs.addMsgBlock(mb)

	return mb, nil
}

// Generate the keys for this message block and write them out.
func (fs *fileStore) genEncryptionKeysForBlock(mb *msgBlock) error {
	if mb == nil {
		return nil
	}
	key, bek, seed, encrypted, err := fs.genEncryptionKeys(fmt.Sprintf("%s:%d", fs.cfg.Name, mb.index))
	if err != nil {
		return err
	}
	mb.aek, mb.bek, mb.seed, mb.nonce = key, bek, seed, encrypted[:key.NonceSize()]
	mdir := filepath.Join(fs.fcfg.StoreDir, msgDir)
	keyFile := filepath.Join(mdir, fmt.Sprintf(keyScan, mb.index))
	if _, err := os.Stat(keyFile); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.WriteFile(keyFile, encrypted, defaultFilePerms); err != nil {
		return err
	}
	mb.kfn = keyFile
	return nil
}

// Stores a raw message with expected sequence number and timestamp.
// Lock should be held.
func (fs *fileStore) storeRawMsg(subj string, hdr, msg []byte, seq uint64, ts int64) (err error) {
	if fs.closed {
		return ErrStoreClosed
	}

	// Per subject max check needed.
	mmp := uint64(fs.cfg.MaxMsgsPer)
	var psmc uint64
	psmax := mmp > 0 && len(subj) > 0
	if psmax {
		if info, ok := fs.psim[subj]; ok {
			psmc = info.total
		}
	}

	var fseq uint64
	// Check if we are discarding new messages when we reach the limit.
	if fs.cfg.Discard == DiscardNew {
		var asl bool
		if psmax && psmc >= mmp {
			// If we are instructed to discard new per subject, this is an error.
			if fs.cfg.DiscardNewPer {
				return ErrMaxMsgsPerSubject
			}
			if fseq, err = fs.firstSeqForSubj(subj); err != nil {
				return err
			}
			asl = true
		}
		if fs.cfg.MaxMsgs > 0 && fs.state.Msgs >= uint64(fs.cfg.MaxMsgs) && !asl {
			return ErrMaxMsgs
		}
		if fs.cfg.MaxBytes > 0 && fs.state.Bytes+uint64(len(msg)+len(hdr)) >= uint64(fs.cfg.MaxBytes) {
			if !asl || fs.sizeForSeq(fseq) <= len(msg)+len(hdr) {
				return ErrMaxBytes
			}
		}
	}

	// Check sequence.
	if seq != fs.state.LastSeq+1 {
		if seq > 0 {
			return ErrSequenceMismatch
		}
		seq = fs.state.LastSeq + 1
	}

	// Write msg record.
	n, err := fs.writeMsgRecord(seq, ts, subj, hdr, msg)
	if err != nil {
		return err
	}

	// Adjust top level tracking of per subject msg counts.
	if len(subj) > 0 {
		index := fs.lmb.index
		if info, ok := fs.psim[subj]; ok {
			info.total++
			if index > info.lblk {
				info.lblk = index
			}
		} else {
			fs.psim[subj] = &psi{total: 1, fblk: index, lblk: index}
		}
	}

	// Adjust first if needed.
	now := time.Unix(0, ts).UTC()
	if fs.state.Msgs == 0 {
		fs.state.FirstSeq = seq
		fs.state.FirstTime = now
	}

	fs.state.Msgs++
	fs.state.Bytes += n
	fs.state.LastSeq = seq
	fs.state.LastTime = now

	// Enforce per message limits.
	// We snapshotted psmc before our actual write, so >= comparison needed.
	if psmax && psmc >= mmp {
		// We may have done this above.
		if fseq == 0 {
			fseq, _ = fs.firstSeqForSubj(subj)
		}
		if ok, _ := fs.removeMsgViaLimits(fseq); ok {
			// Make sure we are below the limit.
			if psmc--; psmc >= mmp {
				for info, ok := fs.psim[subj]; ok && info.total > mmp; info, ok = fs.psim[subj] {
					if seq, _ := fs.firstSeqForSubj(subj); seq > 0 {
						if ok, _ := fs.removeMsgViaLimits(seq); !ok {
							break
						}
					} else {
						break
					}
				}
			}
		}
	}

	// Limits checks and enforcement.
	// If they do any deletions they will update the
	// byte count on their own, so no need to compensate.
	fs.enforceMsgLimit()
	fs.enforceBytesLimit()

	// Check if we have and need the age expiration timer running.
	if fs.ageChk == nil && fs.cfg.MaxAge != 0 {
		fs.startAgeChk()
	}

	return nil
}

// StoreRawMsg stores a raw message with expected sequence number and timestamp.
func (fs *fileStore) StoreRawMsg(subj string, hdr, msg []byte, seq uint64, ts int64) error {
	fs.mu.Lock()
	err := fs.storeRawMsg(subj, hdr, msg, seq, ts)
	cb := fs.scb
	// Check if first message timestamp requires expiry
	// sooner than initial replica expiry timer set to MaxAge when initializing.
	if !fs.receivedAny && fs.cfg.MaxAge != 0 && ts > 0 {
		fs.receivedAny = true
		// don't block here by calling expireMsgs directly.
		// Instead, set short timeout.
		fs.resetAgeChk(int64(time.Millisecond * 50))
	}
	fs.mu.Unlock()

	if err == nil && cb != nil {
		cb(1, int64(fileStoreMsgSize(subj, hdr, msg)), seq, subj)
	}

	return err
}

// Store stores a message. We hold the main filestore lock for any write operation.
func (fs *fileStore) StoreMsg(subj string, hdr, msg []byte) (uint64, int64, error) {
	fs.mu.Lock()
	seq, ts := fs.state.LastSeq+1, time.Now().UnixNano()
	err := fs.storeRawMsg(subj, hdr, msg, seq, ts)
	cb := fs.scb
	fs.mu.Unlock()

	if err != nil {
		seq, ts = 0, 0
	} else if cb != nil {
		cb(1, int64(fileStoreMsgSize(subj, hdr, msg)), seq, subj)
	}

	return seq, ts, err
}

// skipMsg will update this message block for a skipped message.
// If we do not have any messages, just update the metadata, otherwise
// we will place and empty record marking the sequence as used. The
// sequence will be marked erased.
// fs lock should be held.
func (mb *msgBlock) skipMsg(seq uint64, now time.Time) {
	if mb == nil {
		return
	}
	var needsRecord bool

	nowts := now.UnixNano()

	mb.mu.Lock()
	// If we are empty can just do meta.
	if mb.msgs == 0 {
		mb.last.seq = seq
		mb.last.ts = nowts
		mb.first.seq = seq + 1
		mb.first.ts = nowts
		// Take care of index if needed.
		if nowts-mb.lwits > wiThresh {
			mb.writeIndexInfoLocked()
		}
	} else {
		needsRecord = true
		if mb.dmap == nil {
			mb.dmap = make(map[uint64]struct{})
		}
		mb.dmap[seq] = struct{}{}
	}
	mb.mu.Unlock()

	if needsRecord {
		mb.writeMsgRecord(emptyRecordLen, seq|ebit, _EMPTY_, nil, nil, nowts, true)
	} else {
		mb.kickFlusher()
	}
}

// SkipMsg will use the next sequence number but not store anything.
func (fs *fileStore) SkipMsg() uint64 {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Grab time and last seq.
	now, seq := time.Now().UTC(), fs.state.LastSeq+1
	fs.state.LastSeq, fs.state.LastTime = seq, now
	if fs.state.Msgs == 0 {
		fs.state.FirstSeq, fs.state.FirstTime = seq, now
	}
	if seq == fs.state.FirstSeq {
		fs.state.FirstSeq, fs.state.FirstTime = seq+1, now
	}
	fs.lmb.skipMsg(seq, now)

	return seq
}

// Lock should be held.
func (fs *fileStore) rebuildFirst() {
	if len(fs.blks) == 0 {
		return
	}
	if fmb := fs.blks[0]; fmb != nil {
		fmb.removeIndexFile()
		fmb.rebuildState()
		fmb.writeIndexInfo()
		fs.selectNextFirst()
	}
}

// Optimized helper function to return first sequence.
// subj will always be publish subject here, meaning non-wildcard.
// We assume a fast check that this subj even exists already happened.
// Lock should be held.
func (fs *fileStore) firstSeqForSubj(subj string) (uint64, error) {
	if len(fs.blks) == 0 {
		return 0, nil
	}

	// See if we can optimize where we start.
	start, stop := fs.blks[0].index, fs.lmb.index
	if info, ok := fs.psim[subj]; ok {
		start, stop = info.fblk, info.lblk
	}

	for i := start; i <= stop; i++ {
		mb := fs.bim[i]
		if mb == nil {
			continue
		}
		mb.mu.Lock()
		if err := mb.ensurePerSubjectInfoLoaded(); err != nil {
			mb.mu.Unlock()
			return 0, err
		}
		ss := mb.fss[subj]
		mb.mu.Unlock()
		if ss != nil {
			// Adjust first if it was not where we thought it should be.
			if i != start {
				if info, ok := fs.psim[subj]; ok {
					info.fblk = i
				}
			}
			return ss.First, nil
		}
	}
	return 0, nil
}

// Will check the msg limit and drop firstSeq msg if needed.
// Lock should be held.
func (fs *fileStore) enforceMsgLimit() {
	if fs.cfg.MaxMsgs <= 0 || fs.state.Msgs <= uint64(fs.cfg.MaxMsgs) {
		return
	}
	for nmsgs := fs.state.Msgs; nmsgs > uint64(fs.cfg.MaxMsgs); nmsgs = fs.state.Msgs {
		if removed, err := fs.deleteFirstMsg(); err != nil || !removed {
			fs.rebuildFirst()
			return
		}
	}
}

// Will check the bytes limit and drop msgs if needed.
// Lock should be held.
func (fs *fileStore) enforceBytesLimit() {
	if fs.cfg.MaxBytes <= 0 || fs.state.Bytes <= uint64(fs.cfg.MaxBytes) {
		return
	}
	for bs := fs.state.Bytes; bs > uint64(fs.cfg.MaxBytes); bs = fs.state.Bytes {
		if removed, err := fs.deleteFirstMsg(); err != nil || !removed {
			fs.rebuildFirst()
			return
		}
	}
}

// Will make sure we have limits honored for max msgs per subject on recovery or config update.
// We will make sure to go through all msg blocks etc. but in practice this
// will most likely only be the last one, so can take a more conservative approach.
// Lock should be held.
func (fs *fileStore) enforceMsgPerSubjectLimit() {
	maxMsgsPer := uint64(fs.cfg.MaxMsgsPer)

	// We want to suppress callbacks from remove during this process
	// since these should have already been deleted and accounted for.
	cb := fs.scb
	fs.scb = nil
	defer func() { fs.scb = cb }()

	// collect all that are not correct.
	needAttention := make(map[string]*psi)
	for subj, psi := range fs.psim {
		if psi.total > maxMsgsPer {
			needAttention[subj] = psi
		}
	}

	// Collect all the msgBlks we alter.
	blks := make(map[*msgBlock]struct{})

	// For re-use below.
	var sm StoreMsg

	// Walk all subjects that need attention here.
	for subj, info := range needAttention {
		total, start, stop := info.total, info.fblk, info.lblk

		for i := start; i <= stop; i++ {
			mb := fs.bim[i]
			if mb == nil {
				continue
			}
			// Grab the ss entry for this subject in case sparse.
			mb.mu.Lock()
			mb.ensurePerSubjectInfoLoaded()
			ss := mb.fss[subj]
			mb.mu.Unlock()
			if ss == nil {
				continue
			}
			for seq := ss.First; seq <= ss.Last && total > maxMsgsPer; {
				m, _, err := mb.firstMatching(subj, false, seq, &sm)
				if err == nil {
					seq = m.seq + 1
					if removed, _ := fs.removeMsgViaLimits(m.seq); removed {
						total--
						blks[mb] = struct{}{}
					}
				} else {
					// On error just do single increment.
					seq++
				}
			}
		}
	}

	// Now write updated index for all affected msgBlks.
	for mb := range blks {
		mb.writeIndexInfo()
		mb.tryForceExpireCacheLocked()
	}
}

// Lock should be held.
func (fs *fileStore) deleteFirstMsg() (bool, error) {
	return fs.removeMsgViaLimits(fs.state.FirstSeq)
}

// If we remove via limits that can always be recovered on a restart we
// do not force the system to update the index file.
// Lock should be held.
func (fs *fileStore) removeMsgViaLimits(seq uint64) (bool, error) {
	return fs.removeMsg(seq, false, true, false)
}

// RemoveMsg will remove the message from this store.
// Will return the number of bytes removed.
func (fs *fileStore) RemoveMsg(seq uint64) (bool, error) {
	return fs.removeMsg(seq, false, false, true)
}

func (fs *fileStore) EraseMsg(seq uint64) (bool, error) {
	return fs.removeMsg(seq, true, false, true)
}

// Convenience function to remove per subject tracking at the filestore level.
// Lock should be held.
func (fs *fileStore) removePerSubject(subj string) {
	if len(subj) == 0 {
		return
	}

	// We do not update sense of fblk here but will do so when we resolve during lookup.
	if info, ok := fs.psim[subj]; ok {
		info.total--
		if info.total == 0 {
			delete(fs.psim, subj)
		}
	}
}

// Remove a message, optionally rewriting the mb file.
func (fs *fileStore) removeMsg(seq uint64, secure, viaLimits, needFSLock bool) (bool, error) {
	if seq == 0 {
		return false, ErrStoreMsgNotFound
	}
	fsLock := func() {
		if needFSLock {
			fs.mu.Lock()
		}
	}
	fsUnlock := func() {
		if needFSLock {
			fs.mu.Unlock()
		}
	}

	fsLock()

	if fs.closed {
		fsUnlock()
		return false, ErrStoreClosed
	}
	if fs.sips > 0 {
		fsUnlock()
		return false, ErrStoreSnapshotInProgress
	}
	// If in encrypted mode negate secure rewrite here.
	if secure && fs.prf != nil {
		secure = false
	}

	if fs.state.Msgs == 0 {
		var err = ErrStoreEOF
		if seq <= fs.state.LastSeq {
			err = ErrStoreMsgNotFound
		}
		fsUnlock()
		return false, err
	}

	mb := fs.selectMsgBlock(seq)
	if mb == nil {
		var err = ErrStoreEOF
		if seq <= fs.state.LastSeq {
			err = ErrStoreMsgNotFound
		}
		fsUnlock()
		return false, err
	}

	mb.mu.Lock()

	// See if we are closed or the sequence number is still relevant.
	if mb.closed || seq < mb.first.seq {
		mb.mu.Unlock()
		fsUnlock()
		return false, nil
	}

	// Now check dmap if it is there.
	if mb.dmap != nil {
		if _, ok := mb.dmap[seq]; ok {
			mb.mu.Unlock()
			fsUnlock()
			return false, nil
		}
	}

	// We used to not have to load in the messages except with callbacks or the filtered subject state (which is now always on).
	// Now just load regardless.
	// TODO(dlc) - Figure out a way not to have to load it in, we need subject tracking outside main data block.
	if mb.cacheNotLoaded() {
		if err := mb.loadMsgsWithLock(); err != nil {
			mb.mu.Unlock()
			fsUnlock()
			return false, err
		}
	}

	var smv StoreMsg
	sm, err := mb.cacheLookup(seq, &smv)
	if err != nil {
		mb.mu.Unlock()
		fsUnlock()
		return false, err
	}
	// Grab size
	msz := fileStoreMsgSize(sm.subj, sm.hdr, sm.msg)

	// Set cache timestamp for last remove.
	mb.lrts = time.Now().UnixNano()

	// Global stats
	fs.state.Msgs--
	fs.state.Bytes -= msz

	// Now local mb updates.
	mb.msgs--
	mb.bytes -= msz

	// If we are tracking subjects here make sure we update that accounting.
	mb.ensurePerSubjectInfoLoaded()

	// If we are tracking multiple subjects here make sure we update that accounting.
	mb.removeSeqPerSubject(sm.subj, seq, &smv)
	fs.removePerSubject(sm.subj)

	if secure {
		// Grab record info.
		ri, rl, _, _ := mb.slotInfo(int(seq - mb.cache.fseq))
		mb.eraseMsg(seq, int(ri), int(rl))
	}

	fifo := seq == mb.first.seq
	isLastBlock := mb == fs.lmb
	isEmpty := mb.msgs == 0
	// If we are removing the message via limits we do not need to write the index file here.
	// If viaLimits this means on a restart we will properly cleanup these messages regardless.
	shouldWriteIndex := !isEmpty && !viaLimits

	if fifo {
		mb.selectNextFirst()
		if !isEmpty {
			// Can update this one in place.
			if seq == fs.state.FirstSeq {
				fs.state.FirstSeq = mb.first.seq // new one.
				fs.state.FirstTime = time.Unix(0, mb.first.ts).UTC()
			}
		}
	} else if !isEmpty {
		// Out of order delete.
		if mb.dmap == nil {
			mb.dmap = make(map[uint64]struct{})
		}
		mb.dmap[seq] = struct{}{}
		// Check if <25% utilization and minimum size met.
		if mb.rbytes > compactMinimum && !isLastBlock {
			// Remove the interior delete records
			rbytes := mb.rbytes - uint64(len(mb.dmap)*emptyRecordLen)
			if rbytes>>2 > mb.bytes {
				mb.compact()
			}
		}
	}

	var firstSeqNeedsUpdate bool

	// Decide how we want to clean this up. If last block and the only block left we will hold into index.
	if isEmpty {
		if isLastBlock {
			mb.closeAndKeepIndex(viaLimits)
			// We do not need to writeIndex since just did above.
			shouldWriteIndex = false
		} else {
			fs.removeMsgBlock(mb)
		}
		firstSeqNeedsUpdate = seq == fs.state.FirstSeq
	}

	var qch, fch chan struct{}
	if shouldWriteIndex {
		qch, fch = mb.qch, mb.fch
	}
	cb := fs.scb

	if secure {
		if ld, _ := mb.flushPendingMsgsLocked(); ld != nil {
			// We have the mb lock here, this needs the mb locks so do in its own go routine.
			go fs.rebuildState(ld)
		}
	}
	// Check if we need to write the index file and we are flush in place (fip).
	if shouldWriteIndex && fs.fip {
		// Check if this is the first message, common during expirations etc.
		threshold := wiThresh
		if !fifo {
			// For out-of-order deletes, we will have a shorter threshold, but
			// still won't write the index for every single delete.
			threshold = winfThresh
		}
		if time.Now().UnixNano()-mb.lwits > threshold {
			mb.writeIndexInfoLocked()
		}
	}
	mb.mu.Unlock()

	// Kick outside of lock.
	if !fs.fip && shouldWriteIndex {
		if qch == nil {
			mb.spinUpFlushLoop()
		}
		select {
		case fch <- struct{}{}:
		default:
		}
	}

	// If we emptied the current message block and the seq was state.First.Seq
	// then we need to jump message blocks. We will also write the index so
	// we don't lose track of the first sequence.
	if firstSeqNeedsUpdate {
		fs.selectNextFirst()
		// Write out the new first message block if we have one.
		// We can ignore if we really have not changed message blocks from above.
		if len(fs.blks) > 0 && fs.blks[0] != mb {
			fmb := fs.blks[0]
			fmb.writeIndexInfo()
		}
	}
	fs.mu.Unlock()

	// Storage updates.
	if cb != nil {
		subj := _EMPTY_
		if sm != nil {
			subj = sm.subj
		}
		delta := int64(msz)
		cb(-1, -delta, seq, subj)
	}

	if !needFSLock {
		fs.mu.Lock()
	}

	return true, nil
}

// This will compact and rewrite this block. This should only be called when we know we want to rewrite this block.
// This should not be called on the lmb since we will prune tail deleted messages which could cause issues with
// writing new messages. We will silently bail on any issues with the underlying block and let someone else detect.
// Write lock needs to be held.
func (mb *msgBlock) compact() {
	wasLoaded := mb.cacheAlreadyLoaded()
	if !wasLoaded {
		if err := mb.loadMsgsWithLock(); err != nil {
			return
		}
	}

	buf := mb.cache.buf
	nbuf := make([]byte, 0, len(buf))

	var le = binary.LittleEndian
	var firstSet bool

	isDeleted := func(seq uint64) bool {
		if seq == 0 || seq&ebit != 0 || seq < mb.first.seq {
			return true
		}
		var deleted bool
		if mb.dmap != nil {
			_, deleted = mb.dmap[seq]
		}
		return deleted
	}

	// For skip msgs.
	var smh [msgHdrSize]byte

	for index, lbuf := uint32(0), uint32(len(buf)); index < lbuf; {
		if index+msgHdrSize > lbuf {
			return
		}
		hdr := buf[index : index+msgHdrSize]
		rl, slen := le.Uint32(hdr[0:]), le.Uint16(hdr[20:])
		// Clear any headers bit that could be set.
		rl &^= hbit
		dlen := int(rl) - msgHdrSize
		// Do some quick sanity checks here.
		if dlen < 0 || int(slen) > dlen || dlen > int(rl) || rl > rlBadThresh || index+rl > lbuf {
			return
		}
		// Only need to process non-deleted messages.
		seq := le.Uint64(hdr[4:])
		if !isDeleted(seq) {
			// Normal message here.
			nbuf = append(nbuf, buf[index:index+rl]...)
			if !firstSet {
				firstSet = true
				mb.first.seq = seq
			}
		} else if firstSet {
			// This is an interior delete that we need to make sure we have a placeholder for.
			le.PutUint32(smh[0:], emptyRecordLen)
			le.PutUint64(smh[4:], seq|ebit)
			le.PutUint64(smh[12:], 0)
			le.PutUint16(smh[20:], 0)
			nbuf = append(nbuf, smh[:]...)
			mb.hh.Reset()
			mb.hh.Write(smh[4:20])
			checksum := mb.hh.Sum(nil)
			nbuf = append(nbuf, checksum...)
		}
		// Always set last.
		mb.last.seq = seq &^ ebit

		// Advance to next record.
		index += rl
	}

	// Check for encryption.
	if mb.bek != nil && len(nbuf) > 0 {
		// Recreate to reset counter.
		rbek, err := genBlockEncryptionKey(mb.fs.fcfg.Cipher, mb.seed, mb.nonce)
		if err != nil {
			return
		}
		rbek.XORKeyStream(nbuf, nbuf)
	}

	// Close FDs first.
	mb.closeFDsLocked()

	// We will write to a new file and mv/rename it in case of failure.
	mfn := filepath.Join(filepath.Join(mb.fs.fcfg.StoreDir, msgDir), fmt.Sprintf(newScan, mb.index))
	if err := os.WriteFile(mfn, nbuf, defaultFilePerms); err != nil {
		os.Remove(mfn)
		return
	}
	if err := os.Rename(mfn, mb.mfn); err != nil {
		os.Remove(mfn)
		return
	}

	// Close cache and index file and wipe delete map, then rebuild.
	mb.clearCacheAndOffset()
	mb.removeIndexFileLocked()
	mb.deleteDmap()
	mb.rebuildStateLocked()

	// If we entered with the msgs loaded make sure to reload them.
	if wasLoaded {
		mb.loadMsgsWithLock()
	}
}

// Nil out our dmap.
func (mb *msgBlock) deleteDmap() {
	mb.dmap = nil
}

// Grab info from a slot.
// Lock should be held.
func (mb *msgBlock) slotInfo(slot int) (uint32, uint32, bool, error) {
	if mb.cache == nil || slot >= len(mb.cache.idx) {
		return 0, 0, false, errPartialCache
	}

	bi := mb.cache.idx[slot]
	ri, hashChecked := (bi &^ hbit), (bi&hbit) != 0

	// Determine record length
	var rl uint32
	if len(mb.cache.idx) > slot+1 {
		ni := mb.cache.idx[slot+1] &^ hbit
		rl = ni - ri
	} else {
		rl = mb.cache.lrl
	}
	if rl < msgHdrSize {
		return 0, 0, false, errBadMsg
	}
	return uint32(ri), rl, hashChecked, nil
}

func (fs *fileStore) isClosed() bool {
	fs.mu.RLock()
	closed := fs.closed
	fs.mu.RUnlock()
	return closed
}

// Will spin up our flush loop.
func (mb *msgBlock) spinUpFlushLoop() {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	// Are we already running?
	if mb.flusher {
		return
	}
	mb.flusher = true
	mb.fch = make(chan struct{}, 1)
	mb.qch = make(chan struct{})
	fch, qch := mb.fch, mb.qch

	go mb.flushLoop(fch, qch)
}

// Raw low level kicker for flush loops.
func kickFlusher(fch chan struct{}) {
	if fch != nil {
		select {
		case fch <- struct{}{}:
		default:
		}
	}
}

// Kick flusher for this message block.
func (mb *msgBlock) kickFlusher() {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	kickFlusher(mb.fch)
}

func (mb *msgBlock) setInFlusher() {
	mb.mu.Lock()
	mb.flusher = true
	mb.mu.Unlock()
}

func (mb *msgBlock) clearInFlusher() {
	mb.mu.Lock()
	mb.flusher = false
	mb.mu.Unlock()
}

// flushLoop watches for messages, index info, or recently closed msg block updates.
func (mb *msgBlock) flushLoop(fch, qch chan struct{}) {
	mb.setInFlusher()
	defer mb.clearInFlusher()

	// Will use to test if we have meta data updates.
	var firstSeq, lastSeq uint64
	var dmapLen int

	infoChanged := func() bool {
		mb.mu.RLock()
		defer mb.mu.RUnlock()
		var changed bool
		if firstSeq != mb.first.seq || lastSeq != mb.last.seq || dmapLen != len(mb.dmap) {
			changed = true
			firstSeq, lastSeq = mb.first.seq, mb.last.seq
			dmapLen = len(mb.dmap)
		}
		return changed
	}

	for {
		select {
		case <-fch:
			// If we have pending messages process them first.
			if waiting := mb.pendingWriteSize(); waiting != 0 {
				ts := 1 * time.Millisecond
				var waited time.Duration

				for waiting < coalesceMinimum {
					time.Sleep(ts)
					select {
					case <-qch:
						return
					default:
					}
					newWaiting := mb.pendingWriteSize()
					if waited = waited + ts; waited > maxFlushWait || newWaiting <= waiting {
						break
					}
					waiting = newWaiting
					ts *= 2
				}
				mb.flushPendingMsgs()
				// Check if we are no longer the last message block. If we are
				// not we can close FDs and exit.
				mb.fs.mu.RLock()
				notLast := mb != mb.fs.lmb
				mb.fs.mu.RUnlock()
				if notLast {
					if err := mb.closeFDs(); err == nil {
						return
					}
				}
			}
			if infoChanged() {
				mb.writeIndexInfo()
			}
		case <-qch:
			return
		}
	}
}

// Lock should be held.
func (mb *msgBlock) eraseMsg(seq uint64, ri, rl int) error {
	var le = binary.LittleEndian
	var hdr [msgHdrSize]byte

	le.PutUint32(hdr[0:], uint32(rl))
	le.PutUint64(hdr[4:], seq|ebit)
	le.PutUint64(hdr[12:], 0)
	le.PutUint16(hdr[20:], 0)

	// Randomize record
	data := make([]byte, rl-emptyRecordLen)
	mrand.Read(data)

	// Now write to underlying buffer.
	var b bytes.Buffer
	b.Write(hdr[:])
	b.Write(data)

	// Calculate hash.
	mb.hh.Reset()
	mb.hh.Write(hdr[4:20])
	mb.hh.Write(data)
	checksum := mb.hh.Sum(nil)
	// Write to msg record.
	b.Write(checksum)

	// Update both cache and disk.
	nbytes := b.Bytes()

	// Cache
	if ri >= mb.cache.off {
		li := ri - mb.cache.off
		buf := mb.cache.buf[li : li+rl]
		copy(buf, nbytes)
	}

	// Disk
	if mb.cache.off+mb.cache.wp > ri {
		mfd, err := os.OpenFile(mb.mfn, os.O_RDWR, defaultFilePerms)
		if err != nil {
			return err
		}
		defer mfd.Close()
		if _, err = mfd.WriteAt(nbytes, int64(ri)); err == nil {
			mfd.Sync()
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// Truncate this message block to the storedMsg.
func (mb *msgBlock) truncate(sm *StoreMsg) (nmsgs, nbytes uint64, err error) {
	// Make sure we are loaded to process messages etc.
	if err := mb.loadMsgs(); err != nil {
		return 0, 0, err
	}

	// Calculate new eof using slot info from our new last sm.
	ri, rl, _, err := mb.slotInfo(int(sm.seq - mb.cache.fseq))
	if err != nil {
		return 0, 0, err
	}
	// Calculate new eof.
	eof := int64(ri + rl)

	var purged, bytes uint64

	mb.mu.Lock()

	checkDmap := len(mb.dmap) > 0
	var smv StoreMsg

	for seq := mb.last.seq; seq > sm.seq; seq-- {
		if checkDmap {
			if _, ok := mb.dmap[seq]; ok {
				// Delete and skip to next.
				delete(mb.dmap, seq)
				if len(mb.dmap) == 0 {
					mb.dmap = nil
					checkDmap = false
				}
				continue
			}
		}
		// We should have a valid msg to calculate removal stats.
		if m, err := mb.cacheLookup(seq, &smv); err == nil {
			if mb.msgs > 0 {
				rl := fileStoreMsgSize(m.subj, m.hdr, m.msg)
				mb.msgs--
				mb.bytes -= rl
				mb.rbytes -= rl
				// For return accounting.
				purged++
				bytes += uint64(rl)
			}
		}
	}

	// Truncate our msgs and close file.
	if mb.mfd != nil {
		mb.mfd.Truncate(eof)
		mb.mfd.Sync()
		// Update our checksum.
		var lchk [8]byte
		mb.mfd.ReadAt(lchk[:], eof-8)
		copy(mb.lchk[0:], lchk[:])
	} else {
		mb.mu.Unlock()
		return 0, 0, fmt.Errorf("failed to truncate msg block %d, file not open", mb.index)
	}

	// Update our last msg.
	mb.last.seq = sm.seq
	mb.last.ts = sm.ts

	// Clear our cache.
	mb.clearCacheAndOffset()

	// Redo per subject info for this block.
	mb.resetPerSubjectInfo()

	mb.mu.Unlock()

	// Write our index file.
	mb.writeIndexInfo()
	// Load msgs again.
	mb.loadMsgs()

	return purged, bytes, nil
}

// Lock should be held.
func (mb *msgBlock) isEmpty() bool {
	return mb.first.seq > mb.last.seq
}

// Lock should be held.
func (mb *msgBlock) selectNextFirst() {
	var seq uint64
	for seq = mb.first.seq + 1; seq <= mb.last.seq; seq++ {
		if _, ok := mb.dmap[seq]; ok {
			// We will move past this so we can delete the entry.
			delete(mb.dmap, seq)
		} else {
			break
		}
	}
	// Set new first sequence.
	mb.first.seq = seq

	// Check if we are empty..
	if mb.isEmpty() {
		mb.first.ts = 0
		return
	}

	// Need to get the timestamp.
	// We will try the cache direct and fallback if needed.
	var smv StoreMsg
	sm, _ := mb.cacheLookup(seq, &smv)
	if sm == nil {
		// Slow path, need to unlock.
		mb.mu.Unlock()
		sm, _, _ = mb.fetchMsg(seq, &smv)
		mb.mu.Lock()
	}
	if sm != nil {
		mb.first.ts = sm.ts
	} else {
		mb.first.ts = 0
	}
}

// Select the next FirstSeq
// Lock should be held.
func (fs *fileStore) selectNextFirst() {
	if len(fs.blks) > 0 {
		mb := fs.blks[0]
		mb.mu.RLock()
		fs.state.FirstSeq = mb.first.seq
		fs.state.FirstTime = time.Unix(0, mb.first.ts).UTC()
		mb.mu.RUnlock()
	} else {
		// Could not find anything, so treat like purge
		fs.state.FirstSeq = fs.state.LastSeq + 1
		fs.state.FirstTime = time.Time{}
	}
}

// Lock should be held.
func (mb *msgBlock) resetCacheExpireTimer(td time.Duration) {
	if td == 0 {
		td = mb.cexp
	}
	if mb.ctmr == nil {
		mb.ctmr = time.AfterFunc(td, mb.expireCache)
	} else {
		mb.ctmr.Reset(td)
	}
}

// Lock should be held.
func (mb *msgBlock) startCacheExpireTimer() {
	mb.resetCacheExpireTimer(0)
}

// Used when we load in a message block.
// Lock should be held.
func (mb *msgBlock) clearCacheAndOffset() {
	// Reset linear scan tracker.
	mb.llseq = 0
	if mb.cache != nil {
		mb.cache.off = 0
		mb.cache.wp = 0
	}
	mb.clearCache()
}

// Lock should be held.
func (mb *msgBlock) clearCache() {
	if mb.ctmr != nil && mb.fss == nil {
		mb.ctmr.Stop()
		mb.ctmr = nil
	}

	if mb.cache == nil {
		return
	}

	buf := mb.cache.buf
	if mb.cache.off == 0 {
		mb.cache = nil
	} else {
		// Clear msgs and index.
		mb.cache.buf = nil
		mb.cache.idx = nil
		mb.cache.wp = 0
	}
	recycleMsgBlockBuf(buf)
}

// Called to possibly expire a message block cache.
func (mb *msgBlock) expireCache() {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	mb.expireCacheLocked()
}

func (mb *msgBlock) tryForceExpireCache() {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	mb.tryForceExpireCacheLocked()
}

// We will attempt to force expire this by temporarily clearing the last load time.
func (mb *msgBlock) tryForceExpireCacheLocked() {
	llts := mb.llts
	mb.llts = 0
	mb.expireCacheLocked()
	mb.llts = llts
}

// This is for expiration of the write cache, which will be partial with fip.
// So we want to bypass the Pools here.
// Lock should be held.
func (mb *msgBlock) tryExpireWriteCache() []byte {
	if mb.cache == nil {
		return nil
	}
	lwts, buf, llts, nra := mb.lwts, mb.cache.buf, mb.llts, mb.cache.nra
	mb.lwts, mb.cache.nra = 0, true
	mb.expireCacheLocked()
	mb.lwts = lwts
	if mb.cache != nil {
		mb.cache.nra = nra
	}
	// We could check for a certain time since last load, but to be safe just reuse if no loads at all.
	if llts == 0 && (mb.cache == nil || mb.cache.buf == nil) {
		// Clear last write time since we now are about to move on to a new lmb.
		mb.lwts = 0
		return buf[:0]
	}
	return nil
}

// Lock should be held.
func (mb *msgBlock) expireCacheLocked() {
	if mb.cache == nil && mb.fss == nil {
		if mb.ctmr != nil {
			mb.ctmr.Stop()
			mb.ctmr = nil
		}
		return
	}

	// Can't expire if we still have pending.
	if mb.cache != nil && len(mb.cache.buf)-int(mb.cache.wp) > 0 {
		mb.resetCacheExpireTimer(mb.cexp)
		return
	}

	// Grab timestamp to compare.
	tns := time.Now().UnixNano()

	// For the core buffer of messages, we care about reads and writes, but not removes.
	bufts := mb.llts
	if mb.lwts > bufts {
		bufts = mb.lwts
	}

	// Check for activity on the cache that would prevent us from expiring.
	if tns-bufts <= int64(mb.cexp) {
		mb.resetCacheExpireTimer(mb.cexp - time.Duration(tns-bufts))
		return
	}

	// If we are here we will at least expire the core msg buffer.
	// We need to capture offset in case we do a write next before a full load.
	if mb.cache != nil {
		mb.cache.off += len(mb.cache.buf)
		if !mb.cache.nra {
			recycleMsgBlockBuf(mb.cache.buf)
		}
		mb.cache.buf = nil
		mb.cache.wp = 0
	}

	// Check if we can clear out our fss and idx unless under force expire.
	// We used to hold onto the idx longer but removes need buf now so no point.
	mb.writePerSubjectInfo()
	mb.fss = nil
	if mb.indexNeedsUpdateLocked() {
		mb.writeIndexInfoLocked()
	}
	mb.clearCache()
}

func (fs *fileStore) startAgeChk() {
	if fs.ageChk == nil && fs.cfg.MaxAge != 0 {
		fs.ageChk = time.AfterFunc(fs.cfg.MaxAge, fs.expireMsgs)
	}
}

// Lock should be held.
func (fs *fileStore) resetAgeChk(delta int64) {
	if fs.cfg.MaxAge == 0 {
		return
	}

	fireIn := fs.cfg.MaxAge
	if delta > 0 && time.Duration(delta) < fireIn {
		fireIn = time.Duration(delta)
	}
	if fs.ageChk != nil {
		fs.ageChk.Reset(fireIn)
	} else {
		fs.ageChk = time.AfterFunc(fireIn, fs.expireMsgs)
	}
}

// Lock should be held.
func (fs *fileStore) cancelAgeChk() {
	if fs.ageChk != nil {
		fs.ageChk.Stop()
		fs.ageChk = nil
	}
}

// Will expire msgs that are too old.
func (fs *fileStore) expireMsgs() {
	// We need to delete one by one here and can not optimize for the time being.
	// Reason is that we need more information to adjust ack pending in consumers.
	var smv StoreMsg
	var sm *StoreMsg
	fs.mu.RLock()
	maxAge := int64(fs.cfg.MaxAge)
	minAge := time.Now().UnixNano() - maxAge
	fs.mu.RUnlock()

	for sm, _ = fs.msgForSeq(0, &smv); sm != nil && sm.ts <= minAge; sm, _ = fs.msgForSeq(0, &smv) {
		fs.mu.Lock()
		fs.removeMsgViaLimits(sm.seq)
		fs.mu.Unlock()
		// Recalculate in case we are expiring a bunch.
		minAge = time.Now().UnixNano() - maxAge
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Onky cancel if no message left, not on potential lookup error that would result in sm == nil.
	if fs.state.Msgs == 0 {
		fs.cancelAgeChk()
	} else {
		if sm == nil {
			fs.resetAgeChk(0)
		} else {
			fs.resetAgeChk(sm.ts - minAge)
		}
	}
}

// Lock should be held.
func (fs *fileStore) checkAndFlushAllBlocks() {
	for _, mb := range fs.blks {
		if mb.pendingWriteSize() > 0 {
			// Since fs lock is held need to pull this apart in case we need to rebuild state.
			mb.mu.Lock()
			ld, _ := mb.flushPendingMsgsLocked()
			mb.mu.Unlock()
			if ld != nil {
				fs.rebuildStateLocked(ld)
			}
		}
		if mb.indexNeedsUpdate() {
			mb.writeIndexInfo()
		}
	}
}

// This will check all the checksums on messages and report back any sequence numbers with errors.
func (fs *fileStore) checkMsgs() *LostStreamData {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	fs.checkAndFlushAllBlocks()

	// Clear any global subject state.
	fs.psim = make(map[string]*psi)

	for _, mb := range fs.blks {
		if ld, err := mb.rebuildState(); err != nil && ld != nil {
			// Rebuild fs state too.
			mb.fs.rebuildStateLocked(ld)
		}
		fs.populateGlobalPerSubjectInfo(mb)
	}

	return fs.ld
}

// Lock should be held.
func (mb *msgBlock) enableForWriting(fip bool) error {
	if mb == nil {
		return errNoMsgBlk
	}
	if mb.mfd != nil {
		return nil
	}
	mfd, err := os.OpenFile(mb.mfn, os.O_CREATE|os.O_RDWR, defaultFilePerms)
	if err != nil {
		return fmt.Errorf("error opening msg block file [%q]: %v", mb.mfn, err)
	}
	mb.mfd = mfd

	// Spin up our flusher loop if needed.
	if !fip {
		mb.spinUpFlushLoop()
	}

	return nil
}

// Will write the message record to the underlying message block.
// filestore lock will be held.
func (mb *msgBlock) writeMsgRecord(rl, seq uint64, subj string, mhdr, msg []byte, ts int64, flush bool) error {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	// Make sure we have a cache setup.
	if mb.cache == nil {
		mb.setupWriteCache(nil)
	}
	// Enable for writing if our mfd is not open.
	if mb.mfd == nil {
		if err := mb.enableForWriting(flush); err != nil {
			return err
		}
	}

	// Check if we are tracking per subject for our simple state.
	// Do this before changing the cache that would trigger a flush pending msgs call
	// if we needed to regenerate the per subject info.
	if len(subj) > 0 && !mb.noTrack {
		if err := mb.ensurePerSubjectInfoLoaded(); err != nil {
			return err
		}
		if ss := mb.fss[subj]; ss != nil {
			ss.Msgs++
			ss.Last = seq
		} else {
			mb.fss[subj] = &SimpleState{Msgs: 1, First: seq, Last: seq}
		}
		mb.fssNeedsWrite = true
	}

	// Indexing
	index := len(mb.cache.buf) + int(mb.cache.off)

	// Formats
	// Format with no header
	// total_len(4) sequence(8) timestamp(8) subj_len(2) subj msg hash(8)
	// With headers, high bit on total length will be set.
	// total_len(4) sequence(8) timestamp(8) subj_len(2) subj hdr_len(4) hdr msg hash(8)

	// First write header, etc.
	var le = binary.LittleEndian
	var hdr [msgHdrSize]byte

	l := uint32(rl)
	hasHeaders := len(mhdr) > 0
	if hasHeaders {
		l |= hbit
	}

	le.PutUint32(hdr[0:], l)
	le.PutUint64(hdr[4:], seq)
	le.PutUint64(hdr[12:], uint64(ts))
	le.PutUint16(hdr[20:], uint16(len(subj)))

	// Now write to underlying buffer.
	mb.cache.buf = append(mb.cache.buf, hdr[:]...)
	mb.cache.buf = append(mb.cache.buf, subj...)

	if hasHeaders {
		var hlen [4]byte
		le.PutUint32(hlen[0:], uint32(len(mhdr)))
		mb.cache.buf = append(mb.cache.buf, hlen[:]...)
		mb.cache.buf = append(mb.cache.buf, mhdr...)
	}
	mb.cache.buf = append(mb.cache.buf, msg...)

	// Calculate hash.
	mb.hh.Reset()
	mb.hh.Write(hdr[4:20])
	mb.hh.Write([]byte(subj))
	if hasHeaders {
		mb.hh.Write(mhdr)
	}
	mb.hh.Write(msg)
	checksum := mb.hh.Sum(nil)
	// Grab last checksum
	copy(mb.lchk[0:], checksum)

	// Update write through cache.
	// Write to msg record.
	mb.cache.buf = append(mb.cache.buf, checksum...)
	// Write index
	mb.cache.idx = append(mb.cache.idx, uint32(index)|hbit)
	mb.cache.lrl = uint32(rl)
	if mb.cache.fseq == 0 {
		mb.cache.fseq = seq
	}

	// Set cache timestamp for last store.
	mb.lwts = ts
	// Decide if we write index info if flushing in place.
	writeIndex := ts-mb.lwits > wiThresh

	// Accounting
	mb.updateAccounting(seq, ts, rl)

	fch, werr := mb.fch, mb.werr

	// If we should be flushing, or had a write error, do so here.
	if flush || werr != nil {
		ld, err := mb.flushPendingMsgsLocked()
		if ld != nil && mb.fs != nil {
			// We have the mb lock here, this needs the mb locks so do in its own go routine.
			go mb.fs.rebuildState(ld)
		}
		if err != nil {
			return err
		}
		if writeIndex {
			// If this fails still proceed on since the write above succeeded.
			// We can recover this condition.
			mb.writeIndexInfoLocked()
		}
	} else {
		// Kick the flusher here.
		kickFlusher(fch)
	}

	return nil
}

// How many bytes pending to be written for this message block.
func (mb *msgBlock) pendingWriteSize() int {
	if mb == nil {
		return 0
	}
	var pending int
	mb.mu.RLock()
	if mb.mfd != nil && mb.cache != nil {
		pending = len(mb.cache.buf) - int(mb.cache.wp)
	}
	mb.mu.RUnlock()
	return pending
}

// Try to close our FDs if we can.
func (mb *msgBlock) closeFDs() error {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	return mb.closeFDsLocked()
}

func (mb *msgBlock) closeFDsLocked() error {
	if buf, _ := mb.bytesPending(); len(buf) > 0 {
		return errPendingData
	}
	mb.closeFDsLockedNoCheck()
	return nil
}

func (mb *msgBlock) closeFDsLockedNoCheck() {
	if mb.mfd != nil {
		mb.mfd.Close()
		mb.mfd = nil
	}
	if mb.ifd != nil {
		mb.ifd.Close()
		mb.ifd = nil
	}
}

// bytesPending returns the buffer to be used for writing to the underlying file.
// This marks we are in flush and will return nil if asked again until cleared.
// Lock should be held.
func (mb *msgBlock) bytesPending() ([]byte, error) {
	if mb == nil || mb.mfd == nil {
		return nil, errNoPending
	}
	if mb.cache == nil {
		return nil, errNoCache
	}
	if len(mb.cache.buf) <= mb.cache.wp {
		return nil, errNoPending
	}
	buf := mb.cache.buf[mb.cache.wp:]
	if len(buf) == 0 {
		return nil, errNoPending
	}
	return buf, nil
}

// Returns the current blkSize including deleted msgs etc.
func (mb *msgBlock) blkSize() uint64 {
	mb.mu.RLock()
	nb := mb.rbytes
	mb.mu.RUnlock()
	return nb
}

// Update accounting on a write msg.
// Lock should be held.
func (mb *msgBlock) updateAccounting(seq uint64, ts int64, rl uint64) {
	isDeleted := seq&ebit != 0
	if isDeleted {
		seq = seq &^ ebit
	}

	if mb.first.seq == 0 || mb.first.ts == 0 {
		mb.first.seq = seq
		mb.first.ts = ts
	}
	// Need atomics here for selectMsgBlock speed.
	atomic.StoreUint64(&mb.last.seq, seq)
	mb.last.ts = ts
	mb.rbytes += rl
	// Only update this accounting if message is not a deleted message.
	if !isDeleted {
		mb.bytes += rl
		mb.msgs++
	}
}

// Lock should be held.
func (fs *fileStore) writeMsgRecord(seq uint64, ts int64, subj string, hdr, msg []byte) (uint64, error) {
	var err error

	// Get size for this message.
	rl := fileStoreMsgSize(subj, hdr, msg)
	if rl&hbit != 0 {
		return 0, ErrMsgTooLarge
	}
	// Grab our current last message block.
	mb := fs.lmb
	if mb == nil || mb.msgs > 0 && mb.blkSize()+rl > fs.fcfg.BlockSize {
		if mb, err = fs.newMsgBlockForWrite(); err != nil {
			return 0, err
		}
	}

	// Ask msg block to store in write through cache.
	err = mb.writeMsgRecord(rl, seq, subj, hdr, msg, ts, fs.fip)

	return rl, err
}

// Sync msg and index files as needed. This is called from a timer.
func (fs *fileStore) syncBlocks() {
	fs.mu.RLock()
	if fs.closed {
		fs.mu.RUnlock()
		return
	}
	blks := append([]*msgBlock(nil), fs.blks...)
	fs.mu.RUnlock()

	for _, mb := range blks {
		// Flush anything that may be pending.
		if mb.pendingWriteSize() > 0 {
			mb.flushPendingMsgs()
		}
		if mb.indexNeedsUpdate() {
			mb.writeIndexInfo()
		}
		// Do actual sync. Hold lock for consistency.
		mb.mu.Lock()
		if !mb.closed {
			if mb.mfd != nil {
				mb.mfd.Sync()
			}
			if mb.ifd != nil {
				mb.ifd.Truncate(mb.liwsz)
				mb.ifd.Sync()
			}
			// See if we can close FDs due to being idle.
			if mb.ifd != nil || mb.mfd != nil && mb.sinceLastWriteActivity() > closeFDsIdle {
				mb.dirtyCloseWithRemove(false)
			}
		}
		mb.mu.Unlock()
	}

	fs.mu.Lock()
	fs.syncTmr = time.AfterFunc(fs.fcfg.SyncInterval, fs.syncBlocks)
	fs.mu.Unlock()
}

// Select the message block where this message should be found.
// Return nil if not in the set.
// Read lock should be held.
func (fs *fileStore) selectMsgBlock(seq uint64) *msgBlock {
	_, mb := fs.selectMsgBlockWithIndex(seq)
	return mb
}

func (fs *fileStore) selectMsgBlockWithIndex(seq uint64) (int, *msgBlock) {
	// Check for out of range.
	if seq < fs.state.FirstSeq || seq > fs.state.LastSeq {
		return -1, nil
	}

	// Starting index, defaults to beginning.
	si := 0

	// TODO(dlc) - Use new AVL and make this real for anything beyond certain size.
	// Max threshold before we probe for a starting block to start our linear search.
	const maxl = 256
	if nb := len(fs.blks); nb > maxl {
		d := nb / 8
		for _, i := range []int{d, 2 * d, 3 * d, 4 * d, 5 * d, 6 * d, 7 * d} {
			mb := fs.blks[i]
			if seq <= atomic.LoadUint64(&mb.last.seq) {
				break
			}
			si = i
		}
	}

	// blks are sorted in ascending order.
	for i := si; i < len(fs.blks); i++ {
		mb := fs.blks[i]
		if seq <= atomic.LoadUint64(&mb.last.seq) {
			return i, mb
		}
	}

	return -1, nil
}

// Select the message block where this message should be found.
// Return nil if not in the set.
func (fs *fileStore) selectMsgBlockForStart(minTime time.Time) *msgBlock {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	t := minTime.UnixNano()
	for _, mb := range fs.blks {
		mb.mu.RLock()
		found := t <= mb.last.ts
		mb.mu.RUnlock()
		if found {
			return mb
		}
	}
	return nil
}

// Index a raw msg buffer.
// Lock should be held.
func (mb *msgBlock) indexCacheBuf(buf []byte) error {
	var le = binary.LittleEndian

	var fseq uint64
	var idx []uint32
	var index uint32

	if mb.cache == nil {
		// Approximation, may adjust below.
		fseq = mb.first.seq
		idx = make([]uint32, 0, mb.msgs)
		mb.cache = &cache{}
	} else {
		fseq = mb.cache.fseq
		idx = mb.cache.idx
		if len(idx) == 0 {
			idx = make([]uint32, 0, mb.msgs)
		}
		index = uint32(len(mb.cache.buf))
		buf = append(mb.cache.buf, buf...)
	}

	lbuf := uint32(len(buf))

	for index < lbuf {
		if index+msgHdrSize > lbuf {
			return errCorruptState
		}
		hdr := buf[index : index+msgHdrSize]
		rl, seq, slen := le.Uint32(hdr[0:]), le.Uint64(hdr[4:]), le.Uint16(hdr[20:])

		// Clear any headers bit that could be set.
		rl &^= hbit
		dlen := int(rl) - msgHdrSize

		// Do some quick sanity checks here.
		if dlen < 0 || int(slen) > dlen || dlen > int(rl) || index+rl > lbuf || rl > 32*1024*1024 {
			// This means something is off.
			// TODO(dlc) - Add into bad list?
			return errCorruptState
		}

		// Clear erase bit.
		seq = seq &^ ebit
		// Adjust if we guessed wrong.
		if seq != 0 && seq < fseq {
			fseq = seq
		}
		// We defer checksum checks to individual msg cache lookups to amortorize costs and
		// not introduce latency for first message from a newly loaded block.
		idx = append(idx, index)
		mb.cache.lrl = uint32(rl)
		index += mb.cache.lrl
	}
	mb.cache.buf = buf
	mb.cache.idx = idx
	mb.cache.fseq = fseq
	mb.cache.wp += int(lbuf)

	return nil
}

// flushPendingMsgs writes out any messages for this message block.
func (mb *msgBlock) flushPendingMsgs() error {
	mb.mu.Lock()
	fsLostData, err := mb.flushPendingMsgsLocked()
	fs := mb.fs
	mb.mu.Unlock()

	// Signals us that we need to rebuild filestore state.
	if fsLostData != nil && fs != nil {
		// Rebuild fs state too.
		fs.rebuildState(fsLostData)
	}
	return err
}

// Write function for actual data.
// mb.mfd should not be nil.
// Lock should held.
func (mb *msgBlock) writeAt(buf []byte, woff int64) (int, error) {
	// Used to mock write failures.
	if mb.mockWriteErr {
		// Reset on trip.
		mb.mockWriteErr = false
		return 0, errors.New("mock write error")
	}
	return mb.mfd.WriteAt(buf, woff)
}

// flushPendingMsgsLocked writes out any messages for this message block.
// Lock should be held.
func (mb *msgBlock) flushPendingMsgsLocked() (*LostStreamData, error) {
	// Signals us that we need to rebuild filestore state.
	var fsLostData *LostStreamData

	if mb.cache == nil || mb.mfd == nil {
		return nil, nil
	}

	buf, err := mb.bytesPending()
	// If we got an error back return here.
	if err != nil {
		// No pending data to be written is not an error.
		if err == errNoPending || err == errNoCache {
			err = nil
		}
		return nil, err
	}

	woff := int64(mb.cache.off + mb.cache.wp)
	lob := len(buf)

	// TODO(dlc) - Normally we would not hold the lock across I/O so we can improve performance.
	// We will hold to stabilize the code base, as we have had a few anomalies with partial cache errors
	// under heavy load.

	// Check if we need to encrypt.
	if mb.bek != nil && lob > 0 {
		// Need to leave original alone.
		var dst []byte
		if lob <= defaultLargeBlockSize {
			dst = getMsgBlockBuf(lob)[:lob]
		} else {
			dst = make([]byte, lob)
		}
		mb.bek.XORKeyStream(dst, buf)
		buf = dst
	}

	// Append new data to the message block file.
	for lbb := lob; lbb > 0; lbb = len(buf) {
		n, err := mb.writeAt(buf, woff)
		if err != nil {
			mb.removePerSubjectInfoLocked()
			mb.removeIndexFileLocked()
			mb.dirtyCloseWithRemove(false)
			fsLostData, _ := mb.rebuildStateLocked()
			mb.werr = err
			return fsLostData, err
		}
		// Update our write offset.
		woff += int64(n)
		// Partial write.
		if n != lbb {
			buf = buf[n:]
		} else {
			// Done.
			break
		}
	}

	// Clear any error.
	mb.werr = nil

	// Cache may be gone.
	if mb.cache == nil || mb.mfd == nil {
		return fsLostData, mb.werr
	}

	// Check for additional writes while we were writing to the disk.
	moreBytes := len(mb.cache.buf) - mb.cache.wp - lob

	// Decide what we want to do with the buffer in hand. If we have load interest
	// we will hold onto the whole thing, otherwise empty the buffer, possibly reusing it.
	if ts := time.Now().UnixNano(); ts < mb.llts || (ts-mb.llts) <= int64(mb.cexp) {
		mb.cache.wp += lob
	} else {
		if cap(mb.cache.buf) <= maxBufReuse {
			buf = mb.cache.buf[:0]
		} else {
			recycleMsgBlockBuf(mb.cache.buf)
			buf = nil
		}
		if moreBytes > 0 {
			nbuf := mb.cache.buf[len(mb.cache.buf)-moreBytes:]
			if moreBytes > (len(mb.cache.buf)/4*3) && cap(nbuf) <= maxBufReuse {
				buf = nbuf
			} else {
				buf = append(buf, nbuf...)
			}
		}
		// Update our cache offset.
		mb.cache.off = int(woff)
		// Reset write pointer.
		mb.cache.wp = 0
		// Place buffer back in the cache structure.
		mb.cache.buf = buf
		// Mark fseq to 0
		mb.cache.fseq = 0
	}

	return fsLostData, mb.werr
}

// Lock should be held.
func (mb *msgBlock) clearLoading() {
	mb.loading = false
}

// Will load msgs from disk.
func (mb *msgBlock) loadMsgs() error {
	// We hold the lock here the whole time by design.
	mb.mu.Lock()
	defer mb.mu.Unlock()
	return mb.loadMsgsWithLock()
}

// Lock should be held.
func (mb *msgBlock) cacheAlreadyLoaded() bool {
	if mb.cache == nil || mb.cache.off != 0 || mb.cache.fseq == 0 || len(mb.cache.buf) == 0 {
		return false
	}
	numEntries := mb.msgs + uint64(len(mb.dmap)) + (mb.first.seq - mb.cache.fseq)
	return numEntries == uint64(len(mb.cache.idx))
}

// Lock should be held.
func (mb *msgBlock) cacheNotLoaded() bool {
	return !mb.cacheAlreadyLoaded()
}

// Used to load in the block contents.
// Lock should be held and all conditionals satisfied prior.
func (mb *msgBlock) loadBlock(buf []byte) ([]byte, error) {
	f, err := os.Open(mb.mfn)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var sz int
	if info, err := f.Stat(); err == nil {
		sz64 := info.Size()
		if int64(int(sz64)) == sz64 {
			sz = int(sz64)
		} else {
			return nil, errMsgBlkTooBig
		}
	}

	if buf == nil {
		buf = getMsgBlockBuf(sz)
		if sz > cap(buf) {
			// We know we will make a new one so just recycle for now.
			recycleMsgBlockBuf(buf)
			buf = nil
		}
	}

	if sz > cap(buf) {
		buf = make([]byte, sz)
	} else {
		buf = buf[:sz]
	}

	n, err := io.ReadFull(f, buf)
	return buf[:n], err
}

// Lock should be held.
func (mb *msgBlock) loadMsgsWithLock() error {
	// Check to see if we are loading already.
	if mb.loading {
		return nil
	}

	// Set loading status.
	mb.loading = true
	defer mb.clearLoading()

	var nchecks int

checkCache:
	nchecks++
	if nchecks > 8 {
		return errCorruptState
	}

	// Check to see if we have a full cache.
	if mb.cacheAlreadyLoaded() {
		return nil
	}

	mb.llts = time.Now().UnixNano()

	// FIXME(dlc) - We could be smarter here.
	if buf, _ := mb.bytesPending(); len(buf) > 0 {
		ld, err := mb.flushPendingMsgsLocked()
		if ld != nil && mb.fs != nil {
			// We do not know if fs is locked or not at this point.
			// This should be an exceptional condition so do so in Go routine.
			go mb.fs.rebuildState(ld)
		}
		if err != nil {
			return err
		}
		goto checkCache
	}

	// Load in the whole block.
	// We want to hold the mb lock here to avoid any changes to state.
	buf, err := mb.loadBlock(nil)
	if err != nil {
		return err
	}

	// Reset the cache since we just read everything in.
	// Make sure this is cleared in case we had a partial when we started.
	mb.clearCacheAndOffset()

	// Check if we need to decrypt.
	if mb.bek != nil && len(buf) > 0 {
		bek, err := genBlockEncryptionKey(mb.fs.fcfg.Cipher, mb.seed, mb.nonce)
		if err != nil {
			return err
		}
		mb.bek = bek
		mb.bek.XORKeyStream(buf, buf)
	}

	if err := mb.indexCacheBuf(buf); err != nil {
		if err == errCorruptState {
			var ld *LostStreamData
			if ld, err = mb.rebuildStateLocked(); ld != nil {
				// We do not know if fs is locked or not at this point.
				// This should be an exceptional condition so do so in Go routine.
				go mb.fs.rebuildState(ld)
			}
		}
		if err != nil {
			return err
		}
		goto checkCache
	}

	if len(buf) > 0 {
		mb.cloads++
		mb.startCacheExpireTimer()
	}

	return nil
}

// Fetch a message from this block, possibly reading in and caching the messages.
// We assume the block was selected and is correct, so we do not do range checks.
func (mb *msgBlock) fetchMsg(seq uint64, sm *StoreMsg) (*StoreMsg, bool, error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if mb.cacheNotLoaded() {
		if err := mb.loadMsgsWithLock(); err != nil {
			return nil, false, err
		}
	}
	fsm, err := mb.cacheLookup(seq, sm)
	if err != nil {
		return nil, false, err
	}
	expireOk := seq == mb.last.seq && mb.llseq == seq
	return fsm, expireOk, err
}

var (
	errNoCache       = errors.New("no message cache")
	errBadMsg        = errors.New("malformed or corrupt message")
	errDeletedMsg    = errors.New("deleted message")
	errPartialCache  = errors.New("partial cache")
	errNoPending     = errors.New("message block does not have pending data")
	errNotReadable   = errors.New("storage directory not readable")
	errCorruptState  = errors.New("corrupt state file")
	errPendingData   = errors.New("pending data still present")
	errNoEncryption  = errors.New("encryption not enabled")
	errBadKeySize    = errors.New("encryption bad key size")
	errNoMsgBlk      = errors.New("no message block")
	errMsgBlkTooBig  = errors.New("message block size exceeded int capacity")
	errUnknownCipher = errors.New("unknown cipher")
	errDIOStalled    = errors.New("IO is stalled")
)

// Used for marking messages that have had their checksums checked.
// Used to signal a message record with headers.
const hbit = 1 << 31

// Used for marking erased messages sequences.
const ebit = 1 << 63

// Will do a lookup from cache.
// Lock should be held.
func (mb *msgBlock) cacheLookup(seq uint64, sm *StoreMsg) (*StoreMsg, error) {
	if seq < mb.first.seq || seq > mb.last.seq {
		return nil, ErrStoreMsgNotFound
	}

	// If we have a delete map check it.
	if mb.dmap != nil {
		if _, ok := mb.dmap[seq]; ok {
			return nil, errDeletedMsg
		}
	}
	// Detect no cache loaded.
	if mb.cache == nil || mb.cache.fseq == 0 || len(mb.cache.idx) == 0 || len(mb.cache.buf) == 0 {
		return nil, errNoCache
	}
	// Check partial cache status.
	if seq < mb.cache.fseq {
		return nil, errPartialCache
	}

	bi, _, hashChecked, err := mb.slotInfo(int(seq - mb.cache.fseq))
	if err != nil {
		return nil, err
	}

	// Update cache activity.
	mb.llts = time.Now().UnixNano()
	// The llseq signals us when we can expire a cache at the end of a linear scan.
	// We want to only update when we know the last reads (multiple consumers) are sequential.
	if mb.llseq == 0 || seq < mb.llseq || seq == mb.llseq+1 {
		mb.llseq = seq
	}

	li := int(bi) - mb.cache.off
	if li >= len(mb.cache.buf) {
		return nil, errPartialCache
	}
	buf := mb.cache.buf[li:]

	// We use the high bit to denote we have already checked the checksum.
	var hh hash.Hash64
	if !hashChecked {
		hh = mb.hh // This will force the hash check in msgFromBuf.
	}

	// Parse from the raw buffer.
	fsm, err := mb.msgFromBuf(buf, sm, hh)
	if err != nil || fsm == nil {
		return nil, err
	}

	// Deleted messages that are decoded return a 0 for seqeunce.
	if fsm.seq == 0 {
		return nil, errDeletedMsg
	}

	if seq != fsm.seq {
		recycleMsgBlockBuf(mb.cache.buf)
		mb.cache.buf = nil
		return nil, fmt.Errorf("sequence numbers for cache load did not match, %d vs %d", seq, fsm.seq)
	}

	// Clear the check bit here after we know all is good.
	if !hashChecked {
		mb.cache.idx[seq-mb.cache.fseq] = (bi | hbit)
	}

	return fsm, nil
}

// Used when we are checking if discarding a message due to max msgs per subject will give us
// enough room for a max bytes condition.
// Lock should be already held.
func (fs *fileStore) sizeForSeq(seq uint64) int {
	if seq == 0 {
		return 0
	}
	var smv StoreMsg
	if mb := fs.selectMsgBlock(seq); mb != nil {
		if sm, _, _ := mb.fetchMsg(seq, &smv); sm != nil {
			return int(fileStoreMsgSize(sm.subj, sm.hdr, sm.msg))
		}
	}
	return 0
}

// Will return message for the given sequence number.
func (fs *fileStore) msgForSeq(seq uint64, sm *StoreMsg) (*StoreMsg, error) {
	// TODO(dlc) - Since Store, Remove, Skip all hold the write lock on fs this will
	// be stalled. Need another lock if want to happen in parallel.
	fs.mu.RLock()
	if fs.closed {
		fs.mu.RUnlock()
		return nil, ErrStoreClosed
	}
	// Indicates we want first msg.
	if seq == 0 {
		seq = fs.state.FirstSeq
	}
	// Make sure to snapshot here.
	mb, lmb, lseq := fs.selectMsgBlock(seq), fs.lmb, fs.state.LastSeq
	fs.mu.RUnlock()

	if mb == nil {
		var err = ErrStoreEOF
		if seq <= lseq {
			err = ErrStoreMsgNotFound
		}
		return nil, err
	}

	fsm, expireOk, err := mb.fetchMsg(seq, sm)
	if err != nil {
		return nil, err
	}

	// We detected a linear scan and access to the last message.
	// If we are not the last message block we can try to expire the cache.
	if mb != lmb && expireOk {
		mb.tryForceExpireCache()
	}

	return fsm, nil
}

// Internal function to return msg parts from a raw buffer.
// Lock should be held.
func (mb *msgBlock) msgFromBuf(buf []byte, sm *StoreMsg, hh hash.Hash64) (*StoreMsg, error) {
	if len(buf) < emptyRecordLen {
		return nil, errBadMsg
	}
	var le = binary.LittleEndian

	hdr := buf[:msgHdrSize]
	rl := le.Uint32(hdr[0:])
	hasHeaders := rl&hbit != 0
	rl &^= hbit // clear header bit
	dlen := int(rl) - msgHdrSize
	slen := int(le.Uint16(hdr[20:]))
	// Simple sanity check.
	if dlen < 0 || slen > dlen || int(rl) > len(buf) {
		return nil, errBadMsg
	}
	data := buf[msgHdrSize : msgHdrSize+dlen]
	// Do checksum tests here if requested.
	if hh != nil {
		hh.Reset()
		hh.Write(hdr[4:20])
		hh.Write(data[:slen])
		if hasHeaders {
			hh.Write(data[slen+4 : dlen-8])
		} else {
			hh.Write(data[slen : dlen-8])
		}
		if !bytes.Equal(hh.Sum(nil), data[len(data)-8:]) {
			return nil, errBadMsg
		}
	}
	seq := le.Uint64(hdr[4:])
	if seq&ebit != 0 {
		seq = 0
	}
	ts := int64(le.Uint64(hdr[12:]))

	// Create a StoreMsg if needed.
	if sm == nil {
		sm = new(StoreMsg)
	} else {
		sm.clear()
	}
	// To recycle the large blocks we can never pass back a reference, so need to copy for the upper
	// layers and for us to be safe to expire, and recycle, the large msgBlocks.
	end := dlen - 8

	if hasHeaders {
		hl := le.Uint32(data[slen:])
		bi := slen + 4
		li := bi + int(hl)
		sm.buf = append(sm.buf, data[bi:end]...)
		li, end = li-bi, end-bi
		sm.hdr = sm.buf[0:li:li]
		sm.msg = sm.buf[li:end]
	} else {
		sm.buf = append(sm.buf, data[slen:end]...)
		sm.msg = sm.buf[0 : end-slen]
	}
	sm.seq, sm.ts = seq, ts
	// Treat subject a bit different to not reference underlying buf.
	if slen > 0 {
		sm.subj = mb.subjString(data[:slen])
	}

	return sm, nil
}

// Given the `key` byte slice, this function will return the subject
// as a copy of `key` or a configured subject as to minimize memory allocations.
// Lock should be held.
func (mb *msgBlock) subjString(skey []byte) string {
	if len(skey) == 0 {
		return _EMPTY_
	}
	key := string(skey)

	if lsubjs := len(mb.fs.cfg.Subjects); lsubjs > 0 {
		if lsubjs == 1 {
			// The cast for the comparison does not make a copy
			if key == mb.fs.cfg.Subjects[0] {
				return mb.fs.cfg.Subjects[0]
			}
		} else {
			for _, subj := range mb.fs.cfg.Subjects {
				if key == subj {
					return subj
				}
			}
		}
	}

	return key
}

// LoadMsg will lookup the message by sequence number and return it if found.
func (fs *fileStore) LoadMsg(seq uint64, sm *StoreMsg) (*StoreMsg, error) {
	return fs.msgForSeq(seq, sm)
}

// loadLast will load the last message for a subject. Subject should be non empty and not ">".
func (fs *fileStore) loadLast(subj string, sm *StoreMsg) (lsm *StoreMsg, err error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	if fs.closed || fs.lmb == nil {
		return nil, ErrStoreClosed
	}

	if len(fs.blks) == 0 {
		return nil, ErrStoreMsgNotFound
	}

	start, stop := fs.lmb.index, fs.blks[0].index
	wc := subjectHasWildcard(subj)
	// If literal subject check for presence.
	if !wc {
		if info := fs.psim[subj]; info == nil {
			return nil, ErrStoreMsgNotFound
		} else {
			start, stop = info.lblk, info.fblk
		}
	}

	// Walk blocks backwards.
	for i := start; i >= stop; i-- {
		mb := fs.bim[i]
		if mb == nil {
			continue
		}
		mb.mu.Lock()
		if err := mb.ensurePerSubjectInfoLoaded(); err != nil {
			mb.mu.Unlock()
			return nil, err
		}
		_, _, l := mb.filteredPendingLocked(subj, wc, mb.first.seq)
		if l > 0 {
			if mb.cacheNotLoaded() {
				if err := mb.loadMsgsWithLock(); err != nil {
					mb.mu.Unlock()
					return nil, err
				}
			}
			lsm, err = mb.cacheLookup(l, sm)
		}
		mb.mu.Unlock()
		if l > 0 {
			break
		}
	}
	return lsm, err
}

// LoadLastMsg will return the last message we have that matches a given subject.
// The subject can be a wildcard.
func (fs *fileStore) LoadLastMsg(subject string, smv *StoreMsg) (sm *StoreMsg, err error) {
	if subject == _EMPTY_ || subject == fwcs {
		sm, err = fs.msgForSeq(fs.lastSeq(), smv)
	} else {
		sm, err = fs.loadLast(subject, smv)
	}
	if sm == nil || (err != nil && err != ErrStoreClosed) {
		err = ErrStoreMsgNotFound
	}
	return sm, err
}

func (fs *fileStore) LoadNextMsg(filter string, wc bool, start uint64, sm *StoreMsg) (*StoreMsg, uint64, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	if fs.closed {
		return nil, 0, ErrStoreClosed
	}
	if start < fs.state.FirstSeq {
		start = fs.state.FirstSeq
	}

	// TODO(dlc) - If num blocks gets large maybe use selectMsgBlock but have it return index b/c
	// we need to keep walking if no match found in first mb.
	for _, mb := range fs.blks {
		// Skip blocks that are less than our starting sequence.
		if start > atomic.LoadUint64(&mb.last.seq) {
			continue
		}
		if sm, expireOk, err := mb.firstMatching(filter, wc, start, sm); err == nil {
			if expireOk && mb != fs.lmb {
				mb.tryForceExpireCache()
			}
			return sm, sm.seq, nil
		} else if err != ErrStoreMsgNotFound {
			return nil, 0, err
		}
	}

	return nil, fs.state.LastSeq, ErrStoreEOF
}

// Type returns the type of the underlying store.
func (fs *fileStore) Type() StorageType {
	return FileStorage
}

// Returns number of subjects in this store.
// Lock should be held.
func (fs *fileStore) numSubjects() int {
	return len(fs.psim)
}

// FastState will fill in state with only the following.
// Msgs, Bytes, First and Last Sequence and Time and NumDeleted.
func (fs *fileStore) FastState(state *StreamState) {
	fs.mu.RLock()
	state.Msgs = fs.state.Msgs
	state.Bytes = fs.state.Bytes
	state.FirstSeq = fs.state.FirstSeq
	state.FirstTime = fs.state.FirstTime
	state.LastSeq = fs.state.LastSeq
	state.LastTime = fs.state.LastTime
	if state.LastSeq > state.FirstSeq {
		state.NumDeleted = int((state.LastSeq - state.FirstSeq + 1) - state.Msgs)
		if state.NumDeleted < 0 {
			state.NumDeleted = 0
		}
	}
	state.Consumers = len(fs.cfs)
	state.NumSubjects = fs.numSubjects()
	fs.mu.RUnlock()
}

// State returns the current state of the stream.
func (fs *fileStore) State() StreamState {
	fs.mu.RLock()
	state := fs.state
	state.Consumers = len(fs.cfs)
	state.NumSubjects = fs.numSubjects()
	state.Deleted = nil // make sure.

	if numDeleted := int((state.LastSeq - state.FirstSeq + 1) - state.Msgs); numDeleted > 0 {
		state.Deleted = make([]uint64, 0, numDeleted)
		cur := fs.state.FirstSeq

		for _, mb := range fs.blks {
			mb.mu.Lock()
			fseq := mb.first.seq
			// Account for messages missing from the head.
			if fseq > cur {
				for seq := cur; seq < fseq; seq++ {
					state.Deleted = append(state.Deleted, seq)
				}
			}
			cur = mb.last.seq + 1 // Expected next first.
			for seq := range mb.dmap {
				if seq < fseq {
					delete(mb.dmap, seq)
				} else {
					state.Deleted = append(state.Deleted, seq)
				}
			}
			mb.mu.Unlock()
		}
	}
	fs.mu.RUnlock()

	state.Lost = fs.lostData()

	// Can not be guaranteed to be sorted.
	if len(state.Deleted) > 0 {
		sort.Slice(state.Deleted, func(i, j int) bool {
			return state.Deleted[i] < state.Deleted[j]
		})
		state.NumDeleted = len(state.Deleted)
	}
	return state
}

func (fs *fileStore) Utilization() (total, reported uint64, err error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	for _, mb := range fs.blks {
		mb.mu.RLock()
		reported += mb.bytes
		total += mb.rbytes
		mb.mu.RUnlock()
	}
	return total, reported, nil
}

func fileStoreMsgSize(subj string, hdr, msg []byte) uint64 {
	if len(hdr) == 0 {
		// length of the message record (4bytes) + seq(8) + ts(8) + subj_len(2) + subj + msg + hash(8)
		return uint64(22 + len(subj) + len(msg) + 8)
	}
	// length of the message record (4bytes) + seq(8) + ts(8) + subj_len(2) + subj + hdr_len(4) + hdr + msg + hash(8)
	return uint64(22 + len(subj) + 4 + len(hdr) + len(msg) + 8)
}

func fileStoreMsgSizeEstimate(slen, maxPayload int) uint64 {
	return uint64(emptyRecordLen + slen + 4 + maxPayload)
}

// Determine time since last write or remove of a message.
// Read lock should be held.
func (mb *msgBlock) sinceLastWriteActivity() time.Duration {
	if mb.closed {
		return 0
	}
	last := mb.lwts
	if mb.lrts > last {
		last = mb.lrts
	}
	return time.Since(time.Unix(0, last).UTC())
}

// Determine if we need to write out this index info.
func (mb *msgBlock) indexNeedsUpdate() bool {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	return mb.indexNeedsUpdateLocked()
}

// Determine if we need to write out this index info.
// Lock should be held.
func (mb *msgBlock) indexNeedsUpdateLocked() bool {
	return mb.lwits < mb.lwts || mb.lwits < mb.lrts
}

// Write index info to the appropriate file.
// Filestore lock should be held.
func (mb *msgBlock) writeIndexInfo() error {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	return mb.writeIndexInfoLocked()
}

// Write index info to the appropriate file.
// Filestore lock and mb lock should be held.
func (mb *msgBlock) writeIndexInfoLocked() error {
	// HEADER: magic version msgs bytes fseq fts lseq lts ndel checksum
	var hdr [indexHdrSize]byte

	// Write header
	hdr[0] = magic
	hdr[1] = version

	n := hdrLen
	n += binary.PutUvarint(hdr[n:], mb.msgs)
	n += binary.PutUvarint(hdr[n:], mb.bytes)
	n += binary.PutUvarint(hdr[n:], mb.first.seq)
	n += binary.PutVarint(hdr[n:], mb.first.ts)
	n += binary.PutUvarint(hdr[n:], mb.last.seq)
	n += binary.PutVarint(hdr[n:], mb.last.ts)
	n += binary.PutUvarint(hdr[n:], uint64(len(mb.dmap)))
	buf := append(hdr[:n], mb.lchk[:]...)

	// Append a delete map if needed
	if len(mb.dmap) > 0 {
		buf = append(buf, mb.genDeleteMap()...)
	}

	// Open our FD if needed.
	if mb.ifd == nil {
		ifd, err := os.OpenFile(mb.ifn, os.O_CREATE|os.O_RDWR, defaultFilePerms)
		if err != nil {
			return err
		}
		if fi, _ := ifd.Stat(); fi != nil {
			mb.liwsz = fi.Size()
		}
		mb.ifd = ifd
	}

	// Encrypt if needed.
	if mb.aek != nil {
		buf = mb.aek.Seal(buf[:0], mb.nonce, buf, nil)
	}

	// Check if this will be a short write, and if so truncate before writing here.
	// We only really need to truncate if we are encryptyed or we have dmap entries.
	// If no dmap entries readIndexInfo does the right thing in the presence of extra data left over.
	if int64(len(buf)) < mb.liwsz && (mb.aek != nil || len(mb.dmap) > 0) {
		if err := mb.ifd.Truncate(0); err != nil {
			mb.werr = err
			return err
		}
	}

	var err error
	if n, err = mb.ifd.WriteAt(buf, 0); err == nil {
		mb.lwits = time.Now().UnixNano()
		mb.liwsz = int64(n)
		mb.werr = nil
	} else {
		mb.werr = err
	}
	return err
}

// readIndexInfo will read in the index information for the message block.
func (mb *msgBlock) readIndexInfo() error {
	buf, err := os.ReadFile(mb.ifn)
	if err != nil {
		return err
	}

	// Set if first time.
	if mb.liwsz == 0 {
		mb.liwsz = int64(len(buf))
	}

	// Decrypt if needed.
	if mb.aek != nil {
		buf, err = mb.aek.Open(buf[:0], mb.nonce, buf, nil)
		if err != nil {
			return err
		}
	}

	if err := checkHeader(buf); err != nil {
		defer os.Remove(mb.ifn)
		return fmt.Errorf("bad index file")
	}

	bi := hdrLen

	// Helpers, will set i to -1 on error.
	readSeq := func() uint64 {
		if bi < 0 {
			return 0
		}
		seq, n := binary.Uvarint(buf[bi:])
		if n <= 0 {
			bi = -1
			return 0
		}
		bi += n
		return seq &^ ebit
	}
	readCount := readSeq
	readTimeStamp := func() int64 {
		if bi < 0 {
			return 0
		}
		ts, n := binary.Varint(buf[bi:])
		if n <= 0 {
			bi = -1
			return -1
		}
		bi += n
		return ts
	}
	mb.msgs = readCount()
	mb.bytes = readCount()
	mb.first.seq = readSeq()
	mb.first.ts = readTimeStamp()
	mb.last.seq = readSeq()
	mb.last.ts = readTimeStamp()
	dmapLen := readCount()

	// Check if this is a short write index file.
	if bi < 0 || bi+checksumSize > len(buf) {
		os.Remove(mb.ifn)
		return fmt.Errorf("short index file")
	}

	// Check for consistency if accounting. If something is off bail and we will rebuild.
	if mb.msgs != (mb.last.seq-mb.first.seq+1)-dmapLen {
		os.Remove(mb.ifn)
		return fmt.Errorf("accounting inconsistent")
	}

	// Checksum
	copy(mb.lchk[0:], buf[bi:bi+checksumSize])
	bi += checksumSize

	// Now check for presence of a delete map
	if dmapLen > 0 {
		mb.dmap = make(map[uint64]struct{}, dmapLen)
		for i := 0; i < int(dmapLen); i++ {
			seq := readSeq()
			if seq == 0 {
				break
			}
			mb.dmap[seq+mb.first.seq] = struct{}{}
		}
	}

	return nil
}

func (mb *msgBlock) genDeleteMap() []byte {
	if len(mb.dmap) == 0 {
		return nil
	}
	buf := make([]byte, len(mb.dmap)*binary.MaxVarintLen64)
	// We use first seq as an offset to cut down on size.
	fseq, n := uint64(mb.first.seq), 0
	for seq := range mb.dmap {
		// This is for lazy cleanup as the first sequence moves up.
		if seq < fseq {
			delete(mb.dmap, seq)
		} else {
			n += binary.PutUvarint(buf[n:], seq-fseq)
		}
	}
	return buf[:n]
}

func syncAndClose(mfd, ifd *os.File) {
	if mfd != nil {
		mfd.Sync()
		mfd.Close()
	}
	if ifd != nil {
		ifd.Sync()
		ifd.Close()
	}
}

// Will return total number of cache loads.
func (fs *fileStore) cacheLoads() uint64 {
	var tl uint64
	fs.mu.RLock()
	for _, mb := range fs.blks {
		tl += mb.cloads
	}
	fs.mu.RUnlock()
	return tl
}

// Will return total number of cached bytes.
func (fs *fileStore) cacheSize() uint64 {
	var sz uint64
	fs.mu.RLock()
	for _, mb := range fs.blks {
		mb.mu.RLock()
		if mb.cache != nil {
			sz += uint64(len(mb.cache.buf))
		}
		mb.mu.RUnlock()
	}
	fs.mu.RUnlock()
	return sz
}

// Will return total number of dmapEntries for all msg blocks.
func (fs *fileStore) dmapEntries() int {
	var total int
	fs.mu.RLock()
	for _, mb := range fs.blks {
		total += len(mb.dmap)
	}
	fs.mu.RUnlock()
	return total
}

// Fixed helper for iterating.
func subjectsEqual(a, b string) bool {
	return a == b
}

func subjectsAll(a, b string) bool {
	return true
}

func compareFn(subject string) func(string, string) bool {
	if subject == _EMPTY_ || subject == fwcs {
		return subjectsAll
	}
	if subjectHasWildcard(subject) {
		return subjectIsSubsetMatch
	}
	return subjectsEqual
}

// PurgeEx will remove messages based on subject filters, sequence and number of messages to keep.
// Will return the number of purged messages.
func (fs *fileStore) PurgeEx(subject string, sequence, keep uint64) (purged uint64, err error) {
	if sequence > 1 && keep > 0 {
		return 0, ErrPurgeArgMismatch
	}

	if subject == _EMPTY_ || subject == fwcs {
		if keep == 0 && (sequence == 0 || sequence == 1) {
			return fs.Purge()
		}
		if sequence > 1 {
			return fs.Compact(sequence)
		} else if keep > 0 {
			fs.mu.RLock()
			msgs, lseq := fs.state.Msgs, fs.state.LastSeq
			fs.mu.RUnlock()
			if keep >= msgs {
				return 0, nil
			}
			return fs.Compact(lseq - keep + 1)
		}
		return 0, nil
	}

	eq, wc := compareFn(subject), subjectHasWildcard(subject)
	var firstSeqNeedsUpdate bool
	var bytes uint64

	// If we have a "keep" designation need to get full filtered state so we know how many to purge.
	var maxp uint64
	if keep > 0 {
		ss := fs.FilteredState(1, subject)
		if keep >= ss.Msgs {
			return 0, nil
		}
		maxp = ss.Msgs - keep
	}

	var smv StoreMsg

	fs.mu.Lock()
	// We may remove blocks as we purge, so don't range directly on fs.blks
	// otherwise we may jump over some (see https://github.com/nats-io/nats-server/issues/3528)
	for i := 0; i < len(fs.blks); i++ {
		mb := fs.blks[i]
		mb.mu.Lock()
		if err := mb.ensurePerSubjectInfoLoaded(); err != nil {
			mb.mu.Unlock()
			continue
		}
		t, f, l := mb.filteredPendingLocked(subject, wc, mb.first.seq)
		if t == 0 {
			mb.mu.Unlock()
			continue
		}

		var shouldExpire bool
		if mb.cacheNotLoaded() {
			mb.loadMsgsWithLock()
			shouldExpire = true
		}
		if sequence > 1 && sequence <= l {
			l = sequence - 1
		}

		for seq := f; seq <= l; seq++ {
			if sm, _ := mb.cacheLookup(seq, &smv); sm != nil && eq(sm.subj, subject) {
				rl := fileStoreMsgSize(sm.subj, sm.hdr, sm.msg)
				// Do fast in place remove.
				// Stats
				if mb.msgs > 0 {
					fs.state.Msgs--
					fs.state.Bytes -= rl
					mb.msgs--
					mb.bytes -= rl
					purged++
					bytes += rl
				}
				// FSS updates.
				mb.removeSeqPerSubject(sm.subj, seq, &smv)
				fs.removePerSubject(sm.subj)

				// Check for first message.
				if seq == mb.first.seq {
					mb.selectNextFirst()
					if mb.isEmpty() {
						fs.removeMsgBlock(mb)
						i--
						firstSeqNeedsUpdate = seq == fs.state.FirstSeq
					} else if seq == fs.state.FirstSeq {
						fs.state.FirstSeq = mb.first.seq // new one.
						fs.state.FirstTime = time.Unix(0, mb.first.ts).UTC()
					}
				} else {
					// Out of order delete.
					if mb.dmap == nil {
						mb.dmap = make(map[uint64]struct{})
					}
					mb.dmap[seq] = struct{}{}
				}

				if maxp > 0 && purged >= maxp {
					break
				}
			}
		}
		// Expire if we were responsible for loading.
		if shouldExpire {
			// Expire this cache before moving on.
			mb.tryForceExpireCacheLocked()
		}
		mb.mu.Unlock()
		// Update our index info on disk.
		mb.writeIndexInfo()

		// Check if we should break out of top level too.
		if maxp > 0 && purged >= maxp {
			break
		}
	}
	if firstSeqNeedsUpdate {
		fs.selectNextFirst()
	}

	cb := fs.scb
	fs.mu.Unlock()

	if cb != nil {
		cb(-int64(purged), -int64(bytes), 0, _EMPTY_)
	}

	return purged, nil
}

// Purge will remove all messages from this store.
// Will return the number of purged messages.
func (fs *fileStore) Purge() (uint64, error) {
	return fs.purge(0)
}

func (fs *fileStore) purge(fseq uint64) (uint64, error) {
	fs.mu.Lock()
	if fs.closed {
		fs.mu.Unlock()
		return 0, ErrStoreClosed
	}

	purged := fs.state.Msgs
	rbytes := int64(fs.state.Bytes)

	fs.state.FirstSeq = fs.state.LastSeq + 1
	fs.state.FirstTime = time.Time{}

	fs.state.Bytes = 0
	fs.state.Msgs = 0

	for _, mb := range fs.blks {
		mb.dirtyClose()
	}

	fs.blks = nil
	fs.lmb = nil
	fs.bim = make(map[uint32]*msgBlock)

	// Move the msgs directory out of the way, will delete out of band.
	// FIXME(dlc) - These can error and we need to change api above to propagate?
	mdir := filepath.Join(fs.fcfg.StoreDir, msgDir)
	pdir := filepath.Join(fs.fcfg.StoreDir, purgeDir)
	// If purge directory still exists then we need to wait
	// in place and remove since rename would fail.
	if _, err := os.Stat(pdir); err == nil {
		os.RemoveAll(pdir)
	}
	os.Rename(mdir, pdir)
	go os.RemoveAll(pdir)
	// Create new one.
	os.MkdirAll(mdir, defaultDirPerms)

	// Make sure we have a lmb to write to.
	if _, err := fs.newMsgBlockForWrite(); err != nil {
		fs.mu.Unlock()
		return purged, err
	}

	// Check if we need to set the first seq to a new number.
	if fseq > fs.state.FirstSeq {
		fs.state.FirstSeq = fseq
		fs.state.LastSeq = fseq - 1
	}
	fs.lmb.first.seq = fs.state.FirstSeq
	fs.lmb.last.seq = fs.state.LastSeq
	fs.lmb.last.ts = fs.state.LastTime.UnixNano()

	fs.lmb.writeIndexInfo()

	// Clear any per subject tracking.
	fs.psim = make(map[string]*psi)

	cb := fs.scb
	fs.mu.Unlock()

	if cb != nil {
		cb(-int64(purged), -rbytes, 0, _EMPTY_)
	}

	return purged, nil
}

// Compact will remove all messages from this store up to
// but not including the seq parameter.
// Will return the number of purged messages.
func (fs *fileStore) Compact(seq uint64) (uint64, error) {
	if seq == 0 {
		return fs.purge(seq)
	}

	var purged, bytes uint64

	// We have to delete interior messages.
	fs.mu.Lock()
	if lseq := fs.state.LastSeq; seq > lseq {
		fs.mu.Unlock()
		return fs.purge(seq)
	}

	smb := fs.selectMsgBlock(seq)
	if smb == nil {
		fs.mu.Unlock()
		return 0, nil
	}

	// All msgblocks up to this one can be thrown away.
	var deleted int
	for _, mb := range fs.blks {
		if mb == smb {
			break
		}
		mb.mu.Lock()
		purged += mb.msgs
		bytes += mb.bytes
		// Make sure we do subject cleanup as well.
		mb.ensurePerSubjectInfoLoaded()
		for subj := range mb.fss {
			fs.removePerSubject(subj)
		}
		// Now close.
		mb.dirtyCloseWithRemove(true)
		mb.mu.Unlock()
		deleted++
	}

	var smv StoreMsg
	var err error
	var isEmpty bool

	smb.mu.Lock()
	if smb.first.seq == seq {
		isEmpty = smb.msgs == 0
		goto SKIP
	}

	// Make sure we have the messages loaded.
	if smb.cacheNotLoaded() {
		if err = smb.loadMsgsWithLock(); err != nil {
			goto SKIP
		}
	}
	for mseq := smb.first.seq; mseq < seq; mseq++ {
		sm, err := smb.cacheLookup(mseq, &smv)
		if err == errDeletedMsg {
			// Update dmap.
			if len(smb.dmap) > 0 {
				delete(smb.dmap, mseq)
				if len(smb.dmap) == 0 {
					smb.dmap = nil
				}
			}
		} else if sm != nil {
			sz := fileStoreMsgSize(sm.subj, sm.hdr, sm.msg)
			if smb.msgs > 0 {
				smb.bytes -= sz
				bytes += sz
				smb.msgs--
				purged++
			}
			// Update fss
			smb.removeSeqPerSubject(sm.subj, mseq, &smv)
			fs.removePerSubject(sm.subj)
		}
	}

	// Check if empty after processing, could happen if tail of messages are all deleted.
	isEmpty = smb.msgs == 0
	if isEmpty {
		smb.dirtyCloseWithRemove(true)
		// Update fs first here as well.
		fs.state.FirstSeq = smb.last.seq + 1
		fs.state.FirstTime = time.Time{}
		deleted++
	} else {
		// Update fs first seq and time.
		smb.first.seq = seq - 1 // Just for start condition for selectNextFirst.
		smb.selectNextFirst()
		fs.state.FirstSeq = smb.first.seq
		fs.state.FirstTime = time.Unix(0, smb.first.ts).UTC()

		// Check if we should reclaim the head space from this block.
		// This will be optimistic only, so don't continue if we encounter any errors here.
		if smb.rbytes > compactMinimum && smb.bytes*2 < smb.rbytes {
			var moff uint32
			moff, _, _, err = smb.slotInfo(int(smb.first.seq - smb.cache.fseq))
			if err != nil || moff >= uint32(len(smb.cache.buf)) {
				goto SKIP
			}
			buf := smb.cache.buf[moff:]
			// Don't reuse, copy to new recycled buf.
			nbuf := getMsgBlockBuf(len(buf))
			nbuf = append(nbuf, buf...)
			smb.closeFDsLockedNoCheck()
			// Check for encryption.
			if smb.bek != nil && len(nbuf) > 0 {
				// Recreate to reset counter.
				bek, err := genBlockEncryptionKey(smb.fs.fcfg.Cipher, smb.seed, smb.nonce)
				if err != nil {
					goto SKIP
				}
				// For future writes make sure to set smb.bek to keep counter correct.
				smb.bek = bek
				smb.bek.XORKeyStream(nbuf, nbuf)
			}
			if err = os.WriteFile(smb.mfn, nbuf, defaultFilePerms); err != nil {
				goto SKIP
			}
			// Make sure to remove fss state.
			smb.fss = nil
			smb.removePerSubjectInfoLocked()
			smb.clearCacheAndOffset()
			smb.rbytes = uint64(len(nbuf))
		}
	}

SKIP:
	if !isEmpty {
		// Make sure to write out our index info.
		smb.writeIndexInfoLocked()
	}

	smb.mu.Unlock()

	if deleted > 0 {
		// Update block map.
		if fs.bim != nil {
			for _, mb := range fs.blks[:deleted] {
				delete(fs.bim, mb.index)
			}
		}
		// Update blks slice.
		fs.blks = copyMsgBlocks(fs.blks[deleted:])
		if lb := len(fs.blks); lb == 0 {
			fs.lmb = nil
		} else {
			fs.lmb = fs.blks[lb-1]
		}
	}

	// Update top level accounting.
	fs.state.Msgs -= purged
	fs.state.Bytes -= bytes

	cb := fs.scb
	fs.mu.Unlock()

	if cb != nil && purged > 0 {
		cb(-int64(purged), -int64(bytes), 0, _EMPTY_)
	}

	return purged, err
}

// Will completely reset our store.
func (fs *fileStore) reset() error {
	fs.mu.Lock()
	if fs.closed {
		fs.mu.Unlock()
		return ErrStoreClosed
	}
	if fs.sips > 0 {
		fs.mu.Unlock()
		return ErrStoreSnapshotInProgress
	}

	var purged, bytes uint64
	cb := fs.scb

	for _, mb := range fs.blks {
		mb.mu.Lock()
		purged += mb.msgs
		bytes += mb.bytes
		mb.dirtyCloseWithRemove(true)
		mb.mu.Unlock()
	}

	// Reset
	fs.state.FirstSeq = 0
	fs.state.FirstTime = time.Time{}
	fs.state.LastSeq = 0
	fs.state.LastTime = time.Now().UTC()
	// Update msgs and bytes.
	fs.state.Msgs = 0
	fs.state.Bytes = 0

	// Reset blocks.
	fs.blks, fs.lmb = nil, nil

	// Reset subject mappings.
	fs.psim = make(map[string]*psi)
	fs.bim = make(map[uint32]*msgBlock)

	fs.mu.Unlock()

	if cb != nil {
		cb(-int64(purged), -int64(bytes), 0, _EMPTY_)
	}

	return nil
}

// Truncate will truncate a stream store up to seq. Sequence needs to be valid.
func (fs *fileStore) Truncate(seq uint64) error {
	// Check for request to reset.
	if seq == 0 {
		return fs.reset()
	}

	fs.mu.Lock()

	if fs.closed {
		fs.mu.Unlock()
		return ErrStoreClosed
	}
	if fs.sips > 0 {
		fs.mu.Unlock()
		return ErrStoreSnapshotInProgress
	}

	nlmb := fs.selectMsgBlock(seq)
	if nlmb == nil {
		fs.mu.Unlock()
		return ErrInvalidSequence
	}
	lsm, _, _ := nlmb.fetchMsg(seq, nil)
	if lsm == nil {
		fs.mu.Unlock()
		return ErrInvalidSequence
	}

	// Set lmb to nlmb and make sure writeable.
	fs.lmb = nlmb
	if err := nlmb.enableForWriting(fs.fip); err != nil {
		return err
	}

	var purged, bytes uint64

	// Truncate our new last message block.
	nmsgs, nbytes, err := nlmb.truncate(lsm)
	if err != nil {
		fs.mu.Unlock()
		return err
	}
	// Account for the truncated msgs and bytes.
	purged += nmsgs
	bytes += nbytes

	// Remove any left over msg blocks.
	getLastMsgBlock := func() *msgBlock { return fs.blks[len(fs.blks)-1] }
	for mb := getLastMsgBlock(); mb != nlmb; mb = getLastMsgBlock() {
		mb.mu.Lock()
		purged += mb.msgs
		bytes += mb.bytes
		fs.removeMsgBlock(mb)
		mb.mu.Unlock()
	}

	// Reset last.
	fs.state.LastSeq = lsm.seq
	fs.state.LastTime = time.Unix(0, lsm.ts).UTC()
	// Update msgs and bytes.
	fs.state.Msgs -= purged
	fs.state.Bytes -= bytes

	// Reset our subject lookup info.
	fs.resetGlobalPerSubjectInfo()

	cb := fs.scb
	fs.mu.Unlock()

	if cb != nil {
		cb(-int64(purged), -int64(bytes), 0, _EMPTY_)
	}

	return nil
}

func (fs *fileStore) lastSeq() uint64 {
	fs.mu.RLock()
	seq := fs.state.LastSeq
	fs.mu.RUnlock()
	return seq
}

// Returns number of msg blks.
func (fs *fileStore) numMsgBlocks() int {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return len(fs.blks)
}

// Will remove our index file.
func (mb *msgBlock) removeIndexFile() {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	mb.removeIndexFileLocked()
}

func (mb *msgBlock) removeIndexFileLocked() {
	if mb.ifd != nil {
		mb.ifd.Close()
		mb.ifd = nil
	}
	if mb.ifn != _EMPTY_ {
		os.Remove(mb.ifn)
	}
}

func (mb *msgBlock) removePerSubjectInfoLocked() {
	if mb.sfn != _EMPTY_ {
		os.Remove(mb.sfn)
	}
}

// Will add a new msgBlock.
// Lock should be held.
func (fs *fileStore) addMsgBlock(mb *msgBlock) {
	fs.blks = append(fs.blks, mb)
	fs.lmb = mb
	fs.bim[mb.index] = mb
}

// Removes the msgBlock
// Both locks should be held.
func (fs *fileStore) removeMsgBlock(mb *msgBlock) {
	mb.dirtyCloseWithRemove(true)

	// Remove from list.
	for i, omb := range fs.blks {
		if mb == omb {
			blks := append(fs.blks[:i], fs.blks[i+1:]...)
			fs.blks = copyMsgBlocks(blks)
			if fs.bim != nil {
				delete(fs.bim, mb.index)
			}
			break
		}
	}
	// Check for us being last message block
	if mb == fs.lmb {
		// Creating a new message write block requires that the lmb lock is not held.
		mb.mu.Unlock()
		fs.newMsgBlockForWrite()
		mb.mu.Lock()
	}
}

// When we have an empty block but want to keep the index for timestamp info etc.
// Lock should be held.
func (mb *msgBlock) closeAndKeepIndex(viaLimits bool) {
	// We will leave a 0 length blk marker.
	if mb.mfd != nil {
		mb.mfd.Truncate(0)
	} else {
		// We were closed, so just write out an empty file.
		os.WriteFile(mb.mfn, nil, defaultFilePerms)
	}
	// Make sure to write the index file so we can remember last seq and ts.
	mb.writeIndexInfoLocked()
	// Close
	mb.dirtyCloseWithRemove(false)

	// Make sure to remove fss state.
	mb.fss = nil
	mb.removePerSubjectInfoLocked()

	// If we are encrypted we should reset our bek counter.
	if mb.bek != nil {
		if bek, err := genBlockEncryptionKey(mb.fs.fcfg.Cipher, mb.seed, mb.nonce); err == nil {
			mb.bek = bek
		}
	}
}

// Called by purge to simply get rid of the cache and close our fds.
// Lock should not be held.
func (mb *msgBlock) dirtyClose() {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	mb.dirtyCloseWithRemove(false)
}

// Should be called with lock held.
func (mb *msgBlock) dirtyCloseWithRemove(remove bool) {
	if mb == nil {
		return
	}
	// Stop cache expiration timer.
	if mb.ctmr != nil {
		mb.ctmr.Stop()
		mb.ctmr = nil
	}
	// Check if we are tracking by subject.
	if mb.fss != nil {
		if !remove {
			mb.writePerSubjectInfo()
		}
		mb.fss = nil
	}
	// Close cache
	mb.clearCacheAndOffset()
	// Quit our loops.
	if mb.qch != nil {
		close(mb.qch)
		mb.qch = nil
	}
	if mb.mfd != nil {
		mb.mfd.Close()
		mb.mfd = nil
	}
	if mb.ifd != nil {
		mb.ifd.Close()
		mb.ifd = nil
	}
	if remove {
		if mb.ifn != _EMPTY_ {
			os.Remove(mb.ifn)
			mb.ifn = _EMPTY_
		}
		if mb.mfn != _EMPTY_ {
			os.Remove(mb.mfn)
			mb.mfn = _EMPTY_
		}
		if mb.sfn != _EMPTY_ {
			os.Remove(mb.sfn)
			mb.sfn = _EMPTY_
		}
		if mb.kfn != _EMPTY_ {
			os.Remove(mb.kfn)
		}
	}
}

// Remove a seq from the fss and select new first.
// Lock should be held.
func (mb *msgBlock) removeSeqPerSubject(subj string, seq uint64, smp *StoreMsg) {
	mb.ensurePerSubjectInfoLoaded()
	ss := mb.fss[subj]
	if ss == nil {
		return
	}

	if ss.Msgs == 1 {
		delete(mb.fss, subj)
		mb.fssNeedsWrite = true // Mark dirty
		return
	}

	ss.Msgs--
	if seq != ss.First {
		return
	}

	// Only one left.
	if ss.Msgs == 1 {
		if seq != ss.First {
			ss.Last = ss.First
			mb.fssNeedsWrite = true // Mark dirty
		} else {
			ss.First = ss.Last
			mb.fssNeedsWrite = true // Mark dirty
		}
		return
	}

	// Recalculate first.
	// TODO(dlc) - Might want to optimize this.
	if seq == ss.First {
		var smv StoreMsg
		if smp == nil {
			smp = &smv
		}
		for tseq := seq + 1; tseq <= ss.Last; tseq++ {
			if sm, _ := mb.cacheLookup(tseq, smp); sm != nil {
				if sm.subj == subj {
					ss.First = tseq
					mb.fssNeedsWrite = true // Mark dirty
					return
				}
			}
		}
	}
}

// Lock should be held.
func (fs *fileStore) resetGlobalPerSubjectInfo() {
	// Clear any global subject state.
	fs.psim = make(map[string]*psi)
	for _, mb := range fs.blks {
		fs.populateGlobalPerSubjectInfo(mb)
	}
}

// Lock should be held.
func (mb *msgBlock) resetPerSubjectInfo() error {
	mb.fss = nil
	mb.removePerSubjectInfoLocked()
	return mb.generatePerSubjectInfo(true)
}

// generatePerSubjectInfo will generate the per subject info via the raw msg block.
func (mb *msgBlock) generatePerSubjectInfo(hasLock bool) error {
	if !hasLock {
		mb.mu.Lock()
		defer mb.mu.Unlock()
	}

	// Check if this mb is empty. This can happen when its the last one and we are holding onto it for seq and timestamp info.
	if mb.msgs == 0 {
		return nil
	}

	if mb.cacheNotLoaded() {
		if err := mb.loadMsgsWithLock(); err != nil {
			return err
		}
	}

	// Create new one regardless.
	mb.fss = make(map[string]*SimpleState)

	var smv StoreMsg
	fseq, lseq := mb.first.seq, mb.last.seq
	for seq := fseq; seq <= lseq; seq++ {
		sm, err := mb.cacheLookup(seq, &smv)
		if err != nil {
			// Since we are walking by sequence we can ignore some errors that are benign to rebuilding our state.
			if err == ErrStoreMsgNotFound || err == errDeletedMsg {
				continue
			}
			if err == errNoCache {
				return nil
			}
			return err
		}
		if sm != nil && len(sm.subj) > 0 {
			if ss := mb.fss[sm.subj]; ss != nil {
				ss.Msgs++
				ss.Last = seq
			} else {
				mb.fss[sm.subj] = &SimpleState{Msgs: 1, First: seq, Last: seq}
			}
			mb.fssNeedsWrite = true
		}
	}

	if len(mb.fss) > 0 {
		// Make sure we run the cache expire timer.
		mb.llts = time.Now().UnixNano()
		mb.startCacheExpireTimer()
	}
	return nil
}

func (mb *msgBlock) loadPerSubjectInfo() ([]byte, error) {
	const (
		fileHashIndex = 16
		mbHashIndex   = 8
		minFileSize   = 24
	)

	buf, err := os.ReadFile(mb.sfn)
	if err != nil {
		return nil, err
	}

	if len(buf) < minFileSize || checkHeader(buf) != nil {
		return nil, errors.New("short fss state")
	}

	// Check that we did not have any bit flips.
	mb.hh.Reset()
	mb.hh.Write(buf[0 : len(buf)-fileHashIndex])
	fhash := buf[len(buf)-fileHashIndex : len(buf)-mbHashIndex]
	if checksum := mb.hh.Sum(nil); !bytes.Equal(checksum, fhash) {
		return nil, errors.New("corrupt fss state")
	}

	// Make sure it matches the last update recorded.
	if !bytes.Equal(buf[len(buf)-mbHashIndex:], mb.lchk[:]) {
		return nil, errors.New("outdated fss state")
	}

	return buf, nil
}

// Helper to make sure fss loaded if we are tracking.
// Lock should be held
func (mb *msgBlock) ensurePerSubjectInfoLoaded() error {
	if mb.fss != nil || mb.noTrack {
		return nil
	}
	if mb.msgs == 0 {
		mb.fss = make(map[string]*SimpleState)
		return nil
	}
	// Load from file.
	return mb.readPerSubjectInfo(true)
}

// Called on recovery to populate the global psim state.
// Lock should be held.
func (fs *fileStore) populateGlobalPerSubjectInfo(mb *msgBlock) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if err := mb.readPerSubjectInfo(true); err != nil {
		return
	}

	// Quick sanity check.
	// TODO(dlc) - This is here to auto-clear a bug.
	fssMsgs := uint64(0)
	for subj, ss := range mb.fss {
		if len(subj) > 0 {
			fssMsgs += ss.Msgs
		}
	}
	// If we are off rebuild.
	if fssMsgs != mb.msgs {
		mb.generatePerSubjectInfo(true)
	}

	// Now populate psim.
	for subj, ss := range mb.fss {
		if len(subj) > 0 {
			if info, ok := fs.psim[subj]; ok {
				info.total += ss.Msgs
				if mb.index > info.lblk {
					info.lblk = mb.index
				}
			} else {
				fs.psim[subj] = &psi{total: ss.Msgs, fblk: mb.index, lblk: mb.index}
			}
		}
	}
}

// readPerSubjectInfo will attempt to restore the per subject information.
func (mb *msgBlock) readPerSubjectInfo(hasLock bool) error {
	if mb.noTrack {
		return nil
	}

	buf, err := mb.loadPerSubjectInfo()
	// On failure re-generate.
	if err != nil {
		return mb.generatePerSubjectInfo(hasLock)
	}

	bi := hdrLen
	readU64 := func() uint64 {
		if bi < 0 {
			return 0
		}
		num, n := binary.Uvarint(buf[bi:])
		if n <= 0 {
			bi = -1
			return 0
		}
		bi += n
		return num
	}

	numEntries := readU64()
	fss := make(map[string]*SimpleState, numEntries)

	if !hasLock {
		mb.mu.Lock()
	}
	for i := uint64(0); i < numEntries; i++ {
		lsubj := readU64()
		// Make a copy or use a configured subject (to avoid mem allocation)
		subj := mb.subjString(buf[bi : bi+int(lsubj)])
		bi += int(lsubj)
		msgs, first, last := readU64(), readU64(), readU64()
		fss[subj] = &SimpleState{Msgs: msgs, First: first, Last: last}
	}
	mb.fss = fss
	mb.fssNeedsWrite = false

	// Make sure we run the cache expire timer.
	if len(mb.fss) > 0 {
		mb.llts = time.Now().UnixNano()
		mb.startCacheExpireTimer()
	}

	if !hasLock {
		mb.mu.Unlock()
	}

	return nil
}

// writePerSubjectInfo will write out per subject information if we are tracking per subject.
// Lock should be held.
func (mb *msgBlock) writePerSubjectInfo() error {
	// Raft groups do not have any subjects.
	if len(mb.fss) == 0 || len(mb.sfn) == 0 || !mb.fssNeedsWrite {
		return nil
	}
	var scratch [4 * binary.MaxVarintLen64]byte
	var b bytes.Buffer
	b.WriteByte(magic)
	b.WriteByte(version)
	n := binary.PutUvarint(scratch[0:], uint64(len(mb.fss)))
	b.Write(scratch[0:n])
	for subj, ss := range mb.fss {
		n := binary.PutUvarint(scratch[0:], uint64(len(subj)))
		b.Write(scratch[0:n])
		b.WriteString(subj)
		// Encode all three parts of our simple state into same scratch buffer.
		n = binary.PutUvarint(scratch[0:], ss.Msgs)
		n += binary.PutUvarint(scratch[n:], ss.First)
		n += binary.PutUvarint(scratch[n:], ss.Last)
		b.Write(scratch[0:n])
	}
	// Calculate hash for this information.
	mb.hh.Reset()
	mb.hh.Write(b.Bytes())
	b.Write(mb.hh.Sum(nil))
	// Now copy over checksum from the block itself, this allows us to know if we are in sync.
	b.Write(mb.lchk[:])

	// Gate this for when we have a large number of blocks expiring at the same time.
	// Since we have the lock we would rather fail here then block.
	// This is an optional structure that can be rebuilt on restart.
	var err error
	select {
	case <-dios:
		if err = os.WriteFile(mb.sfn, b.Bytes(), defaultFilePerms); err == nil {
			// Clear write flag if no error.
			mb.fssNeedsWrite = false
		}
		dios <- struct{}{}
	default:
		err = errDIOStalled
	}

	return err
}

// Close the message block.
func (mb *msgBlock) close(sync bool) {
	if mb == nil {
		return
	}
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if mb.closed {
		return
	}
	mb.closed = true

	// Stop cache expiration timer.
	if mb.ctmr != nil {
		mb.ctmr.Stop()
		mb.ctmr = nil
	}

	// Check if we are tracking by subject.
	if len(mb.fss) > 0 && mb.fssNeedsWrite {
		mb.writePerSubjectInfo()
	}
	mb.fss = nil
	mb.fssNeedsWrite = false

	// Close cache
	mb.clearCacheAndOffset()
	// Quit our loops.
	if mb.qch != nil {
		close(mb.qch)
		mb.qch = nil
	}
	if sync {
		syncAndClose(mb.mfd, mb.ifd)
	} else {
		if mb.mfd != nil {
			mb.mfd.Close()
		}
		if mb.ifd != nil {
			mb.ifd.Close()
		}
	}
	mb.mfd = nil
	mb.ifd = nil
}

func (fs *fileStore) closeAllMsgBlocks(sync bool) {
	for _, mb := range fs.blks {
		mb.close(sync)
	}
}

func (fs *fileStore) Delete() error {
	if fs.isClosed() {
		// Always attempt to remove since we could have been closed beforehand.
		os.RemoveAll(fs.fcfg.StoreDir)
		return ErrStoreClosed
	}
	fs.Purge()

	pdir := filepath.Join(fs.fcfg.StoreDir, purgeDir)
	// If purge directory still exists then we need to wait
	// in place and remove since rename would fail.
	if _, err := os.Stat(pdir); err == nil {
		os.RemoveAll(pdir)
	}

	if err := fs.Stop(); err != nil {
		return err
	}

	err := os.RemoveAll(fs.fcfg.StoreDir)
	if err == nil {
		return nil
	}
	ttl := time.Now().Add(time.Second)
	for time.Now().Before(ttl) {
		time.Sleep(10 * time.Millisecond)
		if err = os.RemoveAll(fs.fcfg.StoreDir); err == nil {
			return nil
		}
	}
	return err
}

// Lock should be held.
func (fs *fileStore) cancelSyncTimer() {
	if fs.syncTmr != nil {
		fs.syncTmr.Stop()
		fs.syncTmr = nil
	}
}

func (fs *fileStore) Stop() error {
	fs.mu.Lock()
	if fs.closed {
		fs.mu.Unlock()
		return ErrStoreClosed
	}
	fs.closed = true
	fs.lmb = nil

	fs.checkAndFlushAllBlocks()
	fs.closeAllMsgBlocks(false)

	fs.cancelSyncTimer()
	fs.cancelAgeChk()

	var _cfs [256]ConsumerStore
	cfs := append(_cfs[:0], fs.cfs...)
	fs.cfs = nil
	fs.mu.Unlock()

	for _, o := range cfs {
		o.Stop()
	}

	return nil
}

const errFile = "errors.txt"

// Stream our snapshot through S2 compression and tar.
func (fs *fileStore) streamSnapshot(w io.WriteCloser, state *StreamState, includeConsumers bool) {
	defer w.Close()

	enc := s2.NewWriter(w)
	defer enc.Close()

	tw := tar.NewWriter(enc)
	defer tw.Close()

	defer func() {
		fs.mu.Lock()
		fs.sips--
		fs.mu.Unlock()
	}()

	modTime := time.Now().UTC()

	writeFile := func(name string, buf []byte) error {
		hdr := &tar.Header{
			Name:    name,
			Mode:    0600,
			ModTime: modTime,
			Uname:   "nats",
			Gname:   "nats",
			Size:    int64(len(buf)),
			Format:  tar.FormatPAX,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := tw.Write(buf); err != nil {
			return err
		}
		return nil
	}

	writeErr := func(err string) {
		writeFile(errFile, []byte(err))
	}

	fs.mu.Lock()
	blks := fs.blks
	// Grab our general meta data.
	// We do this now instead of pulling from files since they could be encrypted.
	meta, err := json.Marshal(fs.cfg)
	if err != nil {
		fs.mu.Unlock()
		writeErr(fmt.Sprintf("Could not gather stream meta file: %v", err))
		return
	}
	fs.hh.Reset()
	fs.hh.Write(meta)
	sum := []byte(hex.EncodeToString(fs.hh.Sum(nil)))
	fs.mu.Unlock()

	// Meta first.
	if writeFile(JetStreamMetaFile, meta) != nil {
		return
	}
	if writeFile(JetStreamMetaFileSum, sum) != nil {
		return
	}

	// Can't use join path here, tar only recognizes relative paths with forward slashes.
	msgPre := msgDir + "/"

	var bbuf []byte

	// Now do messages themselves.
	for _, mb := range blks {
		if mb.pendingWriteSize() > 0 {
			mb.flushPendingMsgs()
		}
		if mb.indexNeedsUpdate() {
			mb.writeIndexInfo()
		}
		mb.mu.Lock()
		buf, err := os.ReadFile(mb.ifn)
		if err != nil {
			mb.mu.Unlock()
			writeErr(fmt.Sprintf("Could not read message block [%d] index file: %v", mb.index, err))
			return
		}
		// Check for encryption.
		if mb.aek != nil && len(buf) > 0 {
			buf, err = mb.aek.Open(buf[:0], mb.nonce, buf, nil)
			if err != nil {
				mb.mu.Unlock()
				writeErr(fmt.Sprintf("Could not decrypt message block [%d] index file: %v", mb.index, err))
				return
			}
		}
		if writeFile(msgPre+fmt.Sprintf(indexScan, mb.index), buf) != nil {
			mb.mu.Unlock()
			return
		}
		// We could stream but don't want to hold the lock and prevent changes, so just read in and
		// release the lock for now.
		bbuf, err = mb.loadBlock(bbuf)
		if err != nil {
			mb.mu.Unlock()
			writeErr(fmt.Sprintf("Could not read message block [%d]: %v", mb.index, err))
			return
		}
		// Check for encryption.
		if mb.bek != nil && len(bbuf) > 0 {
			rbek, err := genBlockEncryptionKey(fs.fcfg.Cipher, mb.seed, mb.nonce)
			if err != nil {
				mb.mu.Unlock()
				writeErr(fmt.Sprintf("Could not create encryption key for message block [%d]: %v", mb.index, err))
				return
			}
			rbek.XORKeyStream(bbuf, bbuf)
		}
		// Make sure we snapshot the per subject info.
		mb.writePerSubjectInfo()
		buf, err = os.ReadFile(mb.sfn)
		// If not there that is ok and not fatal.
		if err == nil && writeFile(msgPre+fmt.Sprintf(fssScan, mb.index), buf) != nil {
			mb.mu.Unlock()
			return
		}
		mb.mu.Unlock()
		// Do this one unlocked.
		if writeFile(msgPre+fmt.Sprintf(blkScan, mb.index), bbuf) != nil {
			return
		}
	}

	// Bail if no consumers requested.
	if !includeConsumers {
		return
	}

	// Do consumers' state last.
	fs.mu.Lock()
	cfs := fs.cfs
	fs.mu.Unlock()

	for _, cs := range cfs {
		o, ok := cs.(*consumerFileStore)
		if !ok {
			continue
		}
		o.mu.Lock()
		// Grab our general meta data.
		// We do this now instead of pulling from files since they could be encrypted.
		meta, err := json.Marshal(o.cfg)
		if err != nil {
			o.mu.Unlock()
			writeErr(fmt.Sprintf("Could not gather consumer meta file for %q: %v", o.name, err))
			return
		}
		o.hh.Reset()
		o.hh.Write(meta)
		sum := []byte(hex.EncodeToString(o.hh.Sum(nil)))

		// We can have the running state directly encoded now.
		state, err := o.encodeState()
		if err != nil {
			o.mu.Unlock()
			writeErr(fmt.Sprintf("Could not encode consumer state for %q: %v", o.name, err))
			return
		}
		odirPre := filepath.Join(consumerDir, o.name)
		o.mu.Unlock()

		// Write all the consumer files.
		if writeFile(filepath.Join(odirPre, JetStreamMetaFile), meta) != nil {
			return
		}
		if writeFile(filepath.Join(odirPre, JetStreamMetaFileSum), sum) != nil {
			return
		}
		writeFile(filepath.Join(odirPre, consumerState), state)
	}
}

// Create a snapshot of this stream and its consumer's state along with messages.
func (fs *fileStore) Snapshot(deadline time.Duration, checkMsgs, includeConsumers bool) (*SnapshotResult, error) {
	fs.mu.Lock()
	if fs.closed {
		fs.mu.Unlock()
		return nil, ErrStoreClosed
	}
	// Only allow one at a time.
	if fs.sips > 0 {
		fs.mu.Unlock()
		return nil, ErrStoreSnapshotInProgress
	}
	// Mark us as snapshotting
	fs.sips += 1
	fs.mu.Unlock()

	if checkMsgs {
		ld := fs.checkMsgs()
		if ld != nil && len(ld.Msgs) > 0 {
			return nil, fmt.Errorf("snapshot check detected %d bad messages", len(ld.Msgs))
		}
	}

	pr, pw := net.Pipe()

	// Set a write deadline here to protect ourselves.
	if deadline > 0 {
		pw.SetWriteDeadline(time.Now().Add(deadline))
	}

	// We can add to our stream while snapshotting but not delete anything.
	var state StreamState
	fs.FastState(&state)

	// Stream in separate Go routine.
	go fs.streamSnapshot(pw, &state, includeConsumers)

	return &SnapshotResult{pr, state}, nil
}

// Helper to return the config.
func (fs *fileStore) fileStoreConfig() FileStoreConfig {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.fcfg
}

////////////////////////////////////////////////////////////////////////////////
// Consumers
////////////////////////////////////////////////////////////////////////////////

type consumerFileStore struct {
	mu      sync.Mutex
	fs      *fileStore
	cfg     *FileConsumerInfo
	prf     keyGen
	aek     cipher.AEAD
	name    string
	odir    string
	ifn     string
	hh      hash.Hash64
	state   ConsumerState
	fch     chan struct{}
	qch     chan struct{}
	flusher bool
	writing bool
	dirty   bool
	closed  bool
}

func (fs *fileStore) ConsumerStore(name string, cfg *ConsumerConfig) (ConsumerStore, error) {
	if fs == nil {
		return nil, fmt.Errorf("filestore is nil")
	}
	if fs.isClosed() {
		return nil, ErrStoreClosed
	}
	if cfg == nil || name == _EMPTY_ {
		return nil, fmt.Errorf("bad consumer config")
	}

	// We now allow overrides from a stream being a filestore type and forcing a consumer to be memory store.
	if cfg.MemoryStorage {
		// Create directly here.
		o := &consumerMemStore{ms: fs, cfg: *cfg}
		fs.AddConsumer(o)
		return o, nil
	}

	odir := filepath.Join(fs.fcfg.StoreDir, consumerDir, name)
	if err := os.MkdirAll(odir, defaultDirPerms); err != nil {
		return nil, fmt.Errorf("could not create consumer directory - %v", err)
	}
	csi := &FileConsumerInfo{Name: name, Created: time.Now().UTC(), ConsumerConfig: *cfg}
	o := &consumerFileStore{
		fs:   fs,
		cfg:  csi,
		prf:  fs.prf,
		name: name,
		odir: odir,
		ifn:  filepath.Join(odir, consumerState),
	}
	key := sha256.Sum256([]byte(fs.cfg.Name + "/" + name))
	hh, err := highwayhash.New64(key[:])
	if err != nil {
		return nil, fmt.Errorf("could not create hash: %v", err)
	}
	o.hh = hh

	// Check for encryption.
	if o.prf != nil {
		if ekey, err := os.ReadFile(filepath.Join(odir, JetStreamMetaFileKey)); err == nil {
			if len(ekey) < minBlkKeySize {
				return nil, errBadKeySize
			}
			// Recover key encryption key.
			rb, err := fs.prf([]byte(fs.cfg.Name + tsep + o.name))
			if err != nil {
				return nil, err
			}

			sc := fs.fcfg.Cipher
			kek, err := genEncryptionKey(sc, rb)
			if err != nil {
				return nil, err
			}
			ns := kek.NonceSize()
			nonce := ekey[:ns]
			seed, err := kek.Open(nil, nonce, ekey[ns:], nil)
			if err != nil {
				// We may be here on a cipher conversion, so attempt to convert.
				if err = o.convertCipher(); err != nil {
					return nil, err
				}
			} else {
				o.aek, err = genEncryptionKey(sc, seed)
			}
			if err != nil {
				return nil, err
			}
		}
	}

	// Track if we are creating the directory so that we can clean up if we encounter an error.
	var didCreate bool

	// Write our meta data iff does not exist.
	meta := filepath.Join(odir, JetStreamMetaFile)
	if _, err := os.Stat(meta); err != nil && os.IsNotExist(err) {
		didCreate = true
		csi.Created = time.Now().UTC()
		if err := o.writeConsumerMeta(); err != nil {
			os.RemoveAll(odir)
			return nil, err
		}
	}

	// If we expect to be encrypted check that what we are restoring is not plaintext.
	// This can happen on snapshot restores or conversions.
	if o.prf != nil {
		keyFile := filepath.Join(odir, JetStreamMetaFileKey)
		if _, err := os.Stat(keyFile); err != nil && os.IsNotExist(err) {
			if err := o.writeConsumerMeta(); err != nil {
				if didCreate {
					os.RemoveAll(odir)
				}
				return nil, err
			}
			// Redo the state file as well here if we have one and we can tell it was plaintext.
			if buf, err := os.ReadFile(o.ifn); err == nil {
				if _, err := decodeConsumerState(buf); err == nil {
					if err := os.WriteFile(o.ifn, o.encryptState(buf), defaultFilePerms); err != nil {
						if didCreate {
							os.RemoveAll(odir)
						}
						return nil, err
					}
				}
			}
		}
	}

	// Create channels to control our flush go routine.
	o.fch = make(chan struct{}, 1)
	o.qch = make(chan struct{})
	go o.flushLoop(o.fch, o.qch)

	fs.AddConsumer(o)

	return o, nil
}

func (o *consumerFileStore) convertCipher() error {
	fs := o.fs
	odir := filepath.Join(fs.fcfg.StoreDir, consumerDir, o.name)

	ekey, err := os.ReadFile(filepath.Join(odir, JetStreamMetaFileKey))
	if err != nil {
		return err
	}
	if len(ekey) < minBlkKeySize {
		return errBadKeySize
	}
	// Recover key encryption key.
	rb, err := fs.prf([]byte(fs.cfg.Name + tsep + o.name))
	if err != nil {
		return err
	}

	// Do these in reverse since converting.
	sc := fs.fcfg.Cipher
	osc := AES
	if sc == AES {
		osc = ChaCha
	}
	kek, err := genEncryptionKey(osc, rb)
	if err != nil {
		return err
	}
	ns := kek.NonceSize()
	nonce := ekey[:ns]
	seed, err := kek.Open(nil, nonce, ekey[ns:], nil)
	if err != nil {
		return err
	}
	aek, err := genEncryptionKey(osc, seed)
	if err != nil {
		return err
	}
	// Now read in and decode our state using the old cipher.
	buf, err := os.ReadFile(o.ifn)
	if err != nil {
		return err
	}
	buf, err = aek.Open(nil, buf[:ns], buf[ns:], nil)
	if err != nil {
		return err
	}

	// Since we are here we recovered our old state.
	// Now write our meta, which will generate the new keys with the new cipher.
	if err := o.writeConsumerMeta(); err != nil {
		return err
	}

	// Now write out or state with the new cipher.
	return o.writeState(buf)
}

// Kick flusher for this consumer.
// Lock should be held.
func (o *consumerFileStore) kickFlusher() {
	if o.fch != nil {
		select {
		case o.fch <- struct{}{}:
		default:
		}
	}
	o.dirty = true
}

// Set in flusher status
func (o *consumerFileStore) setInFlusher() {
	o.mu.Lock()
	o.flusher = true
	o.mu.Unlock()
}

// Clear in flusher status
func (o *consumerFileStore) clearInFlusher() {
	o.mu.Lock()
	o.flusher = false
	o.mu.Unlock()
}

// Report in flusher status
func (o *consumerFileStore) inFlusher() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.flusher
}

// flushLoop watches for consumer updates and the quit channel.
func (o *consumerFileStore) flushLoop(fch, qch chan struct{}) {

	o.setInFlusher()
	defer o.clearInFlusher()

	// Maintain approximately 10 updates per second per consumer under load.
	const minTime = 100 * time.Millisecond
	var lastWrite time.Time
	var dt *time.Timer

	setDelayTimer := func(addWait time.Duration) {
		if dt == nil {
			dt = time.NewTimer(addWait)
			return
		}
		if !dt.Stop() {
			select {
			case <-dt.C:
			default:
			}
		}
		dt.Reset(addWait)
	}

	for {
		select {
		case <-fch:
			if ts := time.Since(lastWrite); ts < minTime {
				setDelayTimer(minTime - ts)
				select {
				case <-dt.C:
				case <-qch:
					return
				}
			}
			o.mu.Lock()
			if o.closed {
				o.mu.Unlock()
				return
			}
			buf, err := o.encodeState()
			o.mu.Unlock()
			if err != nil {
				return
			}
			// TODO(dlc) - if we error should start failing upwards.
			if err := o.writeState(buf); err == nil {
				lastWrite = time.Now()
			}
		case <-qch:
			return
		}
	}
}

// SetStarting sets our starting stream sequence.
func (o *consumerFileStore) SetStarting(sseq uint64) error {
	o.mu.Lock()
	o.state.Delivered.Stream = sseq
	buf, err := o.encodeState()
	o.mu.Unlock()
	if err != nil {
		return err
	}
	return o.writeState(buf)
}

// HasState returns if this store has a recorded state.
func (o *consumerFileStore) HasState() bool {
	o.mu.Lock()
	_, err := os.Stat(o.ifn)
	o.mu.Unlock()
	return err == nil
}

// UpdateDelivered is called whenever a new message has been delivered.
func (o *consumerFileStore) UpdateDelivered(dseq, sseq, dc uint64, ts int64) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if dc != 1 && o.cfg.AckPolicy == AckNone {
		return ErrNoAckPolicy
	}

	// On restarts the old leader may get a replay from the raft logs that are old.
	if dseq <= o.state.AckFloor.Consumer {
		return nil
	}

	// See if we expect an ack for this.
	if o.cfg.AckPolicy != AckNone {
		// Need to create pending records here.
		if o.state.Pending == nil {
			o.state.Pending = make(map[uint64]*Pending)
		}
		var p *Pending
		// Check for an update to a message already delivered.
		if sseq <= o.state.Delivered.Stream {
			if p = o.state.Pending[sseq]; p != nil {
				p.Sequence, p.Timestamp = dseq, ts
			}
		} else {
			// Add to pending.
			o.state.Pending[sseq] = &Pending{dseq, ts}
		}
		// Update delivered as needed.
		if dseq > o.state.Delivered.Consumer {
			o.state.Delivered.Consumer = dseq
		}
		if sseq > o.state.Delivered.Stream {
			o.state.Delivered.Stream = sseq
		}

		if dc > 1 {
			if o.state.Redelivered == nil {
				o.state.Redelivered = make(map[uint64]uint64)
			}
			o.state.Redelivered[sseq] = dc - 1
		}
	} else {
		// For AckNone just update delivered and ackfloor at the same time.
		o.state.Delivered.Consumer = dseq
		o.state.Delivered.Stream = sseq
		o.state.AckFloor.Consumer = dseq
		o.state.AckFloor.Stream = sseq
	}
	// Make sure we flush to disk.
	o.kickFlusher()

	return nil
}

// UpdateAcks is called whenever a consumer with explicit ack or ack all acks a message.
func (o *consumerFileStore) UpdateAcks(dseq, sseq uint64) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.cfg.AckPolicy == AckNone {
		return ErrNoAckPolicy
	}

	// On restarts the old leader may get a replay from the raft logs that are old.
	if dseq <= o.state.AckFloor.Consumer {
		return nil
	}

	if len(o.state.Pending) == 0 || o.state.Pending[sseq] == nil {
		return ErrStoreMsgNotFound
	}

	// Check for AckAll here.
	if o.cfg.AckPolicy == AckAll {
		sgap := sseq - o.state.AckFloor.Stream
		o.state.AckFloor.Consumer = dseq
		o.state.AckFloor.Stream = sseq
		for seq := sseq; seq > sseq-sgap; seq-- {
			delete(o.state.Pending, seq)
			if len(o.state.Redelivered) > 0 {
				delete(o.state.Redelivered, seq)
			}
		}
		o.kickFlusher()
		return nil
	}

	// AckExplicit

	// First delete from our pending state.
	if p, ok := o.state.Pending[sseq]; ok {
		delete(o.state.Pending, sseq)
		dseq = p.Sequence // Use the original.
	}
	if len(o.state.Pending) == 0 {
		o.state.AckFloor.Consumer = o.state.Delivered.Consumer
		o.state.AckFloor.Stream = o.state.Delivered.Stream
	} else if dseq == o.state.AckFloor.Consumer+1 {
		o.state.AckFloor.Consumer = dseq
		o.state.AckFloor.Stream = sseq

		if o.state.Delivered.Consumer > dseq {
			for ss := sseq + 1; ss <= o.state.Delivered.Stream; ss++ {
				if p, ok := o.state.Pending[ss]; ok {
					if p.Sequence > 0 {
						o.state.AckFloor.Consumer = p.Sequence - 1
						o.state.AckFloor.Stream = ss - 1
					}
					break
				}
			}
		}
	}
	// We do these regardless.
	delete(o.state.Redelivered, sseq)

	o.kickFlusher()
	return nil
}

const seqsHdrSize = 6*binary.MaxVarintLen64 + hdrLen

// Encode our consumer state, version 2.
// Lock should be held.

func (o *consumerFileStore) EncodedState() ([]byte, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.closed {
		return nil, ErrStoreClosed
	}
	return encodeConsumerState(&o.state), nil
}

func (o *consumerFileStore) encodeState() ([]byte, error) {
	if o.closed {
		return nil, ErrStoreClosed
	}
	return encodeConsumerState(&o.state), nil
}

func (o *consumerFileStore) UpdateConfig(cfg *ConsumerConfig) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	// This is mostly unchecked here. We are assuming the upper layers have done sanity checking.
	csi := o.cfg
	csi.ConsumerConfig = *cfg

	return o.writeConsumerMeta()
}

func (o *consumerFileStore) Update(state *ConsumerState) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	// Check to see if this is an outdated update.
	if state.Delivered.Consumer < o.state.Delivered.Consumer || state.AckFloor.Stream < o.state.AckFloor.Stream {
		return nil
	}

	// Sanity checks.
	if state.AckFloor.Consumer > state.Delivered.Consumer {
		return fmt.Errorf("bad ack floor for consumer")
	}
	if state.AckFloor.Stream > state.Delivered.Stream {
		return fmt.Errorf("bad ack floor for stream")
	}

	// Copy to our state.
	var pending map[uint64]*Pending
	var redelivered map[uint64]uint64
	if len(state.Pending) > 0 {
		pending = make(map[uint64]*Pending, len(state.Pending))
		for seq, p := range state.Pending {
			pending[seq] = &Pending{p.Sequence, p.Timestamp}
			if seq <= state.AckFloor.Stream || seq > state.Delivered.Stream {
				return fmt.Errorf("bad pending entry, sequence [%d] out of range", seq)
			}
		}
	}
	if len(state.Redelivered) > 0 {
		redelivered = make(map[uint64]uint64, len(state.Redelivered))
		for seq, dc := range state.Redelivered {
			redelivered[seq] = dc
		}
	}

	o.state.Delivered = state.Delivered
	o.state.AckFloor = state.AckFloor
	o.state.Pending = pending
	o.state.Redelivered = redelivered

	o.kickFlusher()

	return nil
}

// Will encrypt the state with our asset key. Will be a no-op if encryption not enabled.
// Lock should be held.
func (o *consumerFileStore) encryptState(buf []byte) []byte {
	if o.aek == nil {
		return buf
	}
	// TODO(dlc) - Optimize on space usage a bit?
	nonce := make([]byte, o.aek.NonceSize(), o.aek.NonceSize()+len(buf)+o.aek.Overhead())
	mrand.Read(nonce)
	return o.aek.Seal(nonce, nonce, buf, nil)
}

// Used to limit number of disk IO calls in flight since they could all be blocking an OS thread.
// https://github.com/nats-io/nats-server/issues/2742
var dios chan struct{}

// Used to setup our simplistic counting semaphore using buffered channels.
// golang.org's semaphore seemed a bit heavy.
func init() {
	// Limit ourselves to a max of 4 blocking IO calls.
	const nIO = 4
	dios = make(chan struct{}, nIO)
	// Fill it up to start.
	for i := 0; i < nIO; i++ {
		dios <- struct{}{}
	}
}

func (o *consumerFileStore) writeState(buf []byte) error {
	// Check if we have the index file open.
	o.mu.Lock()
	if o.writing || len(buf) == 0 {
		o.mu.Unlock()
		return nil
	}

	// Check on encryption.
	if o.aek != nil {
		buf = o.encryptState(buf)
	}

	o.writing = true
	o.dirty = false
	ifn := o.ifn
	o.mu.Unlock()

	// Lock not held here but we do limit number of outstanding calls that could block OS threads.
	<-dios
	err := os.WriteFile(ifn, buf, defaultFilePerms)
	dios <- struct{}{}

	o.mu.Lock()
	if err != nil {
		o.dirty = true
	}
	o.writing = false
	o.mu.Unlock()

	return err
}

// Will upodate the config. Only used when recovering ephemerals.
func (o *consumerFileStore) updateConfig(cfg ConsumerConfig) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.cfg = &FileConsumerInfo{ConsumerConfig: cfg}
	return o.writeConsumerMeta()
}

// Write out the consumer meta data, i.e. state.
// Lock should be held.
func (cfs *consumerFileStore) writeConsumerMeta() error {
	meta := filepath.Join(cfs.odir, JetStreamMetaFile)
	if _, err := os.Stat(meta); err != nil && !os.IsNotExist(err) {
		return err
	}

	if cfs.prf != nil && cfs.aek == nil {
		fs := cfs.fs
		key, _, _, encrypted, err := fs.genEncryptionKeys(fs.cfg.Name + tsep + cfs.name)
		if err != nil {
			return err
		}
		cfs.aek = key
		keyFile := filepath.Join(cfs.odir, JetStreamMetaFileKey)
		if _, err := os.Stat(keyFile); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.WriteFile(keyFile, encrypted, defaultFilePerms); err != nil {
			return err
		}
	}

	b, err := json.Marshal(cfs.cfg)
	if err != nil {
		return err
	}
	// Encrypt if needed.
	if cfs.aek != nil {
		nonce := make([]byte, cfs.aek.NonceSize(), cfs.aek.NonceSize()+len(b)+cfs.aek.Overhead())
		mrand.Read(nonce)
		b = cfs.aek.Seal(nonce, nonce, b, nil)
	}

	if err := os.WriteFile(meta, b, defaultFilePerms); err != nil {
		return err
	}
	cfs.hh.Reset()
	cfs.hh.Write(b)
	checksum := hex.EncodeToString(cfs.hh.Sum(nil))
	sum := filepath.Join(cfs.odir, JetStreamMetaFileSum)
	if err := os.WriteFile(sum, []byte(checksum), defaultFilePerms); err != nil {
		return err
	}
	return nil
}

// Make sure the header is correct.
func checkHeader(hdr []byte) error {
	if hdr == nil || len(hdr) < 2 || hdr[0] != magic || hdr[1] != version {
		return errCorruptState
	}
	return nil
}

// Consumer version.
func checkConsumerHeader(hdr []byte) (uint8, error) {
	if hdr == nil || len(hdr) < 2 || hdr[0] != magic {
		return 0, errCorruptState
	}
	version := hdr[1]
	switch version {
	case 1, 2:
		return version, nil
	}
	return 0, fmt.Errorf("unsupported version: %d", version)
}

func (o *consumerFileStore) copyPending() map[uint64]*Pending {
	pending := make(map[uint64]*Pending, len(o.state.Pending))
	for seq, p := range o.state.Pending {
		pending[seq] = &Pending{p.Sequence, p.Timestamp}
	}
	return pending
}

func (o *consumerFileStore) copyRedelivered() map[uint64]uint64 {
	redelivered := make(map[uint64]uint64, len(o.state.Redelivered))
	for seq, dc := range o.state.Redelivered {
		redelivered[seq] = dc
	}
	return redelivered
}

// Type returns the type of the underlying store.
func (o *consumerFileStore) Type() StorageType { return FileStorage }

// State retrieves the state from the state file.
// This is not expected to be called in high performance code, only on startup.
func (o *consumerFileStore) State() (*ConsumerState, error) {
	return o.stateWithCopy(true)
}

// This will not copy pending or redelivered, so should only be done under the
// consumer owner's lock.
func (o *consumerFileStore) BorrowState() (*ConsumerState, error) {
	return o.stateWithCopy(false)
}

func (o *consumerFileStore) stateWithCopy(doCopy bool) (*ConsumerState, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.closed {
		return nil, ErrStoreClosed
	}

	state := &ConsumerState{}

	// See if we have a running state or if we need to read in from disk.
	if o.state.Delivered.Consumer != 0 || o.state.Delivered.Stream != 0 {
		state.Delivered = o.state.Delivered
		state.AckFloor = o.state.AckFloor
		if len(o.state.Pending) > 0 {
			if doCopy {
				state.Pending = o.copyPending()
			} else {
				state.Pending = o.state.Pending
			}
		}
		if len(o.state.Redelivered) > 0 {
			if doCopy {
				state.Redelivered = o.copyRedelivered()
			} else {
				state.Redelivered = o.state.Redelivered
			}
		}
		return state, nil
	}

	// Read the state in here from disk..
	buf, err := os.ReadFile(o.ifn)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	if len(buf) == 0 {
		return state, nil
	}

	// Check on encryption.
	if o.aek != nil {
		ns := o.aek.NonceSize()
		buf, err = o.aek.Open(nil, buf[:ns], buf[ns:], nil)
		if err != nil {
			return nil, err
		}
	}

	state, err = decodeConsumerState(buf)
	if err != nil {
		return nil, err
	}

	// Copy this state into our own.
	o.state.Delivered = state.Delivered
	o.state.AckFloor = state.AckFloor
	if len(state.Pending) > 0 {
		if doCopy {
			o.state.Pending = make(map[uint64]*Pending, len(state.Pending))
			for seq, p := range state.Pending {
				o.state.Pending[seq] = &Pending{p.Sequence, p.Timestamp}
			}
		} else {
			o.state.Pending = state.Pending
		}
	}
	if len(state.Redelivered) > 0 {
		if doCopy {
			o.state.Redelivered = make(map[uint64]uint64, len(state.Redelivered))
			for seq, dc := range state.Redelivered {
				o.state.Redelivered[seq] = dc
			}
		} else {
			o.state.Redelivered = state.Redelivered
		}
	}

	return state, nil
}

// Decode consumer state.
func decodeConsumerState(buf []byte) (*ConsumerState, error) {
	version, err := checkConsumerHeader(buf)
	if err != nil {
		return nil, err
	}

	bi := hdrLen
	// Helpers, will set i to -1 on error.
	readSeq := func() uint64 {
		if bi < 0 {
			return 0
		}
		seq, n := binary.Uvarint(buf[bi:])
		if n <= 0 {
			bi = -1
			return 0
		}
		bi += n
		return seq
	}
	readTimeStamp := func() int64 {
		if bi < 0 {
			return 0
		}
		ts, n := binary.Varint(buf[bi:])
		if n <= 0 {
			bi = -1
			return -1
		}
		bi += n
		return ts
	}
	// Just for clarity below.
	readLen := readSeq
	readCount := readSeq

	state := &ConsumerState{}
	state.AckFloor.Consumer = readSeq()
	state.AckFloor.Stream = readSeq()
	state.Delivered.Consumer = readSeq()
	state.Delivered.Stream = readSeq()

	if bi == -1 {
		return nil, errCorruptState
	}
	if version == 1 {
		// Adjust back. Version 1 also stored delivered as next to be delivered,
		// so adjust that back down here.
		if state.AckFloor.Consumer > 1 {
			state.Delivered.Consumer += state.AckFloor.Consumer - 1
		}
		if state.AckFloor.Stream > 1 {
			state.Delivered.Stream += state.AckFloor.Stream - 1
		}
	}

	// We have additional stuff.
	if numPending := readLen(); numPending > 0 {
		mints := readTimeStamp()
		state.Pending = make(map[uint64]*Pending, numPending)
		for i := 0; i < int(numPending); i++ {
			sseq := readSeq()
			var dseq uint64
			if version == 2 {
				dseq = readSeq()
			}
			ts := readTimeStamp()
			// Check the state machine for corruption, not the value which could be -1.
			if bi == -1 {
				return nil, errCorruptState
			}
			// Adjust seq back.
			sseq += state.AckFloor.Stream
			if sseq == 0 {
				return nil, errCorruptState
			}
			if version == 2 {
				dseq += state.AckFloor.Consumer
			}
			// Adjust the timestamp back.
			if version == 1 {
				ts = (ts + mints) * int64(time.Second)
			} else {
				ts = (mints - ts) * int64(time.Second)
			}
			// Store in pending.
			state.Pending[sseq] = &Pending{dseq, ts}
		}
	}

	// We have redelivered entries here.
	if numRedelivered := readLen(); numRedelivered > 0 {
		state.Redelivered = make(map[uint64]uint64, numRedelivered)
		for i := 0; i < int(numRedelivered); i++ {
			if seq, n := readSeq(), readCount(); seq > 0 && n > 0 {
				// Adjust seq back.
				seq += state.AckFloor.Stream
				state.Redelivered[seq] = n
			}
		}
	}

	return state, nil
}

// Stop the processing of the consumers's state.
func (o *consumerFileStore) Stop() error {
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return nil
	}
	if o.qch != nil {
		close(o.qch)
		o.qch = nil
	}

	var err error
	var buf []byte

	if o.dirty {
		// Make sure to write this out..
		if buf, err = o.encodeState(); err == nil && len(buf) > 0 {
			if o.aek != nil {
				buf = o.encryptState(buf)
			}
		}
	}

	o.odir = _EMPTY_
	o.closed = true
	ifn, fs := o.ifn, o.fs
	o.mu.Unlock()

	fs.RemoveConsumer(o)

	if len(buf) > 0 {
		o.waitOnFlusher()
		<-dios
		err = os.WriteFile(ifn, buf, defaultFilePerms)
		dios <- struct{}{}
	}
	return err
}

func (o *consumerFileStore) waitOnFlusher() {
	if !o.inFlusher() {
		return
	}

	timeout := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(timeout) {
		if !o.inFlusher() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Delete the consumer.
func (o *consumerFileStore) Delete() error {
	return o.delete(false)
}

func (o *consumerFileStore) StreamDelete() error {
	return o.delete(true)
}

func (o *consumerFileStore) delete(streamDeleted bool) error {
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return nil
	}
	if o.qch != nil {
		close(o.qch)
		o.qch = nil
	}

	var err error
	odir := o.odir
	o.odir = _EMPTY_
	o.closed = true
	fs := o.fs
	o.mu.Unlock()

	// If our stream was not deleted this will remove the directories.
	if odir != _EMPTY_ && !streamDeleted {
		<-dios
		err = os.RemoveAll(odir)
		dios <- struct{}{}
	}

	if !streamDeleted {
		fs.RemoveConsumer(o)
	}

	return err
}

func (fs *fileStore) AddConsumer(o ConsumerStore) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.cfs = append(fs.cfs, o)
	return nil
}

func (fs *fileStore) RemoveConsumer(o ConsumerStore) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	for i, cfs := range fs.cfs {
		if o == cfs {
			fs.cfs = append(fs.cfs[:i], fs.cfs[i+1:]...)
			break
		}
	}
	return nil
}

////////////////////////////////////////////////////////////////////////////////
// Templates
////////////////////////////////////////////////////////////////////////////////

type templateFileStore struct {
	dir string
	hh  hash.Hash64
}

func newTemplateFileStore(storeDir string) *templateFileStore {
	tdir := filepath.Join(storeDir, tmplsDir)
	key := sha256.Sum256([]byte("templates"))
	hh, err := highwayhash.New64(key[:])
	if err != nil {
		return nil
	}
	return &templateFileStore{dir: tdir, hh: hh}
}

func (ts *templateFileStore) Store(t *streamTemplate) error {
	dir := filepath.Join(ts.dir, t.Name)
	if err := os.MkdirAll(dir, defaultDirPerms); err != nil {
		return fmt.Errorf("could not create templates storage directory for %q- %v", t.Name, err)
	}
	meta := filepath.Join(dir, JetStreamMetaFile)
	if _, err := os.Stat(meta); (err != nil && !os.IsNotExist(err)) || err == nil {
		return err
	}
	t.mu.Lock()
	b, err := json.Marshal(t)
	t.mu.Unlock()
	if err != nil {
		return err
	}
	if err := os.WriteFile(meta, b, defaultFilePerms); err != nil {
		return err
	}
	// FIXME(dlc) - Do checksum
	ts.hh.Reset()
	ts.hh.Write(b)
	checksum := hex.EncodeToString(ts.hh.Sum(nil))
	sum := filepath.Join(dir, JetStreamMetaFileSum)
	if err := os.WriteFile(sum, []byte(checksum), defaultFilePerms); err != nil {
		return err
	}
	return nil
}

func (ts *templateFileStore) Delete(t *streamTemplate) error {
	return os.RemoveAll(filepath.Join(ts.dir, t.Name))
}
