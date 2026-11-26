package compactor

import (
	"context"
	"slices"
	"sync"
	"time"
)

// priorityClass orders the kinds of work of the coupled planner. A lower
// class runs first.
type priorityClass int

const (
	// priorityIndexMerge is the IndexMerge of a coupled merge whose
	// LogMerge finished. It runs first, so the merge commits soon and
	// releases its leases.
	priorityIndexMerge priorityClass = iota

	// priorityPairSplit is the split of an index run into pairs. It runs
	// before every log merge that did not start, because the pairs it
	// writes can then merge.
	priorityPairSplit

	// priorityLogMerge is the LogMerge of a coupled merge.
	priorityLogMerge
)

// taskPriority orders the waits for a task slot.
type taskPriority struct {
	// started marks a task of a job that holds or held a slot. It runs
	// before every task of a job that did not start. So a job finishes soon
	// and releases its leases, and fewer jobs are half done when the tenant
	// stops.
	started bool

	class priorityClass

	// level is the size level of the input pairs of a merge. A lower level
	// runs first, because its merges are cheap and remove the most pairs per
	// byte.
	level int

	// window is the ToC window of the work. An older window runs first,
	// because it gets no more data and its compaction is final.
	window time.Time

	// planOrder is the order in which the compactor planned the job of the
	// task. An earlier job runs first, so all tasks of one job keep the
	// place of the job.
	planOrder uint64
}

// before reports whether p runs before other. It reports false for equal
// priorities.
func (p taskPriority) before(other taskPriority) bool {
	if p.started != other.started {
		return p.started
	}
	if p.class != other.class {
		return p.class < other.class
	}
	if p.level != other.level {
		return p.level < other.level
	}
	if !p.window.Equal(other.window) {
		return p.window.Before(other.window)
	}
	return p.planOrder < other.planOrder
}

// taskSlots hands out a fixed number of task slots by priority. When a slot
// frees, the wait with the highest priority gets it. Waits of equal priority
// get slots in arrival order.
//
// taskSlots is safe for concurrent use.
type taskSlots struct {
	// unlimited admits every wait at once. Its slots have no other state.
	unlimited bool

	slotsMu sync.Mutex
	free    int
	waits   slotWaits
}

// newTaskSlots returns slots for limit tasks at once. A limit of 0 or less
// means no limit.
func newTaskSlots(limit int) *taskSlots {
	return &taskSlots{unlimited: limit <= 0, free: limit}
}

// startJob blocks until it gets the first slot of a job of priority p, or
// until ctx is done. The returned jobSlots holds the slot until the first
// task of the job takes it. The caller must call releaseHeld when the job
// ends.
//
// startJob returns ctx.Err() if ctx is done, even when a slot was free. A
// job that did not start must not start after a shutdown.
func (s *taskSlots) startJob(ctx context.Context, p taskPriority) (*jobSlots, error) {
	release, err := s.acquire(ctx, p)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	p.started = true
	return &jobSlots{slots: s, priority: p, held: release}, nil
}

// acquire blocks until it gets a slot for a task of priority p, or until ctx
// is done. On success the caller must call release once, when the task ends.
func (s *taskSlots) acquire(ctx context.Context, p taskPriority) (release func(), err error) {
	if s.unlimited {
		return func() {}, nil
	}

	s.slotsMu.Lock()
	if s.free > 0 && len(s.waits) == 0 {
		s.free--
		s.slotsMu.Unlock()
		return s.releaseOnce(), nil
	}
	wait := &slotWait{priority: p, granted: make(chan struct{})}
	s.waits.push(wait)
	s.slotsMu.Unlock()

	select {
	case <-wait.granted:
		return s.releaseOnce(), nil
	case <-ctx.Done():
	}

	s.slotsMu.Lock()
	select {
	case <-wait.granted:
		// release granted the slot after ctx was done. Pass it on.
		s.slotsMu.Unlock()
		s.release()
	default:
		s.waits.remove(wait)
		s.slotsMu.Unlock()
	}
	return nil, ctx.Err()
}

func (s *taskSlots) releaseOnce() func() {
	var once sync.Once
	return func() { once.Do(s.release) }
}

// release gives a slot to the wait with the highest priority, or frees it.
func (s *taskSlots) release() {
	s.slotsMu.Lock()
	defer s.slotsMu.Unlock()
	wait, ok := s.waits.pop()
	if !ok {
		s.free++
		return
	}
	close(wait.granted)
}

// jobSlots admits the tasks of one job that started.
//
// The job holds one slot from its start until its first task takes it. Every
// task of the job waits with the started priority of the job. So a job that
// started never waits behind a job that did not, and it keeps its place
// among the jobs that started.
//
// jobSlots is safe for concurrent use. Build it with [taskSlots.startJob].
type jobSlots struct {
	slots *taskSlots

	// priority is the priority of the job. started is set.
	priority taskPriority

	heldMu sync.Mutex
	// held releases the slot that the job holds and no task took yet. It is
	// nil when the job holds no such slot.
	held func()
}

// admit returns the admission of tasks of class. The first task to call it
// takes the held slot.
func (j *jobSlots) admit(class priorityClass) admitFunc {
	p := j.priority
	p.class = class
	return func(ctx context.Context) (func(), error) {
		if release := j.takeHeld(); release != nil {
			return release, nil
		}
		return j.slots.acquire(ctx, p)
	}
}

func (j *jobSlots) takeHeld() func() {
	j.heldMu.Lock()
	defer j.heldMu.Unlock()
	release := j.held
	j.held = nil
	return release
}

// releaseHeld releases the held slot if no task took it.
func (j *jobSlots) releaseHeld() {
	if release := j.takeHeld(); release != nil {
		release()
	}
}

// slotWait is one wait for a task slot.
type slotWait struct {
	priority taskPriority
	// granted is closed when the wait gets a slot.
	granted chan struct{}
}

// slotWaits holds the waits in the order they get slots. Waits of equal
// priority keep their arrival order.
//
// It is a sorted slice, not a heap. A push costs O(n), which is fine for the
// few hundred waits of one tenant, and keeps the code free of the type
// assertions of container/heap.
type slotWaits []*slotWait

// push adds wait after every wait that does not run after it.
func (w *slotWaits) push(wait *slotWait) {
	i, _ := slices.BinarySearchFunc(*w, wait, func(queued, target *slotWait) int {
		if target.priority.before(queued.priority) {
			return 1
		}
		return -1
	})
	*w = slices.Insert(*w, i, wait)
}

// pop removes and returns the first wait. ok is false if w is empty.
func (w *slotWaits) pop() (wait *slotWait, ok bool) {
	if len(*w) == 0 {
		return nil, false
	}
	wait = (*w)[0]
	*w = slices.Delete(*w, 0, 1)
	return wait, true
}

// remove removes wait if w holds it.
func (w *slotWaits) remove(wait *slotWait) {
	if i := slices.Index(*w, wait); i >= 0 {
		*w = slices.Delete(*w, i, i+1)
	}
}
