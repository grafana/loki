// Copyright 2025 Google LLC
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

package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"path"
	"runtime"
	"sort"
	"sync"
	"time"
)

const (
	defaultPartSize      = 16 * 1024 * 1024 // 16 MiB
	minPartSize          = 5 * 1024 * 1024  // 5 MiB
	baseWorkers          = 4
	maxWorkers           = 16
	tmpObjectPrefix      = "gcs-go-sdk-pu-tmp/"
	maxComposeComponents = 32
	defaultMaxRetries    = 3
	defaultBaseDelay     = 100 * time.Millisecond
	defaultMaxDelay      = 5 * time.Second
)

// ParallelUploadConfig holds configuration for Parallel Uploads.
// Setting this config and EnableParallelUpload flag on Writer enables parallel uploads.
// Supported exclusively for gRPC clients. If used with a JSON client, the
// configuration is ignored and a standard upload is performed.
//
// **Note:** This feature is currently experimental and its API surface may change
// in future releases. It is not yet recommended for production use.
type ParallelUploadConfig struct {

	// PartSize is the size of each part to be uploaded in parallel.
	// Defaults to 16MiB. If a value less than 5MiB is provided, it will be
	// automatically increased to 5MiB. The value is automatically rounded up
	// to the nearest multiple of 256KiB.
	PartSize int

	// MaxConcurrency is the number of goroutines to use for uploading parts in parallel.
	// Defaults to a dynamic value based on the number of CPUs (min(4 + NumCPU/2, 16)).
	MaxConcurrency int
}

// defaults fills in values for the configuration options.
func (c *ParallelUploadConfig) defaults() {
	if c.PartSize == 0 {
		c.PartSize = defaultPartSize
	} else if c.PartSize < minPartSize {
		c.PartSize = minPartSize
	}
	// Use a heuristic for the number of workers: start with 4, add 1 for
	// every 2 CPUs, but don't exceed a cap of 16. This provides a
	// balance between parallelism and resource contention.
	if c.MaxConcurrency == 0 {
		c.MaxConcurrency = min(baseWorkers+(runtime.NumCPU()/2), maxWorkers)
	}
}

type pcuSettings struct {
	// bufferPoolSize is the number of PartSize buffers to pool
	// and is set to MaxConcurrency + 1.
	bufferPoolSize int
}

func newPCUSettings(maxConcurrency int) *pcuSettings {
	c := &pcuSettings{}

	if c.bufferPoolSize == 0 {
		c.bufferPoolSize = maxConcurrency + 1
	}
	return c
}

// newPartName creates a unique name for a temporary part to avoid hotspotting.
func newPartName(bucket, prefix, finalName string, partNumber int) string {
	rnd := generateRandomBytes(4)
	return path.Join(prefix, fmt.Sprintf("%x-%s-part-%d", rnd, finalName, partNumber))
}

type pcuState struct {
	ctx      context.Context
	cancel   context.CancelFunc
	w        *Writer
	config   *ParallelUploadConfig
	settings *pcuSettings

	mu sync.Mutex
	// Handles to the uploaded temporary parts, keyed by partNumber.
	partMap map[int]*ObjectHandle
	// Handles to intermediate composite objects, keyed by their object name.
	intermediateMap map[string]*ObjectHandle
	errOnce         sync.Once
	firstErr        error
	errors          []error
	partNum         int
	currentBuffer   []byte
	bytesBuffered   int64
	buffersAlloc    int

	bufferCh    chan []byte
	uploadCh    chan uploadTask
	resultCh    chan uploadResult
	workerWG    sync.WaitGroup
	collectorWG sync.WaitGroup
	started     bool
	closeOnce   sync.Once

	// Function to upload a part; can be overridden for testing.
	uploadPartFn func(s *pcuState, task uploadTask) (*ObjectHandle, *ObjectAttrs, error)
	// Function to delete an object; can be overridden for testing.
	deleteFn func(ctx context.Context, h *ObjectHandle) error
	// Function to perform cleanup; can be overridden for testing.
	doCleanupFn func(s *pcuState)
	// Function to compose parts; can be overridden for testing.
	composePartsFn func(s *pcuState) error
	// Function to run the compose operation; can be overridden for testing.
	composeFn func(ctx context.Context, composer *Composer) (*ObjectAttrs, error)
}

type uploadTask struct {
	partNumber int
	buffer     []byte
	size       int64
}

type uploadResult struct {
	partNumber int
	obj        *ObjectAttrs
	handle     *ObjectHandle
	err        error
	size       int64
}

func (w *Writer) initPCU(ctx context.Context) error {
	if !w.EnableParallelUpload {
		return nil
	}

	if len(w.MD5) > 0 {
		return errors.New("storage: MD5 checksums are not supported with parallel uploads")
	}

	// Sanity check: If these are nil, something has gone fundamentally wrong in the Writer lifecycle.
	if w.o == nil || w.o.c == nil || w.o.bucket == "" {
		return fmt.Errorf("upload requires a non-nil ObjectHandle with a bucket name and a client")
	}

	if err := w.validateWriteAttrs(); err != nil {
		return err
	}
	if w.o.gen != defaultGen {
		return fmt.Errorf("storage: generation supported on Writer for appendable objects only, got %v", w.o.gen)
	}

	cfg := &w.ParallelUploadConfig
	cfg.defaults()

	// Ensure PartSize is a multiple of googleapi.MinUploadChunkSize.
	cfg.PartSize = gRPCChunkSize(cfg.PartSize)

	s := newPCUSettings(cfg.MaxConcurrency)

	// Track PCU operations using client feature tracking header.
	ctx = addFeatureAttributes(ctx, featurePCU)

	pCtx, cancel := context.WithCancel(ctx)

	state := &pcuState{
		ctx:             pCtx,
		cancel:          cancel,
		w:               w,
		config:          cfg,
		settings:        s,
		bufferCh:        make(chan []byte, s.bufferPoolSize),
		uploadCh:        make(chan uploadTask, cfg.MaxConcurrency), // Buffered to prevent worker starvation.
		resultCh:        make(chan uploadResult),
		partMap:         make(map[int]*ObjectHandle),
		intermediateMap: make(map[string]*ObjectHandle),
		uploadPartFn:    (*pcuState).uploadPart,
		deleteFn: func(ctx context.Context, h *ObjectHandle) error {
			return h.Delete(ctx)
		},
		doCleanupFn:    (*pcuState).doCleanup,
		composePartsFn: (*pcuState).composeParts,
		composeFn: func(ctx context.Context, c *Composer) (*ObjectAttrs, error) {
			return c.Run(ctx)
		},
	}
	w.pcu = state

	state.workerWG.Add(cfg.MaxConcurrency)
	for i := 0; i < cfg.MaxConcurrency; i++ {
		go state.worker()
	}

	state.collectorWG.Add(1)
	go state.resultCollector()

	// Handle to get the first buffer.
	state.currentBuffer = make([]byte, cfg.PartSize)
	state.buffersAlloc = 1
	state.bytesBuffered = 0

	state.started = true
	return nil
}

// worker processes upload tasks from upload channel, reporting results
// and returning buffers to the pool.
func (s *pcuState) worker() {
	defer s.workerWG.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case task, ok := <-s.uploadCh:
			if !ok {
				return
			}
			func(t uploadTask) {
				// Ensure the buffer is returned to the pool.
				defer func() {
					select {
					case s.bufferCh <- t.buffer:
					default:
						// If the buffer pool is full, drop the buffer.
						// This is a safety measure to avoid blocking indefinitely.
					}
				}()
				// This handles the case where cancellation happens before we begin upload.
				if err := s.ctx.Err(); err != nil {
					return
				}

				handle, attrs, err := s.uploadPartFn(s, t)

				if err := s.ctx.Err(); err != nil {
					return
				}
				select {
				// Always send a result to the collector if the context is not cancelled.
				case s.resultCh <- uploadResult{partNumber: t.partNumber, obj: attrs, handle: handle, err: err, size: t.size}:
				case <-s.ctx.Done():
				}
			}(task)
		}
	}
}

func (s *pcuState) uploadPart(task uploadTask) (*ObjectHandle, *ObjectAttrs, error) {
	partName := newPartName(s.w.o.bucket, tmpObjectPrefix, s.w.o.object, task.partNumber)
	partHandle := s.w.o.c.Bucket(s.w.o.bucket).Object(partName)

	pw := partHandle.NewWriter(s.ctx)
	pw.ObjectAttrs.Name = partName
	pw.ObjectAttrs.Size = task.size
	pw.DisableAutoChecksum = s.w.DisableAutoChecksum
	pw.ChunkSize = 0 // Force single-shot upload for parts.

	if _, err := pw.Write(task.buffer[:task.size]); err != nil {
		return nil, nil, fmt.Errorf("failed to write part %d: %w", task.partNumber, err)
	}

	if err := pw.Close(); err != nil {
		return nil, nil, fmt.Errorf("failed to close part %d: %w", task.partNumber, err)
	}

	return partHandle, pw.Attrs(), nil
}

func (s *pcuState) resultCollector() {
	defer s.collectorWG.Done()
	var cumulativeSize int64
	for result := range s.resultCh {
		if result.err != nil {
			s.setError(result.err)
		} else if result.handle != nil {
			s.mu.Lock()
			s.partMap[result.partNumber] = result.handle
			s.mu.Unlock()

			cumulativeSize += result.size
			s.w.progress(cumulativeSize)
		} else {
			// Both are nil: this is an impossible state that indicates a logical error.
			// Setting an error to prevent silent data corruption.
			s.setError(fmt.Errorf("upload result missing both error and handle for part %d", result.partNumber))
		}
	}
}

func (s *pcuState) setError(err error) {
	if err == nil || errors.Is(err, context.Canceled) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errors = append(s.errors, err)

	s.errOnce.Do(func() {
		s.firstErr = err
		s.cancel() // Cancel context on first error.
	})
}

func (s *pcuState) write(p []byte) (int, error) {
	if !s.started {
		return 0, fmt.Errorf("pcuState not started")
	}
	s.mu.Lock()
	err := s.firstErr
	s.mu.Unlock()
	if err != nil {
		return 0, err
	}

	total := len(p)
	for len(p) > 0 {
		// Acquire a buffer from the pool if we don't have one.
		if s.currentBuffer == nil {
			// Fail-fast check before taking a new buffer.
			s.mu.Lock()
			err = s.firstErr
			s.mu.Unlock()
			if err != nil {
				return total - len(p), err
			}

			select {
			case <-s.ctx.Done():
				return total - len(p), s.ctx.Err()
			case s.currentBuffer = <-s.bufferCh:
				s.bytesBuffered = 0
			default:
				if s.buffersAlloc < s.settings.bufferPoolSize {
					s.currentBuffer = make([]byte, s.config.PartSize)
					s.buffersAlloc++
					s.bytesBuffered = 0
				} else {
					select {
					case <-s.ctx.Done():
						return total - len(p), s.ctx.Err()
					case s.currentBuffer = <-s.bufferCh:
						s.bytesBuffered = 0
					}
				}
			}
		}

		n := copy(s.currentBuffer[s.bytesBuffered:], p)
		s.bytesBuffered += int64(n)
		p = p[n:]

		// If the buffer is full, dispatch it to a worker.
		if s.bytesBuffered == int64(s.config.PartSize) {
			if err := s.flushCurrentBuffer(); err != nil {
				return total - len(p), err
			}
		}
	}
	return total, nil
}

func (s *pcuState) flushCurrentBuffer() error {
	if s.bytesBuffered == 0 {
		return nil
	}

	// Capture state for the task while under lock, then release immediately.
	s.mu.Lock()
	if s.firstErr != nil {
		s.mu.Unlock()
		return s.firstErr
	}
	s.partNum++
	pNum := s.partNum
	s.mu.Unlock()

	task := uploadTask{
		partNumber: pNum,
		buffer:     s.currentBuffer,
		size:       s.bytesBuffered,
	}
	// Clear current state so the next Write call picks up a fresh buffer.
	s.currentBuffer = nil
	s.bytesBuffered = 0

	// Dispatch the task. Using a select ensures we don't hang indefinitely
	// if the context is cancelled while the upload queue is full.
	select {
	case <-s.ctx.Done():
		// Return buffer to pool if we couldn't dispatch.
		select {
		case s.bufferCh <- task.buffer:
		default:
			// Discard the buffer if we can't return it, to avoid blocking.
		}
		return s.ctx.Err()
	case s.uploadCh <- task:
		return nil
	}
}

func (s *pcuState) close() error {
	if !s.started {
		return nil
	}
	var err error
	s.closeOnce.Do(func() {

		// Flush the final partial buffer if it exists.
		if err := s.flushCurrentBuffer(); err != nil {
			s.setError(err)
		}

		// Wait for workers, then close resultCh.
		// This prevents "send on closed channel" panics.
		close(s.uploadCh)
		s.workerWG.Wait()
		close(s.resultCh)
		s.collectorWG.Wait()

		// Cleanup is always attempted. We do it in the background to not block returning.
		defer func() {
			go s.doCleanupFn(s)
		}()

		s.mu.Lock()
		err = s.firstErr
		s.mu.Unlock()

		if err != nil {
			return
		}

		// If no parts were actually uploaded (e.g. empty file),
		// fall back to a standard empty object creation.
		if len(s.partMap) == 0 {
			ow := s.w.o.NewWriter(s.w.ctx)
			if ow == nil {
				err = fmt.Errorf("failed to create writer for empty object")
				s.setError(err)
				return
			}
			ow.ObjectAttrs = s.w.ObjectAttrs
			err = ow.Close()
			if err != nil {
				s.setError(err)
			}
			return
		}

		// Perform the recursive composition of parts.
		if err = s.composePartsFn(s); err != nil {
			s.setError(err)
			return
		}
	})
	return err
}

// getSortedParts returns the uploaded parts sorted by part number.
func (s *pcuState) getSortedParts() []*ObjectHandle {
	keys := make([]int, 0, len(s.partMap))
	for k := range s.partMap {
		keys = append(keys, k)
	}
	sort.Ints(keys)

	parts := make([]*ObjectHandle, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, s.partMap[k])
	}
	return parts
}

// composeParts performs the multi-level compose operation to create the final object.
func (s *pcuState) composeParts() error {
	finalComps := s.getSortedParts()
	level := 0

	for len(finalComps) > maxComposeComponents {
		level++
		numIntermediates := (len(finalComps) + maxComposeComponents - 1) / maxComposeComponents
		nextLevel := make([]*ObjectHandle, numIntermediates)

		var wg sync.WaitGroup
		// Use a thread-safe way to capture the first error at this level.
		var levelErr error
		var errOnce sync.Once

		for i := 0; i < numIntermediates; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()

				start := idx * maxComposeComponents
				end := min(start+maxComposeComponents, len(finalComps))

				// Level-based naming with hash to prevent exceeding 1024 bytes.
				h := hex.EncodeToString(generateRandomBytes(4))
				compName := path.Join(tmpObjectPrefix, fmt.Sprintf("int-%s-lv%d-%d", h, level, idx))

				interHandle := s.w.o.c.Bucket(s.w.o.bucket).Object(compName)
				composer := interHandle.ComposerFrom(finalComps[start:end]...)

				_, err := s.composeFn(s.ctx, composer)
				if err != nil {
					errOnce.Do(func() { levelErr = err })
					return
				}
				nextLevel[idx] = interHandle
			}(i)
		}
		wg.Wait()
		// Do a batch insert of intermediate handles to the map to minimize lock contention.
		s.mu.Lock()
		for _, h := range nextLevel {
			if h != nil {
				s.intermediateMap[h.object] = h
			}
		}
		s.mu.Unlock()

		if levelErr != nil {
			return levelErr
		}
		finalComps = nextLevel
	}

	// Final Compose.
	composer := s.w.o.ComposerFrom(finalComps...)
	composer.ObjectAttrs = s.w.ObjectAttrs
	composer.KMSKeyName = s.w.ObjectAttrs.KMSKeyName
	composer.SendCRC32C = s.w.SendCRC32C

	attrs, err := s.composeFn(s.ctx, composer)
	if err != nil {
		return err
	}

	// Perform client-side CRC32C validation if a user-provided checksum was specified.
	if s.w.SendCRC32C && s.w.CRC32C != 0 && attrs.CRC32C != s.w.CRC32C {
		return fmt.Errorf("storage: object was uploaded, but its CRC32C (%d) does not match the expected CRC32C (%d)", attrs.CRC32C, s.w.CRC32C)
	}

	s.w.obj = attrs
	return nil
}

func (s *pcuState) doCleanup() {
	if len(s.partMap) == 0 && len(s.intermediateMap) == 0 {
		return
	}

	var wg sync.WaitGroup

	// Semaphore to avoid spawning too many goroutines for deletion.
	sem := make(chan struct{}, s.config.MaxConcurrency)

	runDelete := func(h *ObjectHandle) {
		defer wg.Done()
		sem <- struct{}{}
		defer func() { <-sem }()

		// Use WithoutCancel to ensure cleanup isn't killed by parent context cancellation.
		// Wrap it in a timeout to prevent hanging indefinitely.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), 5*time.Minute)
		defer cancel()
		// Only log cleanup errors here since its best effort and will rely on bucket
		// lifecycle policies if cleanup fails.
		if err := s.deleteFn(cleanupCtx, h); err != nil {
			log.Printf("storage: failed to delete temporary part %q during parallel upload cleanup: %v", h.object, err)
		}
	}

	for _, h := range s.partMap {
		wg.Add(1)
		go runDelete(h)
	}
	for _, h := range s.intermediateMap {
		wg.Add(1)
		go runDelete(h)
	}
	wg.Wait()
}

// Generates size random bytes.
func generateRandomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}
