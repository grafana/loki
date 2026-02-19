// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

// This file contains code shared between client and server, including
// method handler and middleware definitions.
//
// Much of this is here so that we can factor out commonalities using
// generics. If this becomes unwieldy, it can perhaps be simplified with
// reflection.

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	internaljson "github.com/modelcontextprotocol/go-sdk/internal/json"
	"github.com/modelcontextprotocol/go-sdk/internal/jsonrpc2"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

const (
	// latestProtocolVersion is the latest protocol version that this version of
	// the SDK supports.
	//
	// It is the version that the client sends in the initialization request, and
	// the default version used by the server.
	latestProtocolVersion   = protocolVersion20250618
	protocolVersion20251125 = "2025-11-25" // not yet released
	protocolVersion20250618 = "2025-06-18"
	protocolVersion20250326 = "2025-03-26"
	protocolVersion20241105 = "2024-11-05"
)

var supportedProtocolVersions = []string{
	protocolVersion20251125,
	protocolVersion20250618,
	protocolVersion20250326,
	protocolVersion20241105,
}

// negotiatedVersion returns the effective protocol version to use, given a
// client version.
func negotiatedVersion(clientVersion string) string {
	// In general, prefer to use the clientVersion, but if we don't support the
	// client's version, use the latest version.
	//
	// This handles the case where a new spec version is released, and the SDK
	// does not support it yet.
	if !slices.Contains(supportedProtocolVersions, clientVersion) {
		return latestProtocolVersion
	}
	return clientVersion
}

// A MethodHandler handles MCP messages.
// For methods, exactly one of the return values must be nil.
// For notifications, both must be nil.
type MethodHandler func(ctx context.Context, method string, req Request) (result Result, err error)

// A Session is either a [ClientSession] or a [ServerSession].
type Session interface {
	// ID returns the session ID, or the empty string if there is none.
	ID() string

	sendingMethodInfos() map[string]methodInfo
	receivingMethodInfos() map[string]methodInfo
	sendingMethodHandler() MethodHandler
	receivingMethodHandler() MethodHandler
	getConn() *jsonrpc2.Connection
}

// Middleware is a function from [MethodHandler] to [MethodHandler].
type Middleware func(MethodHandler) MethodHandler

// addMiddleware wraps the handler in the middleware functions.
func addMiddleware(handlerp *MethodHandler, middleware []Middleware) {
	for _, m := range slices.Backward(middleware) {
		*handlerp = m(*handlerp)
	}
}

func defaultSendingMethodHandler(ctx context.Context, method string, req Request) (Result, error) {
	info, ok := req.GetSession().sendingMethodInfos()[method]
	if !ok {
		// This can be called from user code, with an arbitrary value for method.
		return nil, jsonrpc2.ErrNotHandled
	}
	params := req.GetParams()
	if initParams, ok := params.(*InitializeParams); ok {
		// Fix the marshaling of initialize params, to work around #607.
		//
		// The initialize params we produce should never be nil, nor have nil
		// capabilities, so any panic here is a bug.
		params = initParams.toV2()
	}
	// Notifications don't have results.
	if strings.HasPrefix(method, "notifications/") {
		return nil, req.GetSession().getConn().Notify(ctx, method, params)
	}
	// Create the result to unmarshal into.
	// The concrete type of the result is the return type of the receiving function.
	res := info.newResult()
	if err := call(ctx, req.GetSession().getConn(), method, params, res); err != nil {
		return nil, err
	}
	return res, nil
}

// Helper method to avoid typed nil.
func orZero[T any, P *U, U any](p P) T {
	if p == nil {
		var zero T
		return zero
	}
	return any(p).(T)
}

func handleNotify(ctx context.Context, method string, req Request) error {
	mh := req.GetSession().sendingMethodHandler()
	_, err := mh(ctx, method, req)
	return err
}

func handleSend[R Result](ctx context.Context, method string, req Request) (R, error) {
	mh := req.GetSession().sendingMethodHandler()
	// mh might be user code, so ensure that it returns the right values for the jsonrpc2 protocol.
	res, err := mh(ctx, method, req)
	if err != nil {
		var z R
		return z, err
	}
	return res.(R), nil
}

// defaultReceivingMethodHandler is the initial MethodHandler for servers and clients, before being wrapped by middleware.
func defaultReceivingMethodHandler[S Session](ctx context.Context, method string, req Request) (Result, error) {
	info, ok := req.GetSession().receivingMethodInfos()[method]
	if !ok {
		// This can be called from user code, with an arbitrary value for method.
		return nil, jsonrpc2.ErrNotHandled
	}
	return info.handleMethod(ctx, method, req)
}

func handleReceive[S Session](ctx context.Context, session S, jreq *jsonrpc.Request) (Result, error) {
	info, err := checkRequest(jreq, session.receivingMethodInfos())
	if err != nil {
		return nil, err
	}
	params, err := info.unmarshalParams(jreq.Params)
	if err != nil {
		return nil, fmt.Errorf("handling '%s': %w", jreq.Method, err)
	}

	mh := session.receivingMethodHandler()
	re, _ := jreq.Extra.(*RequestExtra)
	req := info.newRequest(session, params, re)
	// mh might be user code, so ensure that it returns the right values for the jsonrpc2 protocol.
	res, err := mh(ctx, jreq.Method, req)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// checkRequest checks the given request against the provided method info, to
// ensure it is a valid MCP request.
//
// If valid, the relevant method info is returned. Otherwise, a non-nil error
// is returned describing why the request is invalid.
//
// This is extracted from request handling so that it can be called in the
// transport layer to preemptively reject bad requests.
func checkRequest(req *jsonrpc.Request, infos map[string]methodInfo) (methodInfo, error) {
	info, ok := infos[req.Method]
	if !ok {
		return methodInfo{}, fmt.Errorf("%w: %q unsupported", jsonrpc2.ErrNotHandled, req.Method)
	}
	if info.flags&notification != 0 && req.IsCall() {
		return methodInfo{}, fmt.Errorf("%w: unexpected id for %q", jsonrpc2.ErrInvalidRequest, req.Method)
	}
	if info.flags&notification == 0 && !req.IsCall() {
		return methodInfo{}, fmt.Errorf("%w: missing id for %q", jsonrpc2.ErrInvalidRequest, req.Method)
	}
	// missingParamsOK is checked here to catch the common case where "params" is
	// missing entirely.
	//
	// However, it's checked again after unmarshalling to catch the rare but
	// possible case where "params" is JSON null (see https://go.dev/issue/33835).
	if info.flags&missingParamsOK == 0 && len(req.Params) == 0 {
		return methodInfo{}, fmt.Errorf("%w: missing required \"params\"", jsonrpc2.ErrInvalidRequest)
	}
	return info, nil
}

// methodInfo is information about sending and receiving a method.
type methodInfo struct {
	// flags is a collection of flags controlling how the JSONRPC method is
	// handled. See individual flag values for documentation.
	flags methodFlags
	// Unmarshal params from the wire into a Params struct.
	// Used on the receive side.
	unmarshalParams func(json.RawMessage) (Params, error)
	newRequest      func(Session, Params, *RequestExtra) Request
	// Run the code when a call to the method is received.
	// Used on the receive side.
	handleMethod MethodHandler
	// Create a pointer to a Result struct.
	// Used on the send side.
	newResult func() Result
}

// The following definitions support converting from typed to untyped method handlers.
// Type parameter meanings:
// - S: sessions
// - P: params
// - R: results

// A typedMethodHandler is like a MethodHandler, but with type information.
type (
	typedClientMethodHandler[P Params, R Result] func(context.Context, *ClientRequest[P]) (R, error)
	typedServerMethodHandler[P Params, R Result] func(context.Context, *ServerRequest[P]) (R, error)
)

type paramsPtr[T any] interface {
	*T
	Params
}

type methodFlags int

const (
	notification    methodFlags = 1 << iota // method is a notification, not request
	missingParamsOK                         // params may be missing or null
)

func newClientMethodInfo[P paramsPtr[T], R Result, T any](d typedClientMethodHandler[P, R], flags methodFlags) methodInfo {
	mi := newMethodInfo[P, R](flags)
	mi.newRequest = func(s Session, p Params, _ *RequestExtra) Request {
		r := &ClientRequest[P]{Session: s.(*ClientSession)}
		if p != nil {
			r.Params = p.(P)
		}
		return r
	}
	mi.handleMethod = MethodHandler(func(ctx context.Context, _ string, req Request) (Result, error) {
		return d(ctx, req.(*ClientRequest[P]))
	})
	return mi
}

func newServerMethodInfo[P paramsPtr[T], R Result, T any](d typedServerMethodHandler[P, R], flags methodFlags) methodInfo {
	mi := newMethodInfo[P, R](flags)
	mi.newRequest = func(s Session, p Params, re *RequestExtra) Request {
		r := &ServerRequest[P]{Session: s.(*ServerSession), Extra: re}
		if p != nil {
			r.Params = p.(P)
		}
		return r
	}
	mi.handleMethod = MethodHandler(func(ctx context.Context, _ string, req Request) (Result, error) {
		return d(ctx, req.(*ServerRequest[P]))
	})
	return mi
}

// newMethodInfo creates a methodInfo from a typedMethodHandler.
//
// If isRequest is set, the method is treated as a request rather than a
// notification.
func newMethodInfo[P paramsPtr[T], R Result, T any](flags methodFlags) methodInfo {
	return methodInfo{
		flags: flags,
		unmarshalParams: func(m json.RawMessage) (Params, error) {
			var p P
			if m != nil {
				if err := internaljson.Unmarshal(m, &p); err != nil {
					return nil, fmt.Errorf("unmarshaling %q into a %T: %w", m, p, err)
				}
			}
			// We must check missingParamsOK here, in addition to checkRequest, to
			// catch the edge cases where "params" is set to JSON null.
			// See also https://go.dev/issue/33835.
			//
			// We need to ensure that p is non-null to guard against crashes, as our
			// internal code or externally provided handlers may assume that params
			// is non-null.
			if flags&missingParamsOK == 0 && p == nil {
				return nil, fmt.Errorf("%w: missing required \"params\"", jsonrpc2.ErrInvalidRequest)
			}
			return orZero[Params](p), nil
		},
		// newResult is used on the send side, to construct the value to unmarshal the result into.
		// R is a pointer to a result struct. There is no way to "unpointer" it without reflection.
		// TODO(jba): explore generic approaches to this, perhaps by treating R in
		// the signature as the unpointered type.
		newResult: func() Result { return reflect.New(reflect.TypeFor[R]().Elem()).Interface().(R) },
	}
}

// serverMethod is glue for creating a typedMethodHandler from a method on Server.
func serverMethod[P Params, R Result](
	f func(*Server, context.Context, *ServerRequest[P]) (R, error),
) typedServerMethodHandler[P, R] {
	return func(ctx context.Context, req *ServerRequest[P]) (R, error) {
		return f(req.Session.server, ctx, req)
	}
}

// clientMethod is glue for creating a typedMethodHandler from a method on Client.
func clientMethod[P Params, R Result](
	f func(*Client, context.Context, *ClientRequest[P]) (R, error),
) typedClientMethodHandler[P, R] {
	return func(ctx context.Context, req *ClientRequest[P]) (R, error) {
		return f(req.Session.client, ctx, req)
	}
}

// serverSessionMethod is glue for creating a typedServerMethodHandler from a method on ServerSession.
func serverSessionMethod[P Params, R Result](f func(*ServerSession, context.Context, P) (R, error)) typedServerMethodHandler[P, R] {
	return func(ctx context.Context, req *ServerRequest[P]) (R, error) {
		return f(req.GetSession().(*ServerSession), ctx, req.Params)
	}
}

// clientSessionMethod is glue for creating a typedMethodHandler from a method on ServerSession.
func clientSessionMethod[P Params, R Result](f func(*ClientSession, context.Context, P) (R, error)) typedClientMethodHandler[P, R] {
	return func(ctx context.Context, req *ClientRequest[P]) (R, error) {
		return f(req.GetSession().(*ClientSession), ctx, req.Params)
	}
}

// MCP-specific error codes.
const (
	// CodeResourceNotFound indicates that a requested resource could not be found.
	CodeResourceNotFound = -32002
	// CodeURLElicitationRequired indicates that the server requires URL elicitation
	// before processing the request. The client should execute the elicitation handler
	// with the elicitations provided in the error data.
	CodeURLElicitationRequired = -32042
)

// URLElicitationRequiredError returns an error indicating that URL elicitation is required
// before the request can be processed. The elicitations parameter should contain the
// elicitation requests that must be completed.
func URLElicitationRequiredError(elicitations []*ElicitParams) error {
	// Validate that all elicitations are URL mode
	for _, elicit := range elicitations {
		mode := elicit.Mode
		if mode == "" {
			mode = "form" // default mode
		}
		if mode != "url" {
			panic(fmt.Sprintf("URLElicitationRequiredError requires all elicitations to be URL mode, got %q", mode))
		}
	}

	data, err := json.Marshal(map[string]any{
		"elicitations": elicitations,
	})
	if err != nil {
		// This should never happen with valid ElicitParams
		panic(fmt.Sprintf("failed to marshal elicitations: %v", err))
	}
	return &jsonrpc.Error{
		Code:    CodeURLElicitationRequired,
		Message: "URL elicitation required",
		Data:    json.RawMessage(data),
	}
}

// Internal error codes
const (
	// The error code if the method exists and was called properly, but the peer does not support it.
	//
	// TODO(rfindley): this code is wrong, and we should fix it to be
	// consistent with other SDKs.
	codeUnsupportedMethod = -31001
)

// notifySessions calls Notify on all the sessions.
// Should be called on a copy of the peer sessions.
// The logger must be non-nil.
func notifySessions[S Session, P Params](sessions []S, method string, params P, logger *slog.Logger) {
	if sessions == nil {
		return
	}
	// Notify with the background context, so the messages are sent on the
	// standalone stream.
	// TODO: make this timeout configurable, or call handleNotify asynchronously.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// TODO: there's a potential spec violation here, when the feature list
	// changes before the session (client or server) is initialized.
	for _, s := range sessions {
		req := newRequest(s, params)
		if err := handleNotify(ctx, method, req); err != nil {
			logger.Warn(fmt.Sprintf("calling %s: %v", method, err))
		}
	}
}

func newRequest[S Session, P Params](s S, p P) Request {
	switch s := any(s).(type) {
	case *ClientSession:
		return &ClientRequest[P]{Session: s, Params: p}
	case *ServerSession:
		return &ServerRequest[P]{Session: s, Params: p}
	default:
		panic("bad session")
	}
}

// Meta is additional metadata for requests, responses and other types.
type Meta map[string]any

// GetMeta returns metadata from a value.
func (m Meta) GetMeta() map[string]any { return m }

// SetMeta sets the metadata on a value.
func (m *Meta) SetMeta(x map[string]any) { *m = x }

const progressTokenKey = "progressToken"

func getProgressToken(p Params) any {
	return p.GetMeta()[progressTokenKey]
}

func setProgressToken(p Params, pt any) {
	switch pt.(type) {
	// Support int32 and int64 for atomic.IntNN.
	case int, int32, int64, string:
	default:
		panic(fmt.Sprintf("progress token %v is of type %[1]T, not int or string", pt))
	}
	m := p.GetMeta()
	if m == nil {
		m = map[string]any{}
	}
	m[progressTokenKey] = pt
}

// A Request is a method request with parameters and additional information, such as the session.
// Request is implemented by [*ClientRequest] and [*ServerRequest].
type Request interface {
	isRequest()
	GetSession() Session
	GetParams() Params
	// GetExtra returns the Extra field for ServerRequests, and nil for ClientRequests.
	GetExtra() *RequestExtra
}

// A ClientRequest is a request to a client.
type ClientRequest[P Params] struct {
	Session *ClientSession
	Params  P
}

// A ServerRequest is a request to a server.
type ServerRequest[P Params] struct {
	Session *ServerSession
	Params  P
	Extra   *RequestExtra
}

// RequestExtra is extra information included in requests, typically from
// the transport layer.
type RequestExtra struct {
	TokenInfo *auth.TokenInfo // bearer token info (e.g. from OAuth) if any
	Header    http.Header     // header from HTTP request, if any

	// If set, CloseSSEStream explicitly closes the current SSE request stream.
	//
	// [SEP-1699] introduced server-side SSE stream disconnection: for
	// long-running requests, servers may opt to close the SSE stream and
	// ask the client to retry at a later time. CloseSSEStream implements this
	// feature; if RetryAfter is set, an event is sent with a `retry:` field
	// to configure the reconnection delay.
	//
	// [SEP-1699]: https://github.com/modelcontextprotocol/modelcontextprotocol/issues/1699
	CloseSSEStream func(CloseSSEStreamArgs)
}

// CloseSSEStreamArgs are arguments for [RequestExtra.CloseSSEStream].
type CloseSSEStreamArgs struct {
	// RetryAfter configures the reconnection delay sent to the client via the
	// SSE retry field. If zero, no retry field is sent.
	RetryAfter time.Duration
}

func (*ClientRequest[P]) isRequest() {}
func (*ServerRequest[P]) isRequest() {}

func (r *ClientRequest[P]) GetSession() Session { return r.Session }
func (r *ServerRequest[P]) GetSession() Session { return r.Session }

func (r *ClientRequest[P]) GetParams() Params { return r.Params }
func (r *ServerRequest[P]) GetParams() Params { return r.Params }

func (r *ClientRequest[P]) GetExtra() *RequestExtra { return nil }
func (r *ServerRequest[P]) GetExtra() *RequestExtra { return r.Extra }

func serverRequestFor[P Params](s *ServerSession, p P) *ServerRequest[P] {
	return &ServerRequest[P]{Session: s, Params: p}
}

func clientRequestFor[P Params](s *ClientSession, p P) *ClientRequest[P] {
	return &ClientRequest[P]{Session: s, Params: p}
}

// Params is a parameter (input) type for an MCP call or notification.
type Params interface {
	// GetMeta returns metadata from a value.
	GetMeta() map[string]any
	// SetMeta sets the metadata on a value.
	SetMeta(map[string]any)

	// isParams discourages implementation of Params outside of this package.
	isParams()
}

// RequestParams is a parameter (input) type for an MCP request.
type RequestParams interface {
	Params

	// GetProgressToken returns the progress token from the params' Meta field, or nil
	// if there is none.
	GetProgressToken() any

	// SetProgressToken sets the given progress token into the params' Meta field.
	// It panics if its argument is not an int or a string.
	SetProgressToken(any)
}

// Result is a result of an MCP call.
type Result interface {
	// isResult discourages implementation of Result outside of this package.
	isResult()

	// GetMeta returns metadata from a value.
	GetMeta() map[string]any
	// SetMeta sets the metadata on a value.
	SetMeta(map[string]any)
}

// emptyResult is returned by methods that have no result, like ping.
// Those methods cannot return nil, because jsonrpc2 cannot handle nils.
type emptyResult struct{}

func (*emptyResult) isResult()               {}
func (*emptyResult) GetMeta() map[string]any { panic("should never be called") }
func (*emptyResult) SetMeta(map[string]any)  { panic("should never be called") }

type listParams interface {
	// Returns a pointer to the param's Cursor field.
	cursorPtr() *string
}

type listResult[T any] interface {
	// Returns a pointer to the param's NextCursor field.
	nextCursorPtr() *string
}

// keepaliveSession represents a session that supports keepalive functionality.
type keepaliveSession interface {
	Ping(ctx context.Context, params *PingParams) error
	Close() error
}

// startKeepalive starts the keepalive mechanism for a session.
// It assigns the cancel function to the provided cancelPtr and starts a goroutine
// that sends ping messages at the specified interval.
func startKeepalive(session keepaliveSession, interval time.Duration, cancelPtr *context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	// Assign cancel function before starting goroutine to avoid race condition.
	// We cannot return it because the caller may need to cancel during the
	// window between goroutine scheduling and function return.
	*cancelPtr = cancel

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pingCtx, pingCancel := context.WithTimeout(context.Background(), interval/2)
				err := session.Ping(pingCtx, nil)
				pingCancel()
				if err != nil {
					// Ping failed, close the session
					_ = session.Close()
					return
				}
			}
		}
	}()
}
