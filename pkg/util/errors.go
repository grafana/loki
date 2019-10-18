package util

import (
	"bytes"
	"fmt"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CodedError interface {
	error
	Code() int
}

type codedError struct {
	code    int
	message string
}

func NewCodedError(code int, message string) CodedError {
	return codedError{
		code:    code,
		message: message,
	}
}

func NewCodedErrorf(code int, message string, args ...interface{}) CodedError {
	return codedError{
		code:    code,
		message: fmt.Sprintf(message, args...),
	}
}

func (h codedError) Error() string {
	return fmt.Sprintf("%v: %v", h.code, h.message)
}

func (h codedError) Code() int {
	return h.code
}

// IsCodedError will tell you if the provided error implements the CodedError interface.
// It will also return the status code of the CodedError, note that if the return is false the status code defaults to 500
func IsCodedError(err error) (bool, int) {
	wasCoded, ce := getCodedError(err)
	return wasCoded, ce.code
}

func getCodedError(err error) (bool, *codedError) {
	err = errors.Cause(err)
	if ce, ok := err.(CodedError); ok {
		err := ce.(codedError)
		return true, &err
	} else {
		level.Error(util.Logger).Log("msg", "non CodedError found, please convert this to a CodedError so it can be properly returned at the GRPC and HTTP client interfaces with an accurate status code", "error", err)
		return false, &codedError{message: err.Error(), code: 500}
	}
}

// The MultiError type implements the error interface, and contains the
// Errors used to construct it.
type MultiError []error

// Returns a concatenated string of the contained errors
func (es MultiError) Error() string {
	var buf bytes.Buffer

	if len(es) > 1 {
		_, _ = fmt.Fprintf(&buf, "%d errors: ", len(es))
	}

	for i, err := range es {
		if i != 0 {
			buf.WriteString("; ")
		}
		buf.WriteString(err.Error())
	}

	return buf.String()
}

// Add adds the error to the error list if it is not nil.
func (es *MultiError) Add(err error) {
	if err == nil {
		return
	}
	if merr, ok := err.(MultiError); ok {
		*es = append(*es, merr...)
	} else {
		*es = append(*es, err)
	}
}

// Err returns the error list as an error or nil if it is empty.
func (es MultiError) Err() error {
	if len(es) == 0 {
		return nil
	}
	return es
}

// IsConnCanceled returns true, if error is from a closed gRPC connection.
// copied from https://github.com/etcd-io/etcd/blob/7f47de84146bdc9225d2080ec8678ca8189a2d2b/clientv3/client.go#L646
func IsConnCanceled(err error) bool {
	if err == nil {
		return false
	}

	// >= gRPC v1.23.x
	s, ok := status.FromError(err)
	if ok {
		// connection is canceled or server has already closed the connection
		return s.Code() == codes.Canceled || s.Message() == "transport is closing"
	}

	return false
}

// CodedErrorSampler keeps a sample of entries giving priority to entries which implement CodedError and have a code >= 500
// errors which do not implement CodedError are treated the same as code == 500
type CodedErrorSampler struct {
	keepMax int
	codes   []int
	errors  []error
}

func NewCodedErrorSampler(keepMax int) *CodedErrorSampler {
	return &CodedErrorSampler{
		keepMax: keepMax,
		codes:   []int{},
		errors:  []error{},
	}
}

// IsFull will return true when the sampler is full of 4xx errors and the passed error is 4xx
// or will return true if the passed error is 5xx and the sampled error array is not full or contains 4xx errors
func (e *CodedErrorSampler) IsFull(err error) bool {
	_, ce := getCodedError(err)
	// 4xx error and sampler is full
	if ce.code < 500 && len(e.errors) == e.keepMax {
		return true
	}
	// Look for any 4xx errors which can be replaced by 5xx
	for _, cd := range e.codes {
		if cd < 500 && ce.code >= 500 {
			return false
		}
	}
	// Check if array is full, which at this point would all be 5xx's
	return len(e.errors) == e.keepMax
}

// AddEntry will add the error to the sample if there is space,
// if the error has a 5xx code it will replace any 4xx code errors
func (e *CodedErrorSampler) AddEntry(err error) {
	_, ce := getCodedError(err)

	if len(e.errors) < e.keepMax {
		e.errors = append(e.errors, err)
		e.codes = append(e.codes, ce.code)
	}

	// Look for any 4xx errors which can be replaced by 5xx
	for i, cd := range e.codes {
		if cd < 500 && ce.code >= 500 {
			e.errors[i] = ce
			e.codes[i] = cd
			return
		}
	}

	// If there wasn't room in the array and none of the errors could be replaced then the sampler is full
}

// Error returns a concatenation of all the Error() strings in the sample, each on a separate line
func (e *CodedErrorSampler) Error() string {
	if len(e.errors) == 0 {
		return ""
	}
	buf := &bytes.Buffer{}
	for _, err := range e.errors {
		fmt.Fprintln(buf, err.Error())
	}
	return buf.String()
}

// Code returns the highest numbered status code from the list of errors in the sample.
// This is a little strange as higher numbered codes do not indicated higher severity,
// however we want to favor 500's vs 400's and this was the easiest implementation.
func (e *CodedErrorSampler) Code() int {
	code := 0
	for _, cd := range e.codes {
		if cd > code {
			code = cd
		}
	}
	return code
}

func (e *CodedErrorSampler) Size() int {
	return len(e.codes)
}
