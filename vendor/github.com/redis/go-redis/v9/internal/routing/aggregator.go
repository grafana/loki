package routing

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
)

var (
	ErrMaxAggregation = errors.New("redis: no valid results to aggregate for max operation")
	ErrMinAggregation = errors.New("redis: no valid results to aggregate for min operation")
	ErrAndAggregation = errors.New("redis: no valid results to aggregate for logical AND operation")
	ErrOrAggregation  = errors.New("redis: no valid results to aggregate for logical OR operation")
)

// ResponseAggregator defines the interface for aggregating responses from multiple shards.
type ResponseAggregator interface {
	// Add processes a single shard response.
	Add(result interface{}, err error) error

	// AddWithKey processes a single shard response for a specific key (used by keyed aggregators).
	AddWithKey(key string, result interface{}, err error) error

	BatchAdd(map[string]AggregatorResErr) error

	BatchSlice([]AggregatorResErr) error

	// Aggregate returns the final aggregated result and any error.
	Aggregate() (interface{}, error)
}

type AggregatorResErr struct {
	Result interface{}
	Err    error
}

// NewResponseAggregator creates an aggregator based on the response policy.
func NewResponseAggregator(policy ResponsePolicy, cmdName string) ResponseAggregator {
	switch policy {
	case RespDefaultKeyless:
		return &DefaultKeylessAggregator{results: make([]interface{}, 0)}
	case RespDefaultHashSlot:
		return &DefaultKeyedAggregator{results: make(map[string]interface{})}
	case RespAllSucceeded:
		return &AllSucceededAggregator{}
	case RespOneSucceeded:
		return &OneSucceededAggregator{}
	case RespAggSum:
		return &AggSumAggregator{}
	case RespAggMin:
		return &AggMinAggregator{}
	case RespAggMax:
		return &AggMaxAggregator{}
	case RespAggLogicalAnd:
		andAgg := &AggLogicalAndAggregator{}
		andAgg.res.Store(true)

		return andAgg
	case RespAggLogicalOr:
		return &AggLogicalOrAggregator{}
	case RespSpecial:
		return NewSpecialAggregator(cmdName)
	default:
		return &AllSucceededAggregator{}
	}
}

func NewDefaultAggregator(isKeyed bool) ResponseAggregator {
	if isKeyed {
		return &DefaultKeyedAggregator{
			results: make(map[string]interface{}),
		}
	}
	return &DefaultKeylessAggregator{}
}

// AllSucceededAggregator returns one non-error reply if every shard succeeded,
// propagates the first error otherwise.
type AllSucceededAggregator struct {
	err atomic.Pointer[error]
	// A pointer, not atomic.Value: Value panics on values of different types.
	res atomic.Pointer[interface{}]
}

func (a *AllSucceededAggregator) Add(result interface{}, err error) error {
	if err != nil {
		a.err.CompareAndSwap(nil, &err)
		return nil
	}

	if result != nil {
		a.res.CompareAndSwap(nil, &result)
	}

	return nil
}

func (a *AllSucceededAggregator) BatchAdd(results map[string]AggregatorResErr) error {
	for _, res := range results {
		err := a.Add(res.Result, res.Err)
		if err != nil {
			return err
		}

		if res.Err != nil {
			return nil
		}
	}

	return nil
}

func (a *AllSucceededAggregator) BatchSlice(results []AggregatorResErr) error {
	for _, res := range results {
		err := a.Add(res.Result, res.Err)
		if err != nil {
			return err
		}

		if res.Err != nil {
			return nil
		}
	}

	return nil
}

func (a *AllSucceededAggregator) Aggregate() (interface{}, error) {
	var res interface{}
	var err error
	r, e := a.res.Load(), a.err.Load()
	if r != nil {
		res = *r
	}
	if e != nil {
		err = *e
	}

	return res, err
}

func (a *AllSucceededAggregator) AddWithKey(key string, result interface{}, err error) error {
	return a.Add(result, err)
}

// OneSucceededAggregator returns the first non-error reply,
// if all shards errored, returns any one of those errors.
type OneSucceededAggregator struct {
	err atomic.Pointer[error]
	// A pointer, not atomic.Value: Value panics on values of different types.
	res atomic.Pointer[interface{}]
}

func (a *OneSucceededAggregator) Add(result interface{}, err error) error {
	if err != nil {
		a.err.CompareAndSwap(nil, &err)
		return nil
	}

	if result != nil {
		a.res.CompareAndSwap(nil, &result)
	}

	return nil
}

func (a *OneSucceededAggregator) BatchAdd(results map[string]AggregatorResErr) error {
	for _, res := range results {
		err := a.Add(res.Result, res.Err)
		if err != nil {
			return err
		}

		if res.Err == nil {
			return nil
		}
	}

	return nil
}

func (a *OneSucceededAggregator) AddWithKey(key string, result interface{}, err error) error {
	return a.Add(result, err)
}

func (a *OneSucceededAggregator) BatchSlice(results []AggregatorResErr) error {
	for _, res := range results {
		err := a.Add(res.Result, res.Err)
		if err != nil {
			return err
		}

		if res.Err == nil {
			return nil
		}
	}

	return nil
}

func (a *OneSucceededAggregator) Aggregate() (interface{}, error) {
	r, e := a.res.Load(), a.err.Load()
	if r != nil {
		return *r, nil
	}
	if e != nil {
		return nil, *e
	}

	return nil, nil
}

type numericAggregation uint8

const (
	numericSum numericAggregation = iota
	numericMin
	numericMax
)

type aggregateNumber struct {
	integer  int64
	floating float64
	isFloat  bool
}

func parseAggregateNumber(value interface{}) (aggregateNumber, error) {
	switch value := value.(type) {
	case int:
		return aggregateNumber{integer: int64(value)}, nil
	case int32:
		return aggregateNumber{integer: int64(value)}, nil
	case int64:
		return aggregateNumber{integer: value}, nil
	case uint64:
		// Redis integer replies and integer aggregates are signed 64-bit.
		if value > math.MaxInt64 {
			return aggregateNumber{}, fmt.Errorf("integer aggregate overflows int64: %d", value)
		}
		return aggregateNumber{integer: int64(value)}, nil
	case float32:
		return aggregateNumber{floating: float64(value), isFloat: true}, nil
	case float64:
		return aggregateNumber{floating: value, isFloat: true}, nil
	default:
		return aggregateNumber{}, fmt.Errorf("cannot convert %T to number", value)
	}
}

func (n aggregateNumber) float64() float64 {
	if n.isFloat {
		return n.floating
	}
	return float64(n.integer)
}

type numericAggregator struct {
	mu     sync.Mutex
	err    error
	value  aggregateNumber
	hasVal bool
}

func (a *numericAggregator) add(op numericAggregation, result interface{}, resultErr error) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if resultErr != nil {
		if a.err == nil {
			a.err = resultErr
		}
		return nil
	}
	if result == nil {
		return nil
	}

	value, err := parseAggregateNumber(result)
	if err != nil {
		if a.err == nil {
			a.err = err
		}
		return err
	}
	if !a.hasVal {
		a.value = value
		a.hasVal = true
		return nil
	}

	switch op {
	case numericSum:
		if a.value.isFloat || value.isFloat {
			a.value = aggregateNumber{floating: a.value.float64() + value.float64(), isFloat: true}
			return nil
		}
		if value.integer > 0 && a.value.integer > math.MaxInt64-value.integer ||
			value.integer < 0 && a.value.integer < math.MinInt64-value.integer {
			err = errors.New("integer overflow during numeric aggregation")
			a.err = err
			return err
		}
		a.value.integer += value.integer
	case numericMin, numericMax:
		if a.value.isFloat || value.isFloat {
			current, candidate := a.value.float64(), value.float64()
			if op == numericMin && candidate < current || op == numericMax && candidate > current {
				current = candidate
			}
			a.value = aggregateNumber{floating: current, isFloat: true}
		} else if op == numericMin && value.integer < a.value.integer ||
			op == numericMax && value.integer > a.value.integer {
			a.value = value
		}
	}
	return nil
}

func (a *numericAggregator) batch(op numericAggregation, results []AggregatorResErr) error {
	for _, result := range results {
		if err := a.add(op, result.Result, result.Err); err != nil {
			return err
		}
		if result.Err != nil {
			return nil
		}
	}
	return nil
}

func (a *numericAggregator) batchMap(op numericAggregation, results map[string]AggregatorResErr) error {
	for _, result := range results {
		if err := a.add(op, result.Result, result.Err); err != nil {
			return err
		}
		if result.Err != nil {
			return nil
		}
	}
	return nil
}

func (a *numericAggregator) result(emptyErr error, emptyIsZero bool) (interface{}, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil {
		return nil, a.err
	}
	if !a.hasVal {
		if emptyIsZero {
			return int64(0), nil
		}
		return nil, emptyErr
	}
	if a.value.isFloat {
		return a.value.floating, nil
	}
	return a.value.integer, nil
}

// AggSumAggregator sums numeric replies without losing integer precision.
type AggSumAggregator struct{ numericAggregator }

func (a *AggSumAggregator) Add(result interface{}, err error) error {
	return a.add(numericSum, result, err)
}

func (a *AggSumAggregator) BatchAdd(results map[string]AggregatorResErr) error {
	return a.batchMap(numericSum, results)
}

func (a *AggSumAggregator) AddWithKey(_ string, result interface{}, err error) error {
	return a.Add(result, err)
}

func (a *AggSumAggregator) BatchSlice(results []AggregatorResErr) error {
	return a.batch(numericSum, results)
}

func (a *AggSumAggregator) Aggregate() (interface{}, error) {
	return a.result(nil, true)
}

// AggMinAggregator returns the minimum numeric value from all shards.
type AggMinAggregator struct{ numericAggregator }

func (a *AggMinAggregator) Add(result interface{}, err error) error {
	return a.add(numericMin, result, err)
}

func (a *AggMinAggregator) BatchAdd(results map[string]AggregatorResErr) error {
	return a.batchMap(numericMin, results)
}

func (a *AggMinAggregator) AddWithKey(_ string, result interface{}, err error) error {
	return a.Add(result, err)
}

func (a *AggMinAggregator) BatchSlice(results []AggregatorResErr) error {
	return a.batch(numericMin, results)
}

func (a *AggMinAggregator) Aggregate() (interface{}, error) {
	return a.result(ErrMinAggregation, false)
}

// AggMaxAggregator returns the maximum numeric value from all shards.
type AggMaxAggregator struct{ numericAggregator }

func (a *AggMaxAggregator) Add(result interface{}, err error) error {
	return a.add(numericMax, result, err)
}

func (a *AggMaxAggregator) BatchAdd(results map[string]AggregatorResErr) error {
	return a.batchMap(numericMax, results)
}

func (a *AggMaxAggregator) AddWithKey(_ string, result interface{}, err error) error {
	return a.Add(result, err)
}

func (a *AggMaxAggregator) BatchSlice(results []AggregatorResErr) error {
	return a.batch(numericMax, results)
}

func (a *AggMaxAggregator) Aggregate() (interface{}, error) {
	return a.result(ErrMaxAggregation, false)
}

// AggLogicalAndAggregator performs logical AND on boolean values.
type AggLogicalAndAggregator struct {
	err       atomic.Pointer[error]
	res       atomic.Bool
	hasResult atomic.Bool
}

func (a *AggLogicalAndAggregator) Add(result interface{}, err error) error {
	if err != nil {
		a.err.CompareAndSwap(nil, &err)
		return nil
	}

	val, e := toBool(result)
	if e != nil {
		a.err.CompareAndSwap(nil, &e)
		return e
	}

	// Atomic AND operation: if val is false, result is always false
	if !val {
		a.res.Store(false)
	}

	a.hasResult.Store(true)

	return nil
}

func (a *AggLogicalAndAggregator) BatchAdd(results map[string]AggregatorResErr) error {
	result := true

	for _, res := range results {
		if res.Err != nil {
			return a.Add(nil, res.Err)
		}

		boolRes, err := toBool(res.Result)
		if err != nil {
			return a.Add(nil, err)
		}

		result = result && boolRes
	}

	return a.Add(result, nil)
}

func (a *AggLogicalAndAggregator) AddWithKey(key string, result interface{}, err error) error {
	return a.Add(result, err)
}

func (a *AggLogicalAndAggregator) BatchSlice(results []AggregatorResErr) error {
	result := true

	for _, res := range results {
		if res.Err != nil {
			return a.Add(nil, res.Err)
		}

		boolRes, err := toBool(res.Result)
		if err != nil {
			return a.Add(nil, err)
		}

		result = result && boolRes
	}

	return a.Add(result, nil)
}

func (a *AggLogicalAndAggregator) Aggregate() (interface{}, error) {
	err := a.err.Load()
	if err != nil {
		return nil, *err
	}

	if !a.hasResult.Load() {
		return nil, ErrAndAggregation
	}
	return a.res.Load(), nil
}

// AggLogicalOrAggregator performs logical OR on boolean values.
type AggLogicalOrAggregator struct {
	err       atomic.Pointer[error]
	res       atomic.Bool
	hasResult atomic.Bool
}

func (a *AggLogicalOrAggregator) Add(result interface{}, err error) error {
	if err != nil {
		a.err.CompareAndSwap(nil, &err)
		return nil
	}

	val, e := toBool(result)
	if e != nil {
		a.err.CompareAndSwap(nil, &e)
		return e
	}

	// Atomic OR operation: if val is true, result is always true
	if val {
		a.res.Store(true)
	}

	a.hasResult.Store(true)

	return nil
}

func (a *AggLogicalOrAggregator) BatchAdd(results map[string]AggregatorResErr) error {
	result := false

	for _, res := range results {
		if res.Err != nil {
			return a.Add(nil, res.Err)
		}

		boolRes, err := toBool(res.Result)
		if err != nil {
			return a.Add(nil, err)
		}

		result = result || boolRes
	}

	return a.Add(result, nil)
}

func (a *AggLogicalOrAggregator) AddWithKey(key string, result interface{}, err error) error {
	return a.Add(result, err)
}

func (a *AggLogicalOrAggregator) BatchSlice(results []AggregatorResErr) error {
	result := false

	for _, res := range results {
		if res.Err != nil {
			return a.Add(nil, res.Err)
		}

		boolRes, err := toBool(res.Result)
		if err != nil {
			return a.Add(nil, err)
		}

		result = result || boolRes
	}

	return a.Add(result, nil)
}

func (a *AggLogicalOrAggregator) Aggregate() (interface{}, error) {
	err := a.err.Load()
	if err != nil {
		return nil, *err
	}

	if !a.hasResult.Load() {
		return nil, ErrOrAggregation
	}
	return a.res.Load(), nil
}

func toBool(val interface{}) (bool, error) {
	if val == nil {
		return false, nil
	}
	switch v := val.(type) {
	case bool:
		return v, nil
	case int64:
		return v != 0, nil
	case int:
		return v != 0, nil
	default:
		return false, fmt.Errorf("cannot convert %T to bool", val)
	}
}

// DefaultKeylessAggregator collects all results in an array, order doesn't matter.
type DefaultKeylessAggregator struct {
	mu       sync.Mutex
	results  []interface{}
	firstErr error
}

func (a *DefaultKeylessAggregator) add(result interface{}, err error) error {
	if err != nil && a.firstErr == nil {
		a.firstErr = err
		return nil
	}
	if err == nil {
		a.results = append(a.results, result)
	}
	return nil
}

func (a *DefaultKeylessAggregator) Add(result interface{}, err error) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.add(result, err)
}

func (a *DefaultKeylessAggregator) BatchAdd(results map[string]AggregatorResErr) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, res := range results {
		err := a.add(res.Result, res.Err)
		if err != nil {
			return err
		}

		if res.Err != nil {
			return nil
		}
	}

	return nil
}

func (a *DefaultKeylessAggregator) AddWithKey(key string, result interface{}, err error) error {
	return a.Add(result, err)
}

func (a *DefaultKeylessAggregator) BatchSlice(results []AggregatorResErr) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, res := range results {
		err := a.add(res.Result, res.Err)
		if err != nil {
			return err
		}

		if res.Err != nil {
			return nil
		}
	}

	return nil
}

func (a *DefaultKeylessAggregator) Aggregate() (interface{}, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.firstErr != nil {
		return nil, a.firstErr
	}
	return a.results, nil
}

// DefaultKeyedAggregator reassembles replies in the exact key order of the original request.
type DefaultKeyedAggregator struct {
	mu       sync.Mutex
	results  map[string]interface{}
	keyOrder []string
	firstErr error
}

func NewDefaultKeyedAggregator(keyOrder []string) *DefaultKeyedAggregator {
	return &DefaultKeyedAggregator{
		results:  make(map[string]interface{}),
		keyOrder: keyOrder,
	}
}

func (a *DefaultKeyedAggregator) add(result interface{}, err error) error {
	if err != nil && a.firstErr == nil {
		a.firstErr = err
		return nil
	}
	// For non-keyed Add, just collect the result without ordering
	if err == nil {
		a.results["__default__"] = result
	}
	return nil
}

func (a *DefaultKeyedAggregator) Add(result interface{}, err error) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.add(result, err)
}

func (a *DefaultKeyedAggregator) BatchAdd(results map[string]AggregatorResErr) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, res := range results {
		err := a.add(res.Result, res.Err)
		if err != nil {
			return err
		}

		if res.Err != nil {
			return nil
		}
	}

	return nil
}

func (a *DefaultKeyedAggregator) addWithKey(key string, result interface{}, err error) error {
	if err != nil && a.firstErr == nil {
		a.firstErr = err
		return nil
	}
	if err == nil {
		a.results[key] = result
	}
	return nil
}

func (a *DefaultKeyedAggregator) AddWithKey(key string, result interface{}, err error) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.addWithKey(key, result, err)
}

func (a *DefaultKeyedAggregator) BatchAddWithKeyOrder(results map[string]AggregatorResErr, keyOrder []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.keyOrder = keyOrder
	for key, res := range results {
		err := a.addWithKey(key, res.Result, res.Err)
		if err != nil {
			return nil
		}

		if res.Err != nil {
			return nil
		}
	}

	return nil
}

func (a *DefaultKeyedAggregator) SetKeyOrder(keyOrder []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.keyOrder = keyOrder
}

func (a *DefaultKeyedAggregator) BatchSlice(results []AggregatorResErr) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, res := range results {
		err := a.add(res.Result, res.Err)
		if err != nil {
			return err
		}

		if res.Err != nil {
			return nil
		}
	}

	return nil
}

func (a *DefaultKeyedAggregator) Aggregate() (interface{}, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.firstErr != nil {
		return nil, a.firstErr
	}

	// If no explicit key order is set, return results in any order
	if len(a.keyOrder) == 0 {
		orderedResults := make([]interface{}, 0, len(a.results))
		for _, result := range a.results {
			orderedResults = append(orderedResults, result)
		}
		return orderedResults, nil
	}

	// Return results in the exact key order
	orderedResults := make([]interface{}, len(a.keyOrder))
	for i, key := range a.keyOrder {
		if result, exists := a.results[key]; exists {
			orderedResults[i] = result
		}
	}
	return orderedResults, nil
}

// SpecialAggregator provides a registry for command-specific aggregation logic.
type SpecialAggregator struct {
	mu             sync.Mutex
	aggregatorFunc func([]interface{}, []error) (interface{}, error)
	results        []interface{}
	errors         []error
}

func (a *SpecialAggregator) add(result interface{}, err error) error {
	a.results = append(a.results, result)
	a.errors = append(a.errors, err)
	return nil
}

func (a *SpecialAggregator) Add(result interface{}, err error) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.add(result, err)
}

func (a *SpecialAggregator) BatchAdd(results map[string]AggregatorResErr) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, res := range results {
		err := a.add(res.Result, res.Err)
		if err != nil {
			return err
		}

		if res.Err != nil {
			return nil
		}
	}

	return nil
}

func (a *SpecialAggregator) AddWithKey(key string, result interface{}, err error) error {
	return a.Add(result, err)
}

func (a *SpecialAggregator) BatchSlice(results []AggregatorResErr) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, res := range results {
		err := a.add(res.Result, res.Err)
		if err != nil {
			return err
		}

		if res.Err != nil {
			return nil
		}
	}

	return nil
}

func (a *SpecialAggregator) Aggregate() (interface{}, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.aggregatorFunc != nil {
		return a.aggregatorFunc(a.results, a.errors)
	}
	// Default behavior: return first non-error result or first error
	for i, err := range a.errors {
		if err == nil {
			return a.results[i], nil
		}
	}
	if len(a.errors) > 0 {
		return nil, a.errors[0]
	}
	return nil, nil
}

// SpecialAggregatorRegistry holds custom aggregation functions for specific commands.
var SpecialAggregatorRegistry = make(map[string]func([]interface{}, []error) (interface{}, error))

// RegisterSpecialAggregator registers a custom aggregation function for a command.
func RegisterSpecialAggregator(cmdName string, fn func([]interface{}, []error) (interface{}, error)) {
	SpecialAggregatorRegistry[cmdName] = fn
}

// NewSpecialAggregator creates a special aggregator with command-specific logic if available.
func NewSpecialAggregator(cmdName string) *SpecialAggregator {
	agg := &SpecialAggregator{}
	if fn, exists := SpecialAggregatorRegistry[cmdName]; exists {
		agg.aggregatorFunc = fn
	}
	return agg
}
