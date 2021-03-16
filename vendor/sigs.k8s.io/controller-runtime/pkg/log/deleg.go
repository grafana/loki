/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package log

import (
	"sync"

	"github.com/go-logr/logr"
)

// loggerPromise knows how to populate a concrete logr.Logger
// with options, given an actual base logger later on down the line.
type loggerPromise struct {
	logger        *DelegatingLogger
	childPromises []*loggerPromise
	promisesLock  sync.Mutex

	name  *string
	tags  []interface{}
	level int
}

func (p *loggerPromise) WithName(l *DelegatingLogger, name string) *loggerPromise {
	res := &loggerPromise{
		logger:       l,
		name:         &name,
		promisesLock: sync.Mutex{},
	}

	p.promisesLock.Lock()
	defer p.promisesLock.Unlock()
	p.childPromises = append(p.childPromises, res)
	return res
}

// WithValues provides a new Logger with the tags appended
func (p *loggerPromise) WithValues(l *DelegatingLogger, tags ...interface{}) *loggerPromise {
	res := &loggerPromise{
		logger:       l,
		tags:         tags,
		promisesLock: sync.Mutex{},
	}

	p.promisesLock.Lock()
	defer p.promisesLock.Unlock()
	p.childPromises = append(p.childPromises, res)
	return res
}

func (p *loggerPromise) V(l *DelegatingLogger, level int) *loggerPromise {
	res := &loggerPromise{
		logger:       l,
		level:        level,
		promisesLock: sync.Mutex{},
	}

	p.promisesLock.Lock()
	defer p.promisesLock.Unlock()
	p.childPromises = append(p.childPromises, res)
	return res
}

// Fulfill instantiates the Logger with the provided logger
func (p *loggerPromise) Fulfill(parentLogger logr.Logger) {
	var logger = parentLogger
	if p.name != nil {
		logger = logger.WithName(*p.name)
	}

	if p.tags != nil {
		logger = logger.WithValues(p.tags...)
	}
	if p.level != 0 {
		logger = logger.V(p.level)
	}

	p.logger.lock.Lock()
	p.logger.logger = logger
	p.logger.promise = nil
	p.logger.lock.Unlock()

	for _, childPromise := range p.childPromises {
		childPromise.Fulfill(logger)
	}
}

// DelegatingLogger is a logr.Logger that delegates to another logr.Logger.
// If the underlying promise is not nil, it registers calls to sub-loggers with
// the logging factory to be populated later, and returns a new delegating
// logger.  It expects to have *some* logr.Logger set at all times (generally
// a no-op logger before the promises are fulfilled).
type DelegatingLogger struct {
	lock    sync.RWMutex
	logger  logr.Logger
	promise *loggerPromise
}

// Enabled tests whether this Logger is enabled.  For example, commandline
// flags might be used to set the logging verbosity and disable some info
// logs.
func (l *DelegatingLogger) Enabled() bool {
	l.lock.RLock()
	defer l.lock.RUnlock()
	return l.logger.Enabled()
}

// Info logs a non-error message with the given key/value pairs as context.
//
// The msg argument should be used to add some constant description to
// the log line.  The key/value pairs can then be used to add additional
// variable information.  The key/value pairs should alternate string
// keys and arbitrary values.
func (l *DelegatingLogger) Info(msg string, keysAndValues ...interface{}) {
	l.lock.RLock()
	defer l.lock.RUnlock()
	l.logger.Info(msg, keysAndValues...)
}

// Error logs an error, with the given message and key/value pairs as context.
// It functions similarly to calling Info with the "error" named value, but may
// have unique behavior, and should be preferred for logging errors (see the
// package documentations for more information).
//
// The msg field should be used to add context to any underlying error,
// while the err field should be used to attach the actual error that
// triggered this log line, if present.
func (l *DelegatingLogger) Error(err error, msg string, keysAndValues ...interface{}) {
	l.lock.RLock()
	defer l.lock.RUnlock()
	l.logger.Error(err, msg, keysAndValues...)
}

// V returns an Logger value for a specific verbosity level, relative to
// this Logger.  In other words, V values are additive.  V higher verbosity
// level means a log message is less important.  It's illegal to pass a log
// level less than zero.
func (l *DelegatingLogger) V(level int) logr.Logger {
	l.lock.RLock()
	defer l.lock.RUnlock()

	if l.promise == nil {
		return l.logger.V(level)
	}

	res := &DelegatingLogger{logger: l.logger}
	promise := l.promise.V(res, level)
	res.promise = promise

	return res
}

// WithName provides a new Logger with the name appended
func (l *DelegatingLogger) WithName(name string) logr.Logger {
	l.lock.RLock()
	defer l.lock.RUnlock()

	if l.promise == nil {
		return l.logger.WithName(name)
	}

	res := &DelegatingLogger{logger: l.logger}
	promise := l.promise.WithName(res, name)
	res.promise = promise

	return res
}

// WithValues provides a new Logger with the tags appended
func (l *DelegatingLogger) WithValues(tags ...interface{}) logr.Logger {
	l.lock.RLock()
	defer l.lock.RUnlock()

	if l.promise == nil {
		return l.logger.WithValues(tags...)
	}

	res := &DelegatingLogger{logger: l.logger}
	promise := l.promise.WithValues(res, tags...)
	res.promise = promise

	return res
}

// Fulfill switches the logger over to use the actual logger
// provided, instead of the temporary initial one, if this method
// has not been previously called.
func (l *DelegatingLogger) Fulfill(actual logr.Logger) {
	if l.promise != nil {
		l.promise.Fulfill(actual)
	}
}

// NewDelegatingLogger constructs a new DelegatingLogger which uses
// the given logger before it's promise is fulfilled.
func NewDelegatingLogger(initial logr.Logger) *DelegatingLogger {
	l := &DelegatingLogger{
		logger:  initial,
		promise: &loggerPromise{promisesLock: sync.Mutex{}},
	}
	l.promise.logger = l
	return l
}
