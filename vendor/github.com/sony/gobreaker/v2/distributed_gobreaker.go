package gobreaker

import (
	"encoding/json"
	"errors"
	"time"
)

var (
	// ErrNoSharedStore is returned when there is no shared store.
	ErrNoSharedStore = errors.New("no shared store")
	// ErrNoSharedState is returned when there is no shared state.
	ErrNoSharedState = errors.New("no shared state")
)

// SharedState represents the shared state of DistributedCircuitBreaker.
type SharedState struct {
	State      State     `json:"state"`
	Generation uint64    `json:"generation"`
	Counts     Counts    `json:"counts"`
	Expiry     time.Time `json:"expiry"`
}

// SharedDataStore stores the shared state of DistributedCircuitBreaker.
type SharedDataStore interface {
	Lock(name string) error
	Unlock(name string) error
	GetData(name string) ([]byte, error)
	SetData(name string, data []byte) error
}

// DistributedCircuitBreaker extends CircuitBreaker with SharedDataStore.
type DistributedCircuitBreaker[T any] struct {
	*CircuitBreaker[T]
	store SharedDataStore
}

// NewDistributedCircuitBreaker returns a new DistributedCircuitBreaker.
func NewDistributedCircuitBreaker[T any](store SharedDataStore, settings Settings) (dcb *DistributedCircuitBreaker[T], err error) {
	if store == nil {
		return nil, ErrNoSharedStore
	}

	dcb = &DistributedCircuitBreaker[T]{
		CircuitBreaker: NewCircuitBreaker[T](settings),
		store:          store,
	}

	err = dcb.lock()
	if err != nil {
		return nil, err
	}
	defer func() {
		e := dcb.unlock()
		if err == nil {
			err = e
		}
	}()

	_, err = dcb.getSharedState()
	if err == ErrNoSharedState {
		err = dcb.setSharedState(dcb.extract())
	}
	if err != nil {
		return nil, err
	}

	return dcb, nil
}

const (
	mutexTimeout  = 5 * time.Second
	mutexWaitTime = 500 * time.Millisecond
)

func (dcb *DistributedCircuitBreaker[T]) mutexKey() string {
	return "gobreaker:mutex:" + dcb.name
}

func (dcb *DistributedCircuitBreaker[T]) lock() error {
	if dcb.store == nil {
		return ErrNoSharedStore
	}

	var err error
	expiry := time.Now().Add(mutexTimeout)
	for time.Now().Before(expiry) {
		err = dcb.store.Lock(dcb.mutexKey())
		if err == nil {
			return nil
		}

		time.Sleep(mutexWaitTime)
	}
	return err
}

func (dcb *DistributedCircuitBreaker[T]) unlock() error {
	if dcb.store == nil {
		return ErrNoSharedStore
	}

	return dcb.store.Unlock(dcb.mutexKey())
}

func (dcb *DistributedCircuitBreaker[T]) sharedStateKey() string {
	return "gobreaker:state:" + dcb.name
}

func (dcb *DistributedCircuitBreaker[T]) getSharedState() (SharedState, error) {
	var state SharedState
	if dcb.store == nil {
		return state, ErrNoSharedStore
	}

	data, err := dcb.store.GetData(dcb.sharedStateKey())
	if len(data) == 0 {
		return state, ErrNoSharedState
	} else if err != nil {
		return state, err
	}

	err = json.Unmarshal(data, &state)
	return state, err
}

func (dcb *DistributedCircuitBreaker[T]) setSharedState(state SharedState) error {
	if dcb.store == nil {
		return ErrNoSharedStore
	}

	data, err := json.Marshal(state)
	if err != nil {
		return err
	}

	return dcb.store.SetData(dcb.sharedStateKey(), data)
}

func (dcb *DistributedCircuitBreaker[T]) inject(shared SharedState) {
	dcb.mutex.Lock()
	defer dcb.mutex.Unlock()

	dcb.state = shared.State
	dcb.generation = shared.Generation
	dcb.counts = shared.Counts
	dcb.expiry = shared.Expiry
}

func (dcb *DistributedCircuitBreaker[T]) extract() SharedState {
	dcb.mutex.Lock()
	defer dcb.mutex.Unlock()

	return SharedState{
		State:      dcb.state,
		Generation: dcb.generation,
		Counts:     dcb.counts,
		Expiry:     dcb.expiry,
	}
}

// State returns the State of DistributedCircuitBreaker.
func (dcb *DistributedCircuitBreaker[T]) State() (state State, err error) {
	shared, err := dcb.getSharedState()
	if err != nil {
		return shared.State, err
	}

	err = dcb.lock()
	if err != nil {
		return state, err
	}
	defer func() {
		e := dcb.unlock()
		if err == nil {
			err = e
		}
	}()

	dcb.inject(shared)
	state = dcb.CircuitBreaker.State()
	shared = dcb.extract()

	err = dcb.setSharedState(shared)
	return state, err
}

// Execute runs the given request if the DistributedCircuitBreaker accepts it.
func (dcb *DistributedCircuitBreaker[T]) Execute(req func() (T, error)) (t T, err error) {
	shared, err := dcb.getSharedState()
	if err != nil {
		return t, err
	}

	err = dcb.lock()
	if err != nil {
		return t, err
	}
	defer func() {
		e := dcb.unlock()
		if err == nil {
			err = e
		}
	}()

	dcb.inject(shared)
	t, err = dcb.CircuitBreaker.Execute(req)
	shared = dcb.extract()

	e := dcb.setSharedState(shared)
	if e != nil {
		return t, e
	}

	return t, err
}
