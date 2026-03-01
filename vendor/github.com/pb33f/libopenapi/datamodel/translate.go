package datamodel

import (
	"context"
	"errors"
	"io"
	"runtime"
	"sync"

	"github.com/pb33f/libopenapi/orderedmap"
)

type (
	ActionFunc[T any]                   func(T) error
	TranslateFunc[IN any, OUT any]      func(IN) (OUT, error)
	TranslateSliceFunc[IN any, OUT any] func(int, IN) (OUT, error)
	TranslateMapFunc[IN any, OUT any]   func(IN) (OUT, error)
	ResultFunc[V any]                   func(V) error
)

type continueError struct {
	error
}

var Continue = &continueError{error: errors.New("Continue")}

type indexedResult[OUT any] struct {
	idx    int
	cont   bool
	output OUT
	err    error
}

type pipelineResult[OUT any] struct {
	seq    int
	cont   bool
	output OUT
	err    error
}

// TranslateSliceParallel iterates a slice in parallel and calls translate()
// asynchronously.
// translate() may return `datamodel.Continue` to continue iteration.
// translate() or result() may return `io.EOF` to break iteration.
// Results are provided sequentially to result() in stable order from slice.
func TranslateSliceParallel[IN any, OUT any](in []IN, translate TranslateSliceFunc[IN, OUT], result ActionFunc[OUT]) error {
	if in == nil {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	workers := runtime.NumCPU()
	jobChan := make(chan int, workers)
	// Buffered to len(in) so workers never block on send.
	doneChan := make(chan indexedResult[OUT], len(in))

	// Bounded worker pool.
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case idx, ok := <-jobChan:
					if !ok {
						return
					}
					out, err := translate(idx, in[idx])
					r := indexedResult[OUT]{idx: idx, output: out}
					if err == Continue {
						r.cont = true
					} else if err != nil {
						r.err = err
					}
					doneChan <- r
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	// Enqueue work, then close doneChan after all workers finish.
	go func() {
		defer func() {
			close(jobChan)
			wg.Wait()
			close(doneChan)
		}()
		for i := range in {
			select {
			case jobChan <- i:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Deliver results in stable order using a pending map.
	pending := make(map[int]indexedResult[OUT])
	nextIdx := 0
	for r := range doneChan {
		pending[r.idx] = r
		// Flush contiguous completed results starting from nextIdx.
		for {
			p, ok := pending[nextIdx]
			if !ok {
				break
			}
			delete(pending, nextIdx)
			nextIdx++
			// Check errors first, even when result callback is nil.
			if p.err != nil {
				cancel()
				for range doneChan {
				}
				if p.err == io.EOF {
					return nil
				}
				return p.err
			}
			if p.cont || result == nil {
				continue
			}
			if err := result(p.output); err != nil {
				cancel()
				for range doneChan {
				}
				if err == io.EOF {
					return nil
				}
				return err
			}
		}
	}
	return nil
}

// TranslateMapParallel iterates a `*orderedmap.Map` in parallel and calls translate()
// asynchronously.
// translate() or result() may return `io.EOF` to break iteration.
// Safely handles nil pointer.
// Results are provided sequentially to result() in stable order from `*orderedmap.Map`.
func TranslateMapParallel[K comparable, V any, RV any](m *orderedmap.Map[K, V], translate TranslateFunc[orderedmap.Pair[K, V], RV], result ResultFunc[RV]) error {
	if m == nil {
		return nil
	}

	// Snapshot pairs for indexed access.
	pairs := make([]orderedmap.Pair[K, V], 0, m.Len())
	for pair := orderedmap.First(m); pair != nil; pair = pair.Next() {
		pairs = append(pairs, pair)
	}
	if len(pairs) == 0 {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	workers := runtime.NumCPU()
	jobChan := make(chan int, workers)
	doneChan := make(chan indexedResult[RV], len(pairs))

	// Bounded worker pool.
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case idx, ok := <-jobChan:
					if !ok {
						return
					}
					out, err := translate(pairs[idx])
					r := indexedResult[RV]{idx: idx, output: out}
					if err != nil {
						r.err = err
					}
					doneChan <- r
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	// Enqueue work, then close doneChan after all workers finish.
	go func() {
		defer func() {
			close(jobChan)
			wg.Wait()
			close(doneChan)
		}()
		for i := range pairs {
			select {
			case jobChan <- i:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Deliver results in stable order.
	pending := make(map[int]indexedResult[RV])
	nextIdx := 0
	for r := range doneChan {
		pending[r.idx] = r
		for {
			p, ok := pending[nextIdx]
			if !ok {
				break
			}
			delete(pending, nextIdx)
			nextIdx++
			if p.err != nil {
				cancel()
				for range doneChan {
				}
				if p.err == io.EOF {
					return nil
				}
				return p.err
			}
			if err := result(p.output); err != nil {
				cancel()
				for range doneChan {
				}
				if err == io.EOF {
					return nil
				}
				return err
			}
		}
	}
	return nil
}

type pipelineWork[IN any] struct {
	seq   int
	input IN
}

// TranslatePipeline processes input sequentially through predicate(), sends to
// translate() in parallel, then outputs in stable order.
// translate() may return `datamodel.Continue` to continue iteration.
// Caller must close `in` channel to indicate EOF.
// TranslatePipeline closes `out` channel to indicate EOF.
func TranslatePipeline[IN any, OUT any](in <-chan IN, out chan<- OUT, translate TranslateFunc[IN, OUT]) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	concurrency := runtime.NumCPU()
	workChan := make(chan pipelineWork[IN], concurrency*2)
	resultChan := make(chan pipelineResult[OUT], concurrency*2)
	var reterr error
	var mu sync.Mutex
	var wg sync.WaitGroup
	defer wg.Wait()

	// Launch worker pool.
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case w, ok := <-workChan:
					if !ok {
						return
					}
					result, err := translate(w.input)
					r := pipelineResult[OUT]{seq: w.seq, output: result}
					if err == Continue {
						r.cont = true
					} else if err != nil {
						mu.Lock()
						if reterr == nil {
							reterr = err
						}
						mu.Unlock()
						cancel()
						// Send the error result so the collector can detect it
						r.err = err
						select {
						case resultChan <- r:
						default:
						}
						return
					}
					select {
					case resultChan <- r:
					case <-ctx.Done():
						return
					}
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	// Iterate input, assign sequence numbers, send to workers.
	wg.Add(1)
	go func() {
		defer func() {
			close(workChan)
			wg.Done()
		}()
		seq := 0
		for {
			select {
			case value, ok := <-in:
				if !ok {
					return
				}
				select {
				case workChan <- pipelineWork[IN]{seq: seq, input: value}:
					seq++
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Close resultChan after all workers and the enqueue goroutine finish.
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results in stable order, send to output channel.
	defer close(out)
	pending := make(map[int]pipelineResult[OUT])
	nextSeq := 0
	for r := range resultChan {
		if r.err != nil {
			// Error already stored in reterr by the worker
			return reterr
		}
		pending[r.seq] = r
		for {
			p, ok := pending[nextSeq]
			if !ok {
				break
			}
			delete(pending, nextSeq)
			nextSeq++
			if p.cont {
				continue
			}
			select {
			case out <- p.output:
			case <-ctx.Done():
				return reterr
			}
		}
	}
	return reterr
}
