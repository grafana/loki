//
// Package dque is a fast embedded durable queue for Go
//
package dque

//
// Copyright (c) 2018 Jon Carlson.  All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.
//

import (
	"strconv"
	"sync"

	"github.com/gofrs/flock"
	"github.com/pkg/errors"

	"io/ioutil"
	"math"
	"os"
	"path"
	"regexp"
)

const lockFile = "lock.lock"

// ErrQueueClosed is the error returned when a queue is closed.
var ErrQueueClosed = errors.New("queue is closed")

var (
	filePattern *regexp.Regexp

	// ErrEmpty is returned when attempting to dequeue from an empty queue.
	ErrEmpty = errors.New("dque is empty")
)

func init() {
	filePattern, _ = regexp.Compile(`^([0-9]+)\.dque$`)
}

type config struct {
	ItemsPerSegment int
}

// DQue is the in-memory representation of a queue on disk.  You must never have
// two *active* DQue instances pointing at the same path on disk.  It is
// acceptable to reconstitute a new instance from disk, but make sure the old
// instance is never enqueued to (or dequeued from) again.
type DQue struct {
	Name    string
	DirPath string
	config  config

	fullPath     string
	fileLock     *flock.Flock
	firstSegment *qSegment
	lastSegment  *qSegment
	builder      func() interface{} // builds a structure to load via gob

	mutex sync.Mutex

	emptyCond *sync.Cond

	turbo bool
}

// New creates a new durable queue
func New(name string, dirPath string, itemsPerSegment int, builder func() interface{}) (*DQue, error) {

	// Validation
	if len(name) == 0 {
		return nil, errors.New("the queue name requires a value")
	}
	if len(dirPath) == 0 {
		return nil, errors.New("the queue directory requires a value")
	}
	if !dirExists(dirPath) {
		return nil, errors.New("the given queue directory is not valid: " + dirPath)
	}
	fullPath := path.Join(dirPath, name)
	if dirExists(fullPath) {
		return nil, errors.New("the given queue directory already exists: " + fullPath + ". Use Open instead")
	}

	if err := os.Mkdir(fullPath, 0755); err != nil {
		return nil, errors.Wrap(err, "error creating queue directory "+fullPath)
	}

	q := DQue{Name: name, DirPath: dirPath}
	q.fullPath = fullPath
	q.config.ItemsPerSegment = itemsPerSegment
	q.builder = builder
	q.emptyCond = sync.NewCond(&q.mutex)

	if err := q.lock(); err != nil {
		return nil, err
	}

	if err := q.load(); err != nil {
		er := q.fileLock.Unlock()
		if er != nil {
			return nil, er
		}
		return nil, err
	}

	return &q, nil
}

// Open opens an existing durable queue.
func Open(name string, dirPath string, itemsPerSegment int, builder func() interface{}) (*DQue, error) {

	// Validation
	if len(name) == 0 {
		return nil, errors.New("the queue name requires a value")
	}
	if len(dirPath) == 0 {
		return nil, errors.New("the queue directory requires a value")
	}
	if !dirExists(dirPath) {
		return nil, errors.New("the given queue directory is not valid (" + dirPath + ")")
	}
	fullPath := path.Join(dirPath, name)
	if !dirExists(fullPath) {
		return nil, errors.New("the given queue does not exist (" + fullPath + ")")
	}

	q := DQue{Name: name, DirPath: dirPath}
	q.fullPath = fullPath
	q.config.ItemsPerSegment = itemsPerSegment
	q.builder = builder
	q.emptyCond = sync.NewCond(&q.mutex)

	if err := q.lock(); err != nil {
		return nil, err
	}

	if err := q.load(); err != nil {
		er := q.fileLock.Unlock()
		if er != nil {
			return nil, er
		}
		return nil, err
	}

	return &q, nil
}

// NewOrOpen either creates a new queue or opens an existing durable queue.
func NewOrOpen(name string, dirPath string, itemsPerSegment int, builder func() interface{}) (*DQue, error) {

	// Validation
	if len(name) == 0 {
		return nil, errors.New("the queue name requires a value")
	}
	if len(dirPath) == 0 {
		return nil, errors.New("the queue directory requires a value")
	}
	if !dirExists(dirPath) {
		return nil, errors.New("the given queue directory is not valid (" + dirPath + ")")
	}
	fullPath := path.Join(dirPath, name)
	if dirExists(fullPath) {
		return Open(name, dirPath, itemsPerSegment, builder)
	}

	return New(name, dirPath, itemsPerSegment, builder)
}

// Close releases the lock on the queue rendering it unusable for further usage by this instance.
// Close will return an error if it has already been called.
func (q *DQue) Close() error {
	// only allow Close while no other function is active
	q.mutex.Lock()
	defer q.mutex.Unlock()

	if q.fileLock == nil {
		return ErrQueueClosed
	}

	err := q.fileLock.Close()
	if err != nil {
		return err
	}

	// Finally mark this instance as closed to prevent any further access
	q.fileLock = nil

	// Wake-up any waiting goroutines for blocking queue access - they should get a ErrQueueClosed
	q.emptyCond.Broadcast()

	// Safe-guard ourself from accidentally using segments after closing the queue
	q.firstSegment = nil
	q.lastSegment = nil

	return nil
}

// Enqueue adds an item to the end of the queue
func (q *DQue) Enqueue(obj interface{}) error {
	// This is heavy-handed but its safe
	q.mutex.Lock()
	defer q.mutex.Unlock()

	if q.fileLock == nil {
		return ErrQueueClosed
	}

	// If this segment is full then create a new one
	if q.lastSegment.sizeOnDisk() >= q.config.ItemsPerSegment {

		// We have filled our last segment to capacity, so create a new one
		seg, err := newQueueSegment(q.fullPath, q.lastSegment.number+1, q.turbo, q.builder)
		if err != nil {
			return errors.Wrapf(err, "error creating new queue segment: %d.", q.lastSegment.number+1)
		}

		// If the last segment is not the first segment
		// then we need to close the file.
		if q.firstSegment != q.lastSegment {
			var err = q.lastSegment.close()
			if err != nil {
				return errors.Wrapf(err, "error closing previous segment file #%d.", q.lastSegment.number)
			}
		}

		// Replace the last segment with the new one
		q.lastSegment = seg

	}

	// Add the object to the last segment
	if err := q.lastSegment.add(obj); err != nil {
		return errors.Wrap(err, "error adding item to the last segment")
	}

	// Wakeup any goroutine that is currently waiting for an item to be enqueued
	q.emptyCond.Broadcast()

	return nil
}

// Dequeue removes and returns the first item in the queue.
// When the queue is empty, nil and dque.ErrEmpty are returned.
func (q *DQue) Dequeue() (interface{}, error) {
	// This is heavy-handed but its safe
	q.mutex.Lock()
	defer q.mutex.Unlock()

	return q.dequeueLocked()
}

func (q *DQue) dequeueLocked() (interface{}, error) {
	if q.fileLock == nil {
		return nil, ErrQueueClosed
	}

	// Remove the first object from the first segment
	obj, err := q.firstSegment.remove()
	if err == errEmptySegment {
		return nil, ErrEmpty
	}
	if err != nil {
		return nil, errors.Wrap(err, "error removing item from the first segment")
	}

	// If this segment is empty and we've reached the max for this segment
	// then delete the file and open the next one.
	if q.firstSegment.size() == 0 &&
		q.firstSegment.sizeOnDisk() >= q.config.ItemsPerSegment {

		// Delete the segment file
		if err := q.firstSegment.delete(); err != nil {
			return obj, errors.Wrap(err, "error deleting queue segment "+q.firstSegment.filePath()+". Queue is in an inconsistent state")
		}

		// We have only one segment and it's now empty so destroy it and
		// create a new one.
		if q.firstSegment.number == q.lastSegment.number {

			// Create the next segment
			seg, err := newQueueSegment(q.fullPath, q.firstSegment.number+1, q.turbo, q.builder)
			if err != nil {
				return obj, errors.Wrap(err, "error creating new segment. Queue is in an inconsistent state")
			}
			q.firstSegment = seg
			q.lastSegment = seg

		} else {

			if q.firstSegment.number+1 == q.lastSegment.number {
				// We have 2 segments, moving down to 1 shared segment
				q.firstSegment = q.lastSegment
			} else {

				// Open the next segment
				seg, err := openQueueSegment(q.fullPath, q.firstSegment.number+1, q.turbo, q.builder)
				if err != nil {
					return obj, errors.Wrap(err, "error creating new segment. Queue is in an inconsistent state")
				}
				q.firstSegment = seg
			}

		}
	}

	return obj, nil
}

// Peek returns the first item in the queue without dequeueing it.
// When the queue is empty, nil and dque.ErrEmpty are returned.
// Do not use this method with multiple dequeueing threads or you may regret it.
func (q *DQue) Peek() (interface{}, error) {
	// This is heavy-handed but it is safe
	q.mutex.Lock()
	defer q.mutex.Unlock()

	return q.peekLocked()
}

func (q *DQue) peekLocked() (interface{}, error) {
	if q.fileLock == nil {
		return nil, ErrQueueClosed
	}

	// Return the first object from the first segment
	obj, err := q.firstSegment.peek()
	if err == errEmptySegment {
		return nil, ErrEmpty
	}
	if err != nil {
		// In reality this will (i.e. should not) never happen
		return nil, errors.Wrap(err, "error getting item from the first segment")
	}

	return obj, nil
}

// DequeueBlock behaves similar to Dequeue, but is a blocking call until an item is available.
func (q *DQue) DequeueBlock() (interface{}, error) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	for {
		obj, err := q.dequeueLocked()
		if err == ErrEmpty {
			q.emptyCond.Wait()
			// Wait() atomically unlocks mutexEmptyCond and suspends execution of the calling goroutine.
			// Receiving the signal does not guarantee an item is available, let's loop and check again.
			continue
		} else if err != nil {
			return nil, err
		}
		return obj, nil
	}
}

// PeekBlock behaves similar to Peek, but is a blocking call until an item is available.
func (q *DQue) PeekBlock() (interface{}, error) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	for {
		obj, err := q.peekLocked()
		if err == ErrEmpty {
			q.emptyCond.Wait()
			// Wait() atomically unlocks mutexEmptyCond and suspends execution of the calling goroutine.
			// Receiving the signal does not guarantee an item is available, let's loop and check again.
			continue
		} else if err != nil {
			return nil, err
		}
		return obj, nil
	}
}

// Size locks things up while calculating so you are guaranteed an accurate
// size... unless you have changed the itemsPerSegment value since the queue
// was last empty.  Then it could be wildly inaccurate.
func (q *DQue) Size() int {
	if q.fileLock == nil {
		return 0
	}

	// This is heavy-handed but it is safe
	q.mutex.Lock()
	defer q.mutex.Unlock()

	return q.SizeUnsafe()
}

// SizeUnsafe returns the approximate number of items in the queue.  Use Size() if
// having the exact size is important to your use-case.
//
// The return value could be wildly inaccurate if the itemsPerSegment value has
// changed since the queue was last empty.
// Also, because this method is not synchronized, the size may change after
// entering this method.
func (q *DQue) SizeUnsafe() int {
	if q.fileLock == nil {
		return 0
	}
	if q.firstSegment.number == q.lastSegment.number {
		return q.firstSegment.size()
	}
	numSegmentsBetween := q.lastSegment.number - q.firstSegment.number - 1
	return q.firstSegment.size() + (numSegmentsBetween * q.config.ItemsPerSegment) + q.lastSegment.size()
}

// SegmentNumbers returns the number of both the first last segmment.
// There is likely no use for this information other than testing.
func (q *DQue) SegmentNumbers() (int, int) {
	if q.fileLock == nil {
		return 0, 0
	}
	return q.firstSegment.number, q.lastSegment.number
}

// Turbo returns true if the turbo flag is on.  Having turbo on speeds things
// up significantly.
func (q *DQue) Turbo() bool {
	return q.turbo
}

// TurboOn allows the filesystem to decide when to sync file changes to disk.
// Throughput is greatly increased by turning turbo on, however there is some
// risk of losing data if a power-loss occurs.
// If turbo is already on an error is returned
func (q *DQue) TurboOn() error {
	// This is heavy-handed but it is safe
	q.mutex.Lock()
	defer q.mutex.Unlock()

	if q.fileLock == nil {
		return ErrQueueClosed
	}

	if q.turbo {
		return errors.New("DQue.TurboOn() is not valid when turbo is on")
	}
	q.turbo = true
	q.firstSegment.turboOn()
	q.lastSegment.turboOn()
	return nil
}

// TurboOff re-enables the "safety" mode that syncs every file change to disk as
// they happen.
// If turbo is already off an error is returned
func (q *DQue) TurboOff() error {
	// This is heavy-handed but it is safe
	q.mutex.Lock()
	defer q.mutex.Unlock()

	if q.fileLock == nil {
		return ErrQueueClosed
	}

	if !q.turbo {
		return errors.New("DQue.TurboOff() is not valid when turbo is off")
	}
	if err := q.firstSegment.turboOff(); err != nil {
		return err
	}
	if err := q.lastSegment.turboOff(); err != nil {
		return err
	}
	q.turbo = false
	return nil
}

// TurboSync allows you to fsync changes to disk, but only if turbo is on.
// If turbo is off an error is returned
func (q *DQue) TurboSync() error {
	// This is heavy-handed but it is safe
	q.mutex.Lock()
	defer q.mutex.Unlock()

	if q.fileLock == nil {
		return ErrQueueClosed
	}
	if !q.turbo {
		return errors.New("DQue.TurboSync() is inappropriate when turbo is off")
	}
	if err := q.firstSegment.turboSync(); err != nil {
		return errors.Wrap(err, "unable to sync changes to disk")
	}
	if err := q.lastSegment.turboSync(); err != nil {
		return errors.Wrap(err, "unable to sync changes to disk")
	}
	return nil
}

// load populates the queue from disk
func (q *DQue) load() error {

	// Find all queue files
	files, err := ioutil.ReadDir(q.fullPath)
	if err != nil {
		return errors.Wrap(err, "unable to read files in "+q.fullPath)
	}

	// Find the smallest and the largest file numbers
	minNum := math.MaxInt32
	maxNum := 0
	for _, f := range files {
		if !f.IsDir() && filePattern.MatchString(f.Name()) {
			// Extract number out of the filename
			fileNumStr := filePattern.FindStringSubmatch(f.Name())[1]
			fileNum, _ := strconv.Atoi(fileNumStr)
			if fileNum > maxNum {
				maxNum = fileNum
			}
			if fileNum < minNum {
				minNum = fileNum
			}
		}
	}

	// If files were found, set q.firstSegment and q.lastSegment
	if maxNum > 0 {

		// We found files
		seg, err := openQueueSegment(q.fullPath, minNum, q.turbo, q.builder)
		if err != nil {
			return errors.Wrap(err, "unable to create queue segment in "+q.fullPath)
		}
		q.firstSegment = seg

		if minNum == maxNum {
			// We have only one segment so the
			// first and last are the same instance (in this case)
			q.lastSegment = q.firstSegment
		} else {
			// We have multiple segments
			seg, err = openQueueSegment(q.fullPath, maxNum, q.turbo, q.builder)
			if err != nil {
				return errors.Wrap(err, "unable to create segment for "+q.fullPath)
			}
			q.lastSegment = seg
		}

	} else {
		// We found no files so build a new queue starting with segment 1
		seg, err := newQueueSegment(q.fullPath, 1, q.turbo, q.builder)
		if err != nil {
			return errors.Wrap(err, "unable to create queue segment in "+q.fullPath)
		}

		// The first and last are the same instance (in this case)
		q.firstSegment = seg
		q.lastSegment = seg
	}

	return nil
}

func (q *DQue) lock() error {
	l := path.Join(q.DirPath, q.Name, lockFile)
	fileLock := flock.New(l)

	locked, err := fileLock.TryLock()
	if err != nil {
		return err
	}
	if !locked {
		return errors.New("failed to acquire flock")
	}

	q.fileLock = fileLock
	return nil
}
