package kadm

import (
	"errors"
	"fmt"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// AuthError can be returned from requests for resources that you are not
// authorized for.
type AuthError struct {
	Err error // Err is the inner *kerr.Error authorization error.
}

func (a *AuthError) Error() string     { return a.Err.Error() }
func (a *AuthError) Unwrap() error     { return a.Err }
func (a *AuthError) Is(err error) bool { return a.Err == err }

func maybeAuthErr(code int16) error {
	switch err := kerr.ErrorForCode(code); err {
	case kerr.ClusterAuthorizationFailed,
		kerr.TopicAuthorizationFailed,
		kerr.GroupAuthorizationFailed,
		kerr.TransactionalIDAuthorizationFailed,
		kerr.DelegationTokenAuthorizationFailed:
		return &AuthError{err}
	}
	return nil
}

// ShardError is a piece of a request that failed. See ShardErrors for more
// detail.
type ShardError struct {
	Req kmsg.Request // Req is a piece of the original request.
	Err error        // Err is the error that resulted in this request failing.

	// Broker, if non-nil, is the broker this request was meant to be
	// issued to. If the NodeID is -1, then this piece of the request
	// failed before being mapped to a broker.
	Broker BrokerDetail
}

// ShardErrors contains each individual error shard of a request.
//
// Under the hood, some requests to Kafka need to be mapped to brokers, split,
// and sent to many brokers. The kgo.Client handles this all internally, but
// returns the individual pieces that were requested as "shards". Internally,
// each of these pieces can also fail, and they can all fail uniquely.
//
// The kadm package takes one further step and hides the failing pieces into
// one meta error, the ShardErrors. Methods in this package that can return
// this meta error are documented; if desired, you can use errors.As to check
// and unwrap any ShardErrors return.
//
// If a request returns ShardErrors, it is possible that some aspects of the
// request were still successful. You can check ShardErrors.AllFailed as a
// shortcut for whether any of the response is usable or not.
type ShardErrors struct {
	Name      string       // Name is the name of the request these shard errors are for.
	AllFailed bool         // AllFailed indicates if the original request was entirely unsuccessful.
	Errs      []ShardError // Errs contains all individual shard errors.
}

func shardErrEach(req kmsg.Request, shards []kgo.ResponseShard, fn func(kmsg.Response) error) error {
	return shardErrEachBroker(req, shards, func(_ BrokerDetail, resp kmsg.Response) error {
		return fn(resp)
	})
}

func shardErrEachBroker(req kmsg.Request, shards []kgo.ResponseShard, fn func(BrokerDetail, kmsg.Response) error) error {
	se := ShardErrors{
		Name: kmsg.NameForKey(req.Key()),
	}
	var ae *AuthError
	for _, shard := range shards {
		if shard.Err != nil {
			se.Errs = append(se.Errs, ShardError{
				Req:    shard.Req,
				Err:    shard.Err,
				Broker: shard.Meta,
			})
			continue
		}
		if err := fn(shard.Meta, shard.Resp); errors.As(err, &ae) {
			return ae
		}
	}
	se.AllFailed = len(shards) == len(se.Errs)
	return se.into()
}

func (se *ShardErrors) into() error {
	if se == nil || len(se.Errs) == 0 {
		return nil
	}
	return se
}

// Merges two shard errors; the input errors should come from the same request.
func mergeShardErrs(e1, e2 error) error {
	var se1, se2 *ShardErrors
	if !errors.As(e1, &se1) {
		return e2
	}
	if !errors.As(e2, &se2) {
		return e1
	}
	se1.Errs = append(se1.Errs, se2.Errs...)
	se1.AllFailed = se1.AllFailed && se2.AllFailed
	return se1
}

// Error returns an error indicating the name of the request that failed, the
// number of separate errors, and the first error.
func (e *ShardErrors) Error() string {
	if len(e.Errs) == 0 {
		return "INVALID: ShardErrors contains no errors!"
	}
	return fmt.Sprintf("request %s has %d separate shard errors, first: %s", e.Name, len(e.Errs), e.Errs[0].Err)
}

// Unwrap returns the underlying errors.
func (e *ShardErrors) Unwrap() []error {
	unwrapped := make([]error, 0, len(e.Errs))

	for _, shardErr := range e.Errs {
		unwrapped = append(unwrapped, shardErr.Err)
	}

	return unwrapped
}
