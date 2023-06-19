// Copyright 2012-2021 The NATS Authors
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
	"bytes"
	"container/heap"
	"container/list"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nkeys"

	"github.com/nats-io/jwt/v2" // only used to decode, not for storage
)

const (
	fileExtension = ".jwt"
)

// validatePathExists checks that the provided path exists and is a dir if requested
func validatePathExists(path string, dir bool) (string, error) {
	if path == _EMPTY_ {
		return _EMPTY_, errors.New("path is not specified")
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return _EMPTY_, fmt.Errorf("error parsing path [%s]: %v", abs, err)
	}

	var finfo os.FileInfo
	if finfo, err = os.Stat(abs); os.IsNotExist(err) {
		return _EMPTY_, fmt.Errorf("the path [%s] doesn't exist", abs)
	}

	mode := finfo.Mode()
	if dir && mode.IsRegular() {
		return _EMPTY_, fmt.Errorf("the path [%s] is not a directory", abs)
	}

	if !dir && mode.IsDir() {
		return _EMPTY_, fmt.Errorf("the path [%s] is not a file", abs)
	}

	return abs, nil
}

// ValidateDirPath checks that the provided path exists and is a dir
func validateDirPath(path string) (string, error) {
	return validatePathExists(path, true)
}

// JWTChanged functions are called when the store file watcher notices a JWT changed
type JWTChanged func(publicKey string)

// DirJWTStore implements the JWT Store interface, keeping JWTs in an optionally sharded
// directory structure
type DirJWTStore struct {
	sync.Mutex
	directory  string
	shard      bool
	readonly   bool
	deleteType deleteType
	operator   map[string]struct{}
	expiration *expirationTracker
	changed    JWTChanged
	deleted    JWTChanged
}

func newDir(dirPath string, create bool) (string, error) {
	fullPath, err := validateDirPath(dirPath)
	if err != nil {
		if !create {
			return _EMPTY_, err
		}
		if err = os.MkdirAll(dirPath, defaultDirPerms); err != nil {
			return _EMPTY_, err
		}
		if fullPath, err = validateDirPath(dirPath); err != nil {
			return _EMPTY_, err
		}
	}
	return fullPath, nil
}

// future proofing in case new options will be added
type dirJWTStoreOption interface{}

// Creates a directory based jwt store.
// Reads files only, does NOT watch directories and files.
func NewImmutableDirJWTStore(dirPath string, shard bool, _ ...dirJWTStoreOption) (*DirJWTStore, error) {
	theStore, err := NewDirJWTStore(dirPath, shard, false, nil)
	if err != nil {
		return nil, err
	}
	theStore.readonly = true
	return theStore, nil
}

// Creates a directory based jwt store.
// Operates on files only, does NOT watch directories and files.
func NewDirJWTStore(dirPath string, shard bool, create bool, _ ...dirJWTStoreOption) (*DirJWTStore, error) {
	fullPath, err := newDir(dirPath, create)
	if err != nil {
		return nil, err
	}
	theStore := &DirJWTStore{
		directory: fullPath,
		shard:     shard,
	}
	return theStore, nil
}

type deleteType int

const (
	NoDelete deleteType = iota
	RenameDeleted
	HardDelete
)

// Creates a directory based jwt store.
//
// When ttl is set deletion of file is based on it and not on the jwt expiration
// To completely disable expiration (including expiration in jwt) set ttl to max duration time.Duration(math.MaxInt64)
//
// limit defines how many files are allowed at any given time. Set to math.MaxInt64 to disable.
// evictOnLimit determines the behavior once limit is reached.
// * true - Evict based on lru strategy
// * false - return an error
func NewExpiringDirJWTStore(dirPath string, shard bool, create bool, delete deleteType, expireCheck time.Duration, limit int64,
	evictOnLimit bool, ttl time.Duration, changeNotification JWTChanged, _ ...dirJWTStoreOption) (*DirJWTStore, error) {
	fullPath, err := newDir(dirPath, create)
	if err != nil {
		return nil, err
	}
	theStore := &DirJWTStore{
		directory:  fullPath,
		shard:      shard,
		deleteType: delete,
		changed:    changeNotification,
	}
	if expireCheck <= 0 {
		if ttl != 0 {
			expireCheck = ttl / 2
		}
		if expireCheck == 0 || expireCheck > time.Minute {
			expireCheck = time.Minute
		}
	}
	if limit <= 0 {
		limit = math.MaxInt64
	}
	theStore.startExpiring(expireCheck, limit, evictOnLimit, ttl)
	theStore.Lock()
	err = filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if strings.HasSuffix(path, fileExtension) {
			if theJwt, err := os.ReadFile(path); err == nil {
				hash := sha256.Sum256(theJwt)
				_, file := filepath.Split(path)
				theStore.expiration.track(strings.TrimSuffix(file, fileExtension), &hash, string(theJwt))
			}
		}
		return nil
	})
	theStore.Unlock()
	if err != nil {
		theStore.Close()
		return nil, err
	}
	return theStore, err
}

func (store *DirJWTStore) IsReadOnly() bool {
	return store.readonly
}

func (store *DirJWTStore) LoadAcc(publicKey string) (string, error) {
	return store.load(publicKey)
}

func (store *DirJWTStore) SaveAcc(publicKey string, theJWT string) error {
	return store.save(publicKey, theJWT)
}

func (store *DirJWTStore) LoadAct(hash string) (string, error) {
	return store.load(hash)
}

func (store *DirJWTStore) SaveAct(hash string, theJWT string) error {
	return store.save(hash, theJWT)
}

func (store *DirJWTStore) Close() {
	store.Lock()
	defer store.Unlock()
	if store.expiration != nil {
		store.expiration.close()
		store.expiration = nil
	}
}

// Pack up to maxJWTs into a package
func (store *DirJWTStore) Pack(maxJWTs int) (string, error) {
	count := 0
	var pack []string
	if maxJWTs > 0 {
		pack = make([]string, 0, maxJWTs)
	} else {
		pack = []string{}
	}
	store.Lock()
	err := filepath.Walk(store.directory, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() && strings.HasSuffix(path, fileExtension) { // this is a JWT
			if count == maxJWTs { // won't match negative
				return nil
			}
			pubKey := strings.TrimSuffix(filepath.Base(path), fileExtension)
			if store.expiration != nil {
				if _, ok := store.expiration.idx[pubKey]; !ok {
					return nil // only include indexed files
				}
			}
			jwtBytes, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if store.expiration != nil {
				claim, err := jwt.DecodeGeneric(string(jwtBytes))
				if err == nil && claim.Expires > 0 && claim.Expires < time.Now().Unix() {
					return nil
				}
			}
			pack = append(pack, fmt.Sprintf("%s|%s", pubKey, string(jwtBytes)))
			count++
		}
		return nil
	})
	store.Unlock()
	if err != nil {
		return _EMPTY_, err
	} else {
		return strings.Join(pack, "\n"), nil
	}
}

// Pack up to maxJWTs into a message and invoke callback with it
func (store *DirJWTStore) PackWalk(maxJWTs int, cb func(partialPackMsg string)) error {
	if maxJWTs <= 0 || cb == nil {
		return errors.New("bad arguments to PackWalk")
	}
	var packMsg []string
	store.Lock()
	dir := store.directory
	exp := store.expiration
	store.Unlock()
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if info != nil && !info.IsDir() && strings.HasSuffix(path, fileExtension) { // this is a JWT
			pubKey := strings.TrimSuffix(filepath.Base(path), fileExtension)
			store.Lock()
			if exp != nil {
				if _, ok := exp.idx[pubKey]; !ok {
					store.Unlock()
					return nil // only include indexed files
				}
			}
			store.Unlock()
			jwtBytes, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if exp != nil {
				claim, err := jwt.DecodeGeneric(string(jwtBytes))
				if err == nil && claim.Expires > 0 && claim.Expires < time.Now().Unix() {
					return nil
				}
			}
			packMsg = append(packMsg, fmt.Sprintf("%s|%s", pubKey, string(jwtBytes)))
			if len(packMsg) == maxJWTs { // won't match negative
				cb(strings.Join(packMsg, "\n"))
				packMsg = nil
			}
		}
		return nil
	})
	if packMsg != nil {
		cb(strings.Join(packMsg, "\n"))
	}
	return err
}

// Merge takes the JWTs from package and adds them to the store
// Merge is destructive in the sense that it doesn't check if the JWT
// is newer or anything like that.
func (store *DirJWTStore) Merge(pack string) error {
	newJWTs := strings.Split(pack, "\n")
	for _, line := range newJWTs {
		if line == _EMPTY_ { // ignore blank lines
			continue
		}
		split := strings.Split(line, "|")
		if len(split) != 2 {
			return fmt.Errorf("line in package didn't contain 2 entries: %q", line)
		}
		pubKey := split[0]
		if !nkeys.IsValidPublicAccountKey(pubKey) {
			return fmt.Errorf("key to merge is not a valid public account key")
		}
		if err := store.saveIfNewer(pubKey, split[1]); err != nil {
			return err
		}
	}
	return nil
}

func (store *DirJWTStore) Reload() error {
	store.Lock()
	exp := store.expiration
	if exp == nil || store.readonly {
		store.Unlock()
		return nil
	}
	idx := exp.idx
	changed := store.changed
	isCache := store.expiration.evictOnLimit
	// clear out indexing data structures
	exp.heap = make([]*jwtItem, 0, len(exp.heap))
	exp.idx = make(map[string]*list.Element)
	exp.lru = list.New()
	exp.hash = [sha256.Size]byte{}
	store.Unlock()
	return filepath.Walk(store.directory, func(path string, info os.FileInfo, err error) error {
		if strings.HasSuffix(path, fileExtension) {
			if theJwt, err := os.ReadFile(path); err == nil {
				hash := sha256.Sum256(theJwt)
				_, file := filepath.Split(path)
				pkey := strings.TrimSuffix(file, fileExtension)
				notify := isCache // for cache, issue cb even when file not present (may have been evicted)
				if i, ok := idx[pkey]; ok {
					notify = !bytes.Equal(i.Value.(*jwtItem).hash[:], hash[:])
				}
				store.Lock()
				exp.track(pkey, &hash, string(theJwt))
				store.Unlock()
				if notify && changed != nil {
					changed(pkey)
				}
			}
		}
		return nil
	})
}

func (store *DirJWTStore) pathForKey(publicKey string) string {
	if len(publicKey) < 2 {
		return _EMPTY_
	}
	if !nkeys.IsValidPublicKey(publicKey) {
		return _EMPTY_
	}
	fileName := fmt.Sprintf("%s%s", publicKey, fileExtension)
	if store.shard {
		last := publicKey[len(publicKey)-2:]
		return filepath.Join(store.directory, last, fileName)
	} else {
		return filepath.Join(store.directory, fileName)
	}
}

// Load checks the memory store and returns the matching JWT or an error
// Assumes lock is NOT held
func (store *DirJWTStore) load(publicKey string) (string, error) {
	store.Lock()
	defer store.Unlock()
	if path := store.pathForKey(publicKey); path == _EMPTY_ {
		return _EMPTY_, fmt.Errorf("invalid public key")
	} else if data, err := os.ReadFile(path); err != nil {
		return _EMPTY_, err
	} else {
		if store.expiration != nil {
			store.expiration.updateTrack(publicKey)
		}
		return string(data), nil
	}
}

// write that keeps hash of all jwt in sync
// Assumes the lock is held. Does return true or an error never both.
func (store *DirJWTStore) write(path string, publicKey string, theJWT string) (bool, error) {
	var newHash *[sha256.Size]byte
	if store.expiration != nil {
		h := sha256.Sum256([]byte(theJWT))
		newHash = &h
		if v, ok := store.expiration.idx[publicKey]; ok {
			store.expiration.updateTrack(publicKey)
			// this write is an update, move to back
			it := v.Value.(*jwtItem)
			oldHash := it.hash[:]
			if bytes.Equal(oldHash, newHash[:]) {
				return false, nil
			}
		} else if int64(store.expiration.Len()) >= store.expiration.limit {
			if !store.expiration.evictOnLimit {
				return false, errors.New("jwt store is full")
			}
			// this write is an add, pick the least recently used value for removal
			i := store.expiration.lru.Front().Value.(*jwtItem)
			if err := os.Remove(store.pathForKey(i.publicKey)); err != nil {
				return false, err
			} else {
				store.expiration.unTrack(i.publicKey)
			}
		}
	}
	if err := os.WriteFile(path, []byte(theJWT), defaultFilePerms); err != nil {
		return false, err
	} else if store.expiration != nil {
		store.expiration.track(publicKey, newHash, theJWT)
	}
	return true, nil
}

func (store *DirJWTStore) delete(publicKey string) error {
	if store.readonly {
		return fmt.Errorf("store is read-only")
	} else if store.deleteType == NoDelete {
		return fmt.Errorf("store is not set up to for delete")
	}
	store.Lock()
	defer store.Unlock()
	name := store.pathForKey(publicKey)
	if store.deleteType == RenameDeleted {
		if err := os.Rename(name, name+".deleted"); err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
	} else if err := os.Remove(name); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	store.expiration.unTrack(publicKey)
	store.deleted(publicKey)
	return nil
}

// Save puts the JWT in a map by public key and performs update callbacks
// Assumes lock is NOT held
func (store *DirJWTStore) save(publicKey string, theJWT string) error {
	if store.readonly {
		return fmt.Errorf("store is read-only")
	}
	store.Lock()
	path := store.pathForKey(publicKey)
	if path == _EMPTY_ {
		store.Unlock()
		return fmt.Errorf("invalid public key")
	}
	dirPath := filepath.Dir(path)
	if _, err := validateDirPath(dirPath); err != nil {
		if err := os.MkdirAll(dirPath, defaultDirPerms); err != nil {
			store.Unlock()
			return err
		}
	}
	changed, err := store.write(path, publicKey, theJWT)
	cb := store.changed
	store.Unlock()
	if changed && cb != nil {
		cb(publicKey)
	}
	return err
}

// Assumes the lock is NOT held, and only updates if the jwt is new, or the one on disk is older
// When changed, invokes jwt changed callback
func (store *DirJWTStore) saveIfNewer(publicKey string, theJWT string) error {
	if store.readonly {
		return fmt.Errorf("store is read-only")
	}
	path := store.pathForKey(publicKey)
	if path == _EMPTY_ {
		return fmt.Errorf("invalid public key")
	}
	dirPath := filepath.Dir(path)
	if _, err := validateDirPath(dirPath); err != nil {
		if err := os.MkdirAll(dirPath, defaultDirPerms); err != nil {
			return err
		}
	}
	if _, err := os.Stat(path); err == nil {
		if newJWT, err := jwt.DecodeGeneric(theJWT); err != nil {
			return err
		} else if existing, err := os.ReadFile(path); err != nil {
			return err
		} else if existingJWT, err := jwt.DecodeGeneric(string(existing)); err != nil {
			// skip if it can't be decoded
		} else if existingJWT.ID == newJWT.ID {
			return nil
		} else if existingJWT.IssuedAt > newJWT.IssuedAt {
			return nil
		} else if newJWT.Subject != publicKey {
			return fmt.Errorf("jwt subject nkey and provided nkey do not match")
		} else if existingJWT.Subject != newJWT.Subject {
			return fmt.Errorf("subject of existing and new jwt do not match")
		}
	}
	store.Lock()
	cb := store.changed
	changed, err := store.write(path, publicKey, theJWT)
	store.Unlock()
	if err != nil {
		return err
	} else if changed && cb != nil {
		cb(publicKey)
	}
	return nil
}

func xorAssign(lVal *[sha256.Size]byte, rVal [sha256.Size]byte) {
	for i := range rVal {
		(*lVal)[i] ^= rVal[i]
	}
}

// returns a hash representing all indexed jwt
func (store *DirJWTStore) Hash() [sha256.Size]byte {
	store.Lock()
	defer store.Unlock()
	if store.expiration == nil {
		return [sha256.Size]byte{}
	} else {
		return store.expiration.hash
	}
}

// An jwtItem is something managed by the priority queue
type jwtItem struct {
	index      int
	publicKey  string
	expiration int64 // consists of unix time of expiration (ttl when set or jwt expiration) in seconds
	hash       [sha256.Size]byte
}

// A expirationTracker implements heap.Interface and holds Items.
type expirationTracker struct {
	heap         []*jwtItem // sorted by jwtItem.expiration
	idx          map[string]*list.Element
	lru          *list.List // keep which jwt are least used
	limit        int64      // limit how many jwt are being tracked
	evictOnLimit bool       // when limit is hit, error or evict using lru
	ttl          time.Duration
	hash         [sha256.Size]byte // xor of all jwtItem.hash in idx
	quit         chan struct{}
	wg           sync.WaitGroup
}

func (q *expirationTracker) Len() int { return len(q.heap) }

func (q *expirationTracker) Less(i, j int) bool {
	pq := q.heap
	return pq[i].expiration < pq[j].expiration
}

func (q *expirationTracker) Swap(i, j int) {
	pq := q.heap
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (q *expirationTracker) Push(x interface{}) {
	n := len(q.heap)
	item := x.(*jwtItem)
	item.index = n
	q.heap = append(q.heap, item)
	q.idx[item.publicKey] = q.lru.PushBack(item)
}

func (q *expirationTracker) Pop() interface{} {
	old := q.heap
	n := len(old)
	item := old[n-1]
	old[n-1] = nil // avoid memory leak
	item.index = -1
	q.heap = old[0 : n-1]
	q.lru.Remove(q.idx[item.publicKey])
	delete(q.idx, item.publicKey)
	return item
}

func (pq *expirationTracker) updateTrack(publicKey string) {
	if e, ok := pq.idx[publicKey]; ok {
		i := e.Value.(*jwtItem)
		if pq.ttl != 0 {
			// only update expiration when set
			i.expiration = time.Now().Add(pq.ttl).UnixNano()
			heap.Fix(pq, i.index)
		}
		if pq.evictOnLimit {
			pq.lru.MoveToBack(e)
		}
	}
}

func (pq *expirationTracker) unTrack(publicKey string) {
	if it, ok := pq.idx[publicKey]; ok {
		xorAssign(&pq.hash, it.Value.(*jwtItem).hash)
		heap.Remove(pq, it.Value.(*jwtItem).index)
		delete(pq.idx, publicKey)
	}
}

func (pq *expirationTracker) track(publicKey string, hash *[sha256.Size]byte, theJWT string) {
	var exp int64
	// prioritize ttl over expiration
	if pq.ttl != 0 {
		if pq.ttl == time.Duration(math.MaxInt64) {
			exp = math.MaxInt64
		} else {
			exp = time.Now().Add(pq.ttl).UnixNano()
		}
	} else {
		if g, err := jwt.DecodeGeneric(theJWT); err == nil {
			exp = time.Unix(g.Expires, 0).UnixNano()
		}
		if exp == 0 {
			exp = math.MaxInt64 // default to indefinite
		}
	}
	if e, ok := pq.idx[publicKey]; ok {
		i := e.Value.(*jwtItem)
		xorAssign(&pq.hash, i.hash) // remove old hash
		i.expiration = exp
		i.hash = *hash
		heap.Fix(pq, i.index)
	} else {
		heap.Push(pq, &jwtItem{-1, publicKey, exp, *hash})
	}
	xorAssign(&pq.hash, *hash) // add in new hash
}

func (pq *expirationTracker) close() {
	if pq == nil || pq.quit == nil {
		return
	}
	close(pq.quit)
	pq.quit = nil
}

func (store *DirJWTStore) startExpiring(reCheck time.Duration, limit int64, evictOnLimit bool, ttl time.Duration) {
	store.Lock()
	defer store.Unlock()
	quit := make(chan struct{})
	pq := &expirationTracker{
		make([]*jwtItem, 0, 10),
		make(map[string]*list.Element),
		list.New(),
		limit,
		evictOnLimit,
		ttl,
		[sha256.Size]byte{},
		quit,
		sync.WaitGroup{},
	}
	store.expiration = pq
	pq.wg.Add(1)
	go func() {
		t := time.NewTicker(reCheck)
		defer t.Stop()
		defer pq.wg.Done()
		for {
			now := time.Now().UnixNano()
			store.Lock()
			if pq.Len() > 0 {
				if it := pq.heap[0]; it.expiration <= now {
					path := store.pathForKey(it.publicKey)
					if err := os.Remove(path); err == nil {
						heap.Pop(pq)
						pq.unTrack(it.publicKey)
						xorAssign(&pq.hash, it.hash)
						store.Unlock()
						continue // we removed an entry, check next one right away
					}
				}
			}
			store.Unlock()
			select {
			case <-t.C:
			case <-quit:
				return
			}
		}
	}()
}
