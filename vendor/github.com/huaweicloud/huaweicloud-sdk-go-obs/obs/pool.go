// Copyright 2019 Huawei Technologies Co.,Ltd.
// Licensed under the Apache License, Version 2.0 (the "License"); you may not use
// this file except in compliance with the License.  You may obtain a copy of the
// License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software distributed
// under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
// CONDITIONS OF ANY KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations under the License.

//nolint:structcheck, unused
package obs

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Future defines interface with function: Get
type Future interface {
	Get() interface{}
}

// FutureResult for task result
type FutureResult struct {
	result     interface{}
	resultChan chan interface{}
	lock       sync.Mutex
}

type panicResult struct {
	presult interface{}
}

func (f *FutureResult) checkPanic() interface{} {
	if r, ok := f.result.(panicResult); ok {
		panic(r.presult)
	}
	return f.result
}

// Get gets the task result
func (f *FutureResult) Get() interface{} {
	if f.resultChan == nil {
		return f.checkPanic()
	}
	f.lock.Lock()
	defer f.lock.Unlock()
	if f.resultChan == nil {
		return f.checkPanic()
	}

	f.result = <-f.resultChan
	close(f.resultChan)
	f.resultChan = nil
	return f.checkPanic()
}

// Task defines interface with function: Run
type Task interface {
	Run() interface{}
}

type funcWrapper struct {
	f func() interface{}
}

func (fw *funcWrapper) Run() interface{} {
	if fw.f != nil {
		return fw.f()
	}
	return nil
}

type taskWrapper struct {
	t Task
	f *FutureResult
}

func (tw *taskWrapper) Run() interface{} {
	if tw.t != nil {
		return tw.t.Run()
	}
	return nil
}

type signalTask struct {
	id string
}

func (signalTask) Run() interface{} {
	return nil
}

type worker struct {
	name      string
	taskQueue chan Task
	wg        *sync.WaitGroup
	pool      *RoutinePool
}

func runTask(t Task) {
	if tw, ok := t.(*taskWrapper); ok {
		defer func() {
			if r := recover(); r != nil {
				tw.f.resultChan <- panicResult{
					presult: r,
				}
			}
		}()
		ret := t.Run()
		tw.f.resultChan <- ret
	} else {
		t.Run()
	}
}

func (*worker) runTask(t Task) {
	runTask(t)
}

func (w *worker) start() {
	go func() {
		defer func() {
			if w.wg != nil {
				w.wg.Done()
			}
		}()
		for {
			task, ok := <-w.taskQueue
			if !ok {
				break
			}
			w.pool.AddCurrentWorkingCnt(1)
			w.runTask(task)
			w.pool.AddCurrentWorkingCnt(-1)
			if w.pool.autoTuneWorker(w) {
				break
			}
		}
	}()
}

func (w *worker) release() {
	w.taskQueue = nil
	w.wg = nil
	w.pool = nil
}

// Pool defines coroutine pool interface
type Pool interface {
	ShutDown()
	Submit(t Task) (Future, error)
	SubmitFunc(f func() interface{}) (Future, error)
	Execute(t Task)
	ExecuteFunc(f func() interface{})
	GetMaxWorkerCnt() int64
	AddMaxWorkerCnt(value int64) int64
	GetCurrentWorkingCnt() int64
	AddCurrentWorkingCnt(value int64) int64
	GetWorkerCnt() int64
	AddWorkerCnt(value int64) int64
	EnableAutoTune()
}

type basicPool struct {
	maxWorkerCnt      int64
	workerCnt         int64
	currentWorkingCnt int64
	isShutDown        int32
}

// ErrTaskInvalid will be returned if the task is nil
var ErrTaskInvalid = errors.New("Task is nil")

func (pool *basicPool) GetCurrentWorkingCnt() int64 {
	return atomic.LoadInt64(&pool.currentWorkingCnt)
}

func (pool *basicPool) AddCurrentWorkingCnt(value int64) int64 {
	return atomic.AddInt64(&pool.currentWorkingCnt, value)
}

func (pool *basicPool) GetWorkerCnt() int64 {
	return atomic.LoadInt64(&pool.workerCnt)
}

func (pool *basicPool) AddWorkerCnt(value int64) int64 {
	return atomic.AddInt64(&pool.workerCnt, value)
}

func (pool *basicPool) GetMaxWorkerCnt() int64 {
	return atomic.LoadInt64(&pool.maxWorkerCnt)
}

func (pool *basicPool) AddMaxWorkerCnt(value int64) int64 {
	return atomic.AddInt64(&pool.maxWorkerCnt, value)
}

func (pool *basicPool) CompareAndSwapCurrentWorkingCnt(oldValue, newValue int64) bool {
	return atomic.CompareAndSwapInt64(&pool.currentWorkingCnt, oldValue, newValue)
}

func (pool *basicPool) EnableAutoTune() {

}

// RoutinePool defines the coroutine pool struct
type RoutinePool struct {
	basicPool
	taskQueue     chan Task
	dispatchQueue chan Task
	workers       map[string]*worker
	cacheCnt      int
	wg            *sync.WaitGroup
	lock          *sync.Mutex
	shutDownWg    *sync.WaitGroup
	autoTune      int32
}

// ErrSubmitTimeout will be returned if submit task timeout when calling SubmitWithTimeout function
var ErrSubmitTimeout = errors.New("Submit task timeout")

// ErrPoolShutDown will be returned if RoutinePool is shutdown
var ErrPoolShutDown = errors.New("RoutinePool is shutdown")

// ErrTaskReject will be returned if submit task is rejected
var ErrTaskReject = errors.New("Submit task is rejected")

var closeQueue = signalTask{id: "closeQueue"}

// NewRoutinePool creates a RoutinePool instance
func NewRoutinePool(maxWorkerCnt, cacheCnt int) Pool {
	if maxWorkerCnt <= 0 {
		maxWorkerCnt = runtime.NumCPU()
	}

	pool := &RoutinePool{
		cacheCnt:   cacheCnt,
		wg:         new(sync.WaitGroup),
		lock:       new(sync.Mutex),
		shutDownWg: new(sync.WaitGroup),
		autoTune:   0,
	}
	pool.isShutDown = 0
	pool.maxWorkerCnt += int64(maxWorkerCnt)
	if pool.cacheCnt <= 0 {
		pool.taskQueue = make(chan Task)
	} else {
		pool.taskQueue = make(chan Task, pool.cacheCnt)
	}
	pool.workers = make(map[string]*worker, pool.maxWorkerCnt)
	// dispatchQueue must not have length
	pool.dispatchQueue = make(chan Task)
	pool.dispatcher()

	return pool
}

// EnableAutoTune sets the autoTune enabled
func (pool *RoutinePool) EnableAutoTune() {
	atomic.StoreInt32(&pool.autoTune, 1)
}

func (pool *RoutinePool) checkStatus(t Task) error {
	if t == nil {
		return ErrTaskInvalid
	}

	if atomic.LoadInt32(&pool.isShutDown) == 1 {
		return ErrPoolShutDown
	}
	return nil
}

func (pool *RoutinePool) dispatcher() {
	pool.shutDownWg.Add(1)
	go func() {
		for {
			task, ok := <-pool.dispatchQueue
			if !ok {
				break
			}

			if task == closeQueue {
				close(pool.taskQueue)
				pool.shutDownWg.Done()
				continue
			}

			if pool.GetWorkerCnt() < pool.GetMaxWorkerCnt() {
				pool.addWorker()
			}

			pool.taskQueue <- task
		}
	}()
}

// AddMaxWorkerCnt sets the maxWorkerCnt field's value and returns it
func (pool *RoutinePool) AddMaxWorkerCnt(value int64) int64 {
	if atomic.LoadInt32(&pool.autoTune) == 1 {
		return pool.basicPool.AddMaxWorkerCnt(value)
	}
	return pool.GetMaxWorkerCnt()
}

func (pool *RoutinePool) addWorker() {
	if atomic.LoadInt32(&pool.autoTune) == 1 {
		pool.lock.Lock()
		defer pool.lock.Unlock()
	}
	w := &worker{}
	w.name = fmt.Sprintf("woker-%d", len(pool.workers))
	w.taskQueue = pool.taskQueue
	w.wg = pool.wg
	pool.AddWorkerCnt(1)
	w.pool = pool
	pool.workers[w.name] = w
	pool.wg.Add(1)
	w.start()
}

func (pool *RoutinePool) autoTuneWorker(w *worker) bool {
	if atomic.LoadInt32(&pool.autoTune) == 0 {
		return false
	}

	if w == nil {
		return false
	}

	workerCnt := pool.GetWorkerCnt()
	maxWorkerCnt := pool.GetMaxWorkerCnt()
	if workerCnt > maxWorkerCnt && atomic.CompareAndSwapInt64(&pool.workerCnt, workerCnt, workerCnt-1) {
		pool.lock.Lock()
		defer pool.lock.Unlock()
		delete(pool.workers, w.name)
		w.wg.Done()
		w.release()
		return true
	}

	return false
}

// ExecuteFunc creates a funcWrapper instance with the specified function and calls the Execute function
func (pool *RoutinePool) ExecuteFunc(f func() interface{}) {
	fw := &funcWrapper{
		f: f,
	}
	pool.Execute(fw)
}

// Execute pushes the specified task to the dispatchQueue
func (pool *RoutinePool) Execute(t Task) {
	if t != nil {
		pool.dispatchQueue <- t
	}
}

// SubmitFunc creates a funcWrapper instance with the specified function and calls the Submit function
func (pool *RoutinePool) SubmitFunc(f func() interface{}) (Future, error) {
	fw := &funcWrapper{
		f: f,
	}
	return pool.Submit(fw)
}

// Submit pushes the specified task to the dispatchQueue, and returns the FutureResult and error info
func (pool *RoutinePool) Submit(t Task) (Future, error) {
	if err := pool.checkStatus(t); err != nil {
		return nil, err
	}
	f := &FutureResult{}
	f.resultChan = make(chan interface{}, 1)
	tw := &taskWrapper{
		t: t,
		f: f,
	}
	pool.dispatchQueue <- tw
	return f, nil
}

// SubmitWithTimeout pushes the specified task to the dispatchQueue, and returns the FutureResult and error info.
// Also takes a timeout value, will return ErrSubmitTimeout if it does't complete within that time.
func (pool *RoutinePool) SubmitWithTimeout(t Task, timeout int64) (Future, error) {
	if timeout <= 0 {
		return pool.Submit(t)
	}
	if err := pool.checkStatus(t); err != nil {
		return nil, err
	}
	timeoutChan := make(chan bool, 1)
	go func() {
		time.Sleep(time.Duration(time.Millisecond * time.Duration(timeout)))
		timeoutChan <- true
		close(timeoutChan)
	}()

	f := &FutureResult{}
	f.resultChan = make(chan interface{}, 1)
	tw := &taskWrapper{
		t: t,
		f: f,
	}
	select {
	case pool.dispatchQueue <- tw:
		return f, nil
	case _, ok := <-timeoutChan:
		if ok {
			return nil, ErrSubmitTimeout
		}
		return nil, ErrSubmitTimeout
	}
}

func (pool *RoutinePool) beforeCloseDispatchQueue() {
	if !atomic.CompareAndSwapInt32(&pool.isShutDown, 0, 1) {
		return
	}
	pool.dispatchQueue <- closeQueue
	pool.wg.Wait()
}

func (pool *RoutinePool) doCloseDispatchQueue() {
	close(pool.dispatchQueue)
	pool.shutDownWg.Wait()
}

// ShutDown closes the RoutinePool instance
func (pool *RoutinePool) ShutDown() {
	pool.beforeCloseDispatchQueue()
	pool.doCloseDispatchQueue()
	for _, w := range pool.workers {
		w.release()
	}
	pool.workers = nil
	pool.taskQueue = nil
	pool.dispatchQueue = nil
}

// NoChanPool defines the coroutine pool struct
type NoChanPool struct {
	basicPool
	wg     *sync.WaitGroup
	tokens chan interface{}
}

// NewNochanPool creates a new NoChanPool instance
func NewNochanPool(maxWorkerCnt int) Pool {
	if maxWorkerCnt <= 0 {
		maxWorkerCnt = runtime.NumCPU()
	}

	pool := &NoChanPool{
		wg:     new(sync.WaitGroup),
		tokens: make(chan interface{}, maxWorkerCnt),
	}
	pool.isShutDown = 0
	pool.AddMaxWorkerCnt(int64(maxWorkerCnt))

	for i := 0; i < maxWorkerCnt; i++ {
		pool.tokens <- struct{}{}
	}

	return pool
}

func (pool *NoChanPool) acquire() {
	<-pool.tokens
}

func (pool *NoChanPool) release() {
	pool.tokens <- 1
}

func (pool *NoChanPool) execute(t Task) {
	pool.wg.Add(1)
	go func() {
		pool.acquire()
		defer func() {
			pool.release()
			pool.wg.Done()
		}()
		runTask(t)
	}()
}

// ShutDown closes the NoChanPool instance
func (pool *NoChanPool) ShutDown() {
	if !atomic.CompareAndSwapInt32(&pool.isShutDown, 0, 1) {
		return
	}
	pool.wg.Wait()
}

// Execute executes the specified task
func (pool *NoChanPool) Execute(t Task) {
	if t != nil {
		pool.execute(t)
	}
}

// ExecuteFunc creates a funcWrapper instance with the specified function and calls the Execute function
func (pool *NoChanPool) ExecuteFunc(f func() interface{}) {
	fw := &funcWrapper{
		f: f,
	}
	pool.Execute(fw)
}

// Submit executes the specified task, and returns the FutureResult and error info
func (pool *NoChanPool) Submit(t Task) (Future, error) {
	if t == nil {
		return nil, ErrTaskInvalid
	}

	f := &FutureResult{}
	f.resultChan = make(chan interface{}, 1)
	tw := &taskWrapper{
		t: t,
		f: f,
	}

	pool.execute(tw)
	return f, nil
}

// SubmitFunc creates a funcWrapper instance with the specified function and calls the Submit function
func (pool *NoChanPool) SubmitFunc(f func() interface{}) (Future, error) {
	fw := &funcWrapper{
		f: f,
	}
	return pool.Submit(fw)
}
