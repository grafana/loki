// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/internal/json"
	"github.com/modelcontextprotocol/go-sdk/internal/jsonrpc2"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// A Client is an MCP client, which may be connected to an MCP server
// using the [Client.Connect] method.
type Client struct {
	impl                    *Implementation
	opts                    ClientOptions
	mu                      sync.Mutex
	roots                   *featureSet[*Root]
	sessions                []*ClientSession
	sendingMethodHandler_   MethodHandler
	receivingMethodHandler_ MethodHandler
}

// NewClient creates a new [Client].
//
// Use [Client.Connect] to connect it to an MCP server.
//
// The first argument must not be nil.
//
// If non-nil, the provided options configure the Client.
func NewClient(impl *Implementation, options *ClientOptions) *Client {
	if impl == nil {
		panic("nil Implementation")
	}
	var opts ClientOptions
	if options != nil {
		opts = *options
	}
	options = nil // prevent reuse

	if opts.Logger == nil { // ensure we have a logger
		opts.Logger = ensureLogger(nil)
	}

	return &Client{
		impl:                    impl,
		opts:                    opts,
		roots:                   newFeatureSet(func(r *Root) string { return r.URI }),
		sendingMethodHandler_:   defaultSendingMethodHandler,
		receivingMethodHandler_: defaultReceivingMethodHandler[*ClientSession],
	}
}

// ClientOptions configures the behavior of the client.
type ClientOptions struct {
	// Logger may be set to a non-nil value to enable logging of client activity.
	Logger *slog.Logger
	// CreateMessageHandler handles incoming requests for sampling/createMessage.
	//
	// Setting CreateMessageHandler to a non-nil value automatically causes the
	// client to advertise the sampling capability, with default value
	// &SamplingCapabilities{}. If [ClientOptions.Capabilities] is set and has a
	// non nil value for [ClientCapabilities.Sampling], that value overrides the
	// inferred capability.
	CreateMessageHandler func(context.Context, *CreateMessageRequest) (*CreateMessageResult, error)
	// ElicitationHandler handles incoming requests for elicitation/create.
	//
	// Setting ElicitationHandler to a non-nil value automatically causes the
	// client to advertise the elicitation capability, with default value
	// &ElicitationCapabilities{}. If [ClientOptions.Capabilities] is set and has
	// a non nil value for [ClientCapabilities.ELicitattion], that value
	// overrides the inferred capability.
	ElicitationHandler func(context.Context, *ElicitRequest) (*ElicitResult, error)
	// Capabilities optionally configures the client's default capabilities,
	// before any capabilities are inferred from other configuration.
	//
	// If Capabilities is nil, the default client capabilities are
	// {"roots":{"listChanged":true}}, for historical reasons. Setting
	// Capabilities to a non-nil value overrides this default. As a special case,
	// to work around #607, Capabilities.Roots is ignored: set
	// Capabilities.RootsV2 to configure the roots capability. This allows the
	// "roots" capability to be disabled entirely.
	//
	// For example:
	//   - To disable the "roots" capability, use &ClientCapabilities{}
	//   - To configure "roots", but disable "listChanged" notifications, use
	//     &ClientCapabilities{RootsV2:&RootCapabilities{}}.
	//
	// # Interaction with capability inference
	//
	// Sampling and elicitation capabilities are automatically added when their
	// corresponding handlers are set, with the default value described at
	// [ClientOptions.CreateMessageHandler] and
	// [ClientOptions.ElicitationHandler]. If the Sampling or Elicitation fields
	// are set in the Capabilities field, their values override the inferred
	// value.
	//
	// For example, to to configure elicitation modes:
	//
	//	Capabilities: &ClientCapabilities{
	//	    Elicitation: &ElicitationCapabilities{
	//	        Form: &FormElicitationCapabilities{},
	//	        URL:  &URLElicitationCapabilities{},
	//	    },
	//	}
	//
	// Conversely, if Capabilities does not set a field (for example, if the
	// Elicitation field is nil), the inferred elicitation capability will be
	// used.
	Capabilities *ClientCapabilities
	// ElicitationCompleteHandler handles incoming notifications for notifications/elicitation/complete.
	ElicitationCompleteHandler func(context.Context, *ElicitationCompleteNotificationRequest)
	// Handlers for notifications from the server.
	ToolListChangedHandler      func(context.Context, *ToolListChangedRequest)
	PromptListChangedHandler    func(context.Context, *PromptListChangedRequest)
	ResourceListChangedHandler  func(context.Context, *ResourceListChangedRequest)
	ResourceUpdatedHandler      func(context.Context, *ResourceUpdatedNotificationRequest)
	LoggingMessageHandler       func(context.Context, *LoggingMessageRequest)
	ProgressNotificationHandler func(context.Context, *ProgressNotificationClientRequest)
	// If non-zero, defines an interval for regular "ping" requests.
	// If the peer fails to respond to pings originating from the keepalive check,
	// the session is automatically closed.
	KeepAlive time.Duration
}

// bind implements the binder[*ClientSession] interface, so that Clients can
// be connected using [connect].
func (c *Client) bind(mcpConn Connection, conn *jsonrpc2.Connection, state *clientSessionState, onClose func()) *ClientSession {
	assert(mcpConn != nil && conn != nil, "nil connection")
	cs := &ClientSession{conn: conn, mcpConn: mcpConn, client: c, onClose: onClose}
	if state != nil {
		cs.state = *state
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessions = append(c.sessions, cs)
	return cs
}

// disconnect implements the binder[*Client] interface, so that
// Clients can be connected using [connect].
func (c *Client) disconnect(cs *ClientSession) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessions = slices.DeleteFunc(c.sessions, func(cs2 *ClientSession) bool {
		return cs2 == cs
	})
}

// TODO: Consider exporting this type and its field.
type unsupportedProtocolVersionError struct {
	version string
}

func (e unsupportedProtocolVersionError) Error() string {
	return fmt.Sprintf("unsupported protocol version: %q", e.version)
}

// ClientSessionOptions is reserved for future use.
type ClientSessionOptions struct {
	// protocolVersion overrides the protocol version sent in the initialize
	// request, for testing. If empty, latestProtocolVersion is used.
	protocolVersion string
}

func (c *Client) capabilities(protocolVersion string) *ClientCapabilities {
	// Start with user-provided capabilities as defaults, or use SDK defaults.
	var caps *ClientCapabilities
	if c.opts.Capabilities != nil {
		// Deep copy the user-provided capabilities to avoid mutation.
		caps = c.opts.Capabilities.clone()
	} else {
		// SDK defaults: roots with listChanged.
		// (this was the default behavior at v1.0.0, and so cannot be changed)
		caps = &ClientCapabilities{
			RootsV2: &RootCapabilities{
				ListChanged: true,
			},
		}
	}

	// Sync Roots from RootsV2 for backward compatibility (#607).
	if caps.RootsV2 != nil {
		caps.Roots = *caps.RootsV2
	}

	// Augment with sampling capability if handler is set.
	if c.opts.CreateMessageHandler != nil {
		if caps.Sampling == nil {
			caps.Sampling = &SamplingCapabilities{}
		}
	}

	// Augment with elicitation capability if handler is set.
	if c.opts.ElicitationHandler != nil {
		if caps.Elicitation == nil {
			caps.Elicitation = &ElicitationCapabilities{}
			// Form elicitation was added in 2025-11-25; for older versions,
			// {} is treated the same as {"form":{}}.
			if protocolVersion >= protocolVersion20251125 {
				caps.Elicitation.Form = &FormElicitationCapabilities{}
			}
		}
	}
	return caps
}

// Connect begins an MCP session by connecting to a server over the given
// transport. The resulting session is initialized, and ready to use.
//
// Typically, it is the responsibility of the client to close the connection
// when it is no longer needed. However, if the connection is closed by the
// server, calls or notifications will return an error wrapping
// [ErrConnectionClosed].
func (c *Client) Connect(ctx context.Context, t Transport, opts *ClientSessionOptions) (cs *ClientSession, err error) {
	cs, err = connect(ctx, t, c, (*clientSessionState)(nil), nil)
	if err != nil {
		return nil, err
	}

	protocolVersion := latestProtocolVersion
	if opts != nil && opts.protocolVersion != "" {
		protocolVersion = opts.protocolVersion
	}
	params := &InitializeParams{
		ProtocolVersion: protocolVersion,
		ClientInfo:      c.impl,
		Capabilities:    c.capabilities(protocolVersion),
	}
	req := &InitializeRequest{Session: cs, Params: params}
	res, err := handleSend[*InitializeResult](ctx, methodInitialize, req)
	if err != nil {
		_ = cs.Close()
		return nil, err
	}
	if !slices.Contains(supportedProtocolVersions, res.ProtocolVersion) {
		return nil, unsupportedProtocolVersionError{res.ProtocolVersion}
	}
	cs.state.InitializeResult = res
	if hc, ok := cs.mcpConn.(clientConnection); ok {
		hc.sessionUpdated(cs.state)
	}
	req2 := &initializedClientRequest{Session: cs, Params: &InitializedParams{}}
	if err := handleNotify(ctx, notificationInitialized, req2); err != nil {
		_ = cs.Close()
		return nil, err
	}

	if c.opts.KeepAlive > 0 {
		cs.startKeepalive(c.opts.KeepAlive)
	}

	return cs, nil
}

// A ClientSession is a logical connection with an MCP server. Its
// methods can be used to send requests or notifications to the server. Create
// a session by calling [Client.Connect].
//
// Call [ClientSession.Close] to close the connection, or await server
// termination with [ClientSession.Wait].
type ClientSession struct {
	// Ensure that onClose is called at most once.
	// We defensively use an atomic CompareAndSwap rather than a sync.Once, in case the
	// onClose callback triggers a re-entrant call to Close.
	calledOnClose atomic.Bool
	onClose       func()

	conn            *jsonrpc2.Connection
	client          *Client
	keepaliveCancel context.CancelFunc
	mcpConn         Connection

	// No mutex is (currently) required to guard the session state, because it is
	// only set synchronously during Client.Connect.
	state clientSessionState

	// Pending URL elicitations waiting for completion notifications.
	pendingElicitationsMu sync.Mutex
	pendingElicitations   map[string]chan struct{}
}

type clientSessionState struct {
	InitializeResult *InitializeResult
}

func (cs *ClientSession) InitializeResult() *InitializeResult { return cs.state.InitializeResult }

func (cs *ClientSession) ID() string {
	if c, ok := cs.mcpConn.(hasSessionID); ok {
		return c.SessionID()
	}
	return ""
}

// Close performs a graceful close of the connection, preventing new requests
// from being handled, and waiting for ongoing requests to return. Close then
// terminates the connection.
//
// Close is idempotent and concurrency safe.
func (cs *ClientSession) Close() error {
	// Note: keepaliveCancel access is safe without a mutex because:
	// 1. keepaliveCancel is only written once during startKeepalive (happens-before all Close calls)
	// 2. context.CancelFunc is safe to call multiple times and from multiple goroutines
	// 3. The keepalive goroutine calls Close on ping failure, but this is safe since
	//    Close is idempotent and conn.Close() handles concurrent calls correctly
	if cs.keepaliveCancel != nil {
		cs.keepaliveCancel()
	}
	err := cs.conn.Close()

	if cs.onClose != nil && cs.calledOnClose.CompareAndSwap(false, true) {
		cs.onClose()
	}

	return err
}

// Wait waits for the connection to be closed by the server.
// Generally, clients should be responsible for closing the connection.
func (cs *ClientSession) Wait() error {
	return cs.conn.Wait()
}

// registerElicitationWaiter registers a waiter for an elicitation complete
// notification with the given elicitation ID. It returns two functions: an await
// function that waits for the notification or context cancellation, and a cleanup
// function that must be called to unregister the waiter. This must be called before
// triggering the elicitation to avoid a race condition where the notification
// arrives before the waiter is registered.
//
// The cleanup function must be called even if the await function is never called,
// to prevent leaking the registration.
func (cs *ClientSession) registerElicitationWaiter(elicitationID string) (await func(context.Context) error, cleanup func()) {
	// Create a channel for this elicitation.
	ch := make(chan struct{}, 1)

	// Register the channel.
	cs.pendingElicitationsMu.Lock()
	if cs.pendingElicitations == nil {
		cs.pendingElicitations = make(map[string]chan struct{})
	}
	cs.pendingElicitations[elicitationID] = ch
	cs.pendingElicitationsMu.Unlock()

	// Return await and cleanup functions.
	await = func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for elicitation completion: %w", ctx.Err())
		case <-ch:
			return nil
		}
	}

	cleanup = func() {
		cs.pendingElicitationsMu.Lock()
		delete(cs.pendingElicitations, elicitationID)
		cs.pendingElicitationsMu.Unlock()
	}

	return await, cleanup
}

// startKeepalive starts the keepalive mechanism for this client session.
func (cs *ClientSession) startKeepalive(interval time.Duration) {
	startKeepalive(cs, interval, &cs.keepaliveCancel)
}

// AddRoots adds the given roots to the client,
// replacing any with the same URIs,
// and notifies any connected servers.
func (c *Client) AddRoots(roots ...*Root) {
	// Only notify if something could change.
	if len(roots) == 0 {
		return
	}
	changeAndNotify(c, notificationRootsListChanged, &RootsListChangedParams{},
		func() bool { c.roots.add(roots...); return true })
}

// RemoveRoots removes the roots with the given URIs,
// and notifies any connected servers if the list has changed.
// It is not an error to remove a nonexistent root.
func (c *Client) RemoveRoots(uris ...string) {
	changeAndNotify(c, notificationRootsListChanged, &RootsListChangedParams{},
		func() bool { return c.roots.remove(uris...) })
}

// changeAndNotify is called when a feature is added or removed.
// It calls change, which should do the work and report whether a change actually occurred.
// If there was a change, it notifies a snapshot of the sessions.
func changeAndNotify[P Params](c *Client, notification string, params P, change func() bool) {
	var sessions []*ClientSession
	// Lock for the change, but not for the notification.
	c.mu.Lock()
	if change() {
		// Check if listChanged is enabled for this notification type.
		if c.shouldSendListChangedNotification(notification) {
			sessions = slices.Clone(c.sessions)
		}
	}
	c.mu.Unlock()
	notifySessions(sessions, notification, params, c.opts.Logger)
}

// shouldSendListChangedNotification checks if the client's capabilities allow
// sending the given list-changed notification.
func (c *Client) shouldSendListChangedNotification(notification string) bool {
	// Get effective capabilities (considering user-provided defaults).
	caps := c.opts.Capabilities

	switch notification {
	case notificationRootsListChanged:
		// If user didn't specify capabilities, default behavior sends notifications.
		if caps == nil {
			return true
		}
		// Check RootsV2 first (preferred), then fall back to Roots.
		if caps.RootsV2 != nil {
			return caps.RootsV2.ListChanged
		}
		return caps.Roots.ListChanged
	default:
		// Unknown notification, allow by default.
		return true
	}
}

func (c *Client) listRoots(_ context.Context, req *ListRootsRequest) (*ListRootsResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	roots := slices.Collect(c.roots.all())
	if roots == nil {
		roots = []*Root{} // avoid JSON null
	}
	return &ListRootsResult{
		Roots: roots,
	}, nil
}

func (c *Client) createMessage(ctx context.Context, req *CreateMessageRequest) (*CreateMessageResult, error) {
	if c.opts.CreateMessageHandler == nil {
		// TODO: wrap or annotate this error? Pick a standard code?
		return nil, &jsonrpc.Error{Code: codeUnsupportedMethod, Message: "client does not support CreateMessage"}
	}
	return c.opts.CreateMessageHandler(ctx, req)
}

// urlElicitationMiddleware returns middleware that automatically handles URL elicitation
// required errors by executing the elicitation handler, waiting for completion notifications,
// and retrying the operation.
//
// This middleware should be added to clients that want automatic URL elicitation handling:
//
//	client := mcp.NewClient(impl, opts)
//	client.AddSendingMiddleware(mcp.urlElicitationMiddleware())
//
// TODO(rfindley): this isn't strictly necessary for the SEP, but may be
// useful. Propose exporting it.
func urlElicitationMiddleware() Middleware {
	return func(next MethodHandler) MethodHandler {
		return func(ctx context.Context, method string, req Request) (Result, error) {
			// Call the underlying handler.
			res, err := next(ctx, method, req)
			if err == nil {
				return res, nil
			}

			// Check if this is a URL elicitation required error.
			var rpcErr *jsonrpc.Error
			if !errors.As(err, &rpcErr) || rpcErr.Code != CodeURLElicitationRequired {
				return res, err
			}

			// Notifications don't support retries.
			if strings.HasPrefix(method, "notifications/") {
				return res, err
			}

			// Extract the client session.
			cs, ok := req.GetSession().(*ClientSession)
			if !ok {
				return res, err
			}

			// Check if the client has an elicitation handler.
			if cs.client.opts.ElicitationHandler == nil {
				return res, err
			}

			// Parse the elicitations from the error data.
			var errorData struct {
				Elicitations []*ElicitParams `json:"elicitations"`
			}
			if rpcErr.Data != nil {
				if err := json.Unmarshal(rpcErr.Data, &errorData); err != nil {
					return nil, fmt.Errorf("failed to parse URL elicitation error data: %w", err)
				}
			}

			// Validate that all elicitations are URL mode.
			for _, elicit := range errorData.Elicitations {
				mode := elicit.Mode
				if mode == "" {
					mode = "form" // Default mode.
				}
				if mode != "url" {
					return nil, fmt.Errorf("URLElicitationRequired error must only contain URL mode elicitations, got %q", mode)
				}
			}

			// Register waiters for all elicitations before executing handlers
			// to avoid race condition where notification arrives before waiter is registered.
			type waiter struct {
				await   func(context.Context) error
				cleanup func()
			}
			waiters := make([]waiter, 0, len(errorData.Elicitations))
			for _, elicitParams := range errorData.Elicitations {
				await, cleanup := cs.registerElicitationWaiter(elicitParams.ElicitationID)
				waiters = append(waiters, waiter{await: await, cleanup: cleanup})
			}

			// Ensure cleanup happens even if we return early.
			defer func() {
				for _, w := range waiters {
					w.cleanup()
				}
			}()

			// Execute the elicitation handler for each elicitation.
			for _, elicitParams := range errorData.Elicitations {
				elicitReq := newClientRequest(cs, elicitParams)
				_, elicitErr := cs.client.elicit(ctx, elicitReq)
				if elicitErr != nil {
					return nil, fmt.Errorf("URL elicitation failed: %w", elicitErr)
				}
			}

			// Wait for all elicitations to complete.
			for _, w := range waiters {
				if err := w.await(ctx); err != nil {
					return nil, err
				}
			}

			// All elicitations complete, retry the original operation.
			return next(ctx, method, req)
		}
	}
}

func (c *Client) elicit(ctx context.Context, req *ElicitRequest) (*ElicitResult, error) {
	if c.opts.ElicitationHandler == nil {
		return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "client does not support elicitation"}
	}

	// Validate the elicitation parameters based on the mode.
	mode := req.Params.Mode
	if mode == "" {
		mode = "form"
	}

	switch mode {
	case "form":
		if req.Params.URL != "" {
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "URL must not be set for form elicitation"}
		}
		schema, err := validateElicitSchema(req.Params.RequestedSchema)
		if err != nil {
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: err.Error()}
		}
		res, err := c.opts.ElicitationHandler(ctx, req)
		if err != nil {
			return nil, err
		}
		// Validate elicitation result content against requested schema.
		if schema != nil && res.Content != nil {
			resolved, err := schema.Resolve(nil)
			if err != nil {
				return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("failed to resolve requested schema: %v", err)}
			}
			if err := resolved.Validate(res.Content); err != nil {
				return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("elicitation result content does not match requested schema: %v", err)}
			}
			err = resolved.ApplyDefaults(&res.Content)
			if err != nil {
				return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("failed to apply schema defalts to elicitation result: %v", err)}
			}
		}
		return res, nil
	case "url":
		if req.Params.RequestedSchema != nil {
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "requestedSchema must not be set for URL elicitation"}
		}
		if req.Params.URL == "" {
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "URL must be set for URL elicitation"}
		}
		// No schema validation for URL mode, just pass through to handler.
		return c.opts.ElicitationHandler(ctx, req)
	default:
		return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("unsupported elicitation mode: %q", mode)}
	}
}

// validateElicitSchema validates that the schema conforms to MCP elicitation schema requirements.
// Per the MCP specification, elicitation schemas are limited to flat objects with primitive properties only.
func validateElicitSchema(wireSchema any) (*jsonschema.Schema, error) {
	if wireSchema == nil {
		return nil, nil // nil schema is allowed
	}

	var schema *jsonschema.Schema
	if err := remarshal(wireSchema, &schema); err != nil {
		return nil, err
	}
	if schema == nil {
		return nil, nil
	}

	// The root schema must be of type "object" if specified
	if schema.Type != "" && schema.Type != "object" {
		return nil, fmt.Errorf("elicit schema must be of type 'object', got %q", schema.Type)
	}

	// Check if the schema has properties
	if schema.Properties != nil {
		for propName, propSchema := range schema.Properties {
			if propSchema == nil {
				continue
			}

			if err := validateElicitProperty(propName, propSchema); err != nil {
				return nil, err
			}
		}
	}

	return schema, nil
}

// validateElicitProperty validates a single property in an elicitation schema.
func validateElicitProperty(propName string, propSchema *jsonschema.Schema) error {
	// Check if this property has nested properties (not allowed)
	if len(propSchema.Properties) > 0 {
		return fmt.Errorf("elicit schema property %q contains nested properties, only primitive properties are allowed", propName)
	}
	// Validate based on the property type - only primitives are supported
	switch propSchema.Type {
	case "string":
		return validateElicitStringProperty(propName, propSchema)
	case "number", "integer":
		return validateElicitNumberProperty(propName, propSchema)
	case "boolean":
		return validateElicitBooleanProperty(propName, propSchema)
	default:
		return fmt.Errorf("elicit schema property %q has unsupported type %q, only string, number, integer, and boolean are allowed", propName, propSchema.Type)
	}
}

// validateElicitStringProperty validates string-type properties, including enums.
func validateElicitStringProperty(propName string, propSchema *jsonschema.Schema) error {
	// Handle enum validation (enums are a special case of strings)
	if len(propSchema.Enum) > 0 {
		// Enums must be string type (or untyped which defaults to string)
		if propSchema.Type != "" && propSchema.Type != "string" {
			return fmt.Errorf("elicit schema property %q has enum values but type is %q, enums are only supported for string type", propName, propSchema.Type)
		}
		// Enum values themselves are validated by the JSON schema library
		// Validate enumNames if present - must match enum length
		if propSchema.Extra != nil {
			if enumNamesRaw, exists := propSchema.Extra["enumNames"]; exists {
				// Type check enumNames - should be a slice
				if enumNamesSlice, ok := enumNamesRaw.([]any); ok {
					if len(enumNamesSlice) != len(propSchema.Enum) {
						return fmt.Errorf("elicit schema property %q has %d enum values but %d enumNames, they must match", propName, len(propSchema.Enum), len(enumNamesSlice))
					}
				} else {
					return fmt.Errorf("elicit schema property %q has invalid enumNames type, must be an array", propName)
				}
			}
		}
		return nil
	}

	// Validate format if specified - only specific formats are allowed
	if propSchema.Format != "" {
		allowedFormats := map[string]bool{
			"email":     true,
			"uri":       true,
			"date":      true,
			"date-time": true,
		}
		if !allowedFormats[propSchema.Format] {
			return fmt.Errorf("elicit schema property %q has unsupported format %q, only email, uri, date, and date-time are allowed", propName, propSchema.Format)
		}
	}

	// Validate minLength constraint if specified
	if propSchema.MinLength != nil {
		if *propSchema.MinLength < 0 {
			return fmt.Errorf("elicit schema property %q has invalid minLength %d, must be non-negative", propName, *propSchema.MinLength)
		}
	}

	// Validate maxLength constraint if specified
	if propSchema.MaxLength != nil {
		if *propSchema.MaxLength < 0 {
			return fmt.Errorf("elicit schema property %q has invalid maxLength %d, must be non-negative", propName, *propSchema.MaxLength)
		}
		// Check that maxLength >= minLength if both are specified
		if propSchema.MinLength != nil && *propSchema.MaxLength < *propSchema.MinLength {
			return fmt.Errorf("elicit schema property %q has maxLength %d less than minLength %d", propName, *propSchema.MaxLength, *propSchema.MinLength)
		}
	}

	return validateDefaultProperty[string](propName, propSchema)
}

// validateElicitNumberProperty validates number and integer-type properties.
func validateElicitNumberProperty(propName string, propSchema *jsonschema.Schema) error {
	if propSchema.Minimum != nil && propSchema.Maximum != nil {
		if *propSchema.Maximum < *propSchema.Minimum {
			return fmt.Errorf("elicit schema property %q has maximum %g less than minimum %g", propName, *propSchema.Maximum, *propSchema.Minimum)
		}
	}

	intDefaultError := validateDefaultProperty[int](propName, propSchema)
	floatDefaultError := validateDefaultProperty[float64](propName, propSchema)
	if intDefaultError != nil && floatDefaultError != nil {
		return fmt.Errorf("elicit schema property %q has default value that cannot be interpreted as an int or float", propName)
	}

	return nil
}

// validateElicitBooleanProperty validates boolean-type properties.
func validateElicitBooleanProperty(propName string, propSchema *jsonschema.Schema) error {
	return validateDefaultProperty[bool](propName, propSchema)
}

func validateDefaultProperty[T any](propName string, propSchema *jsonschema.Schema) error {
	// Validate default value if specified - must be a valid T
	if propSchema.Default != nil {
		var defaultValue T
		if err := json.Unmarshal(propSchema.Default, &defaultValue); err != nil {
			return fmt.Errorf("elicit schema property %q has invalid default value, must be a %T: %v", propName, defaultValue, err)
		}
	}
	return nil
}

// AddSendingMiddleware wraps the current sending method handler using the provided
// middleware. Middleware is applied from right to left, so that the first one is
// executed first.
//
// For example, AddSendingMiddleware(m1, m2, m3) augments the method handler as
// m1(m2(m3(handler))).
//
// Sending middleware is called when a request is sent. It is useful for tasks
// such as tracing, metrics, and adding progress tokens.
func (c *Client) AddSendingMiddleware(middleware ...Middleware) {
	c.mu.Lock()
	defer c.mu.Unlock()
	addMiddleware(&c.sendingMethodHandler_, middleware)
}

// AddReceivingMiddleware wraps the current receiving method handler using
// the provided middleware. Middleware is applied from right to left, so that the
// first one is executed first.
//
// For example, AddReceivingMiddleware(m1, m2, m3) augments the method handler as
// m1(m2(m3(handler))).
//
// Receiving middleware is called when a request is received. It is useful for tasks
// such as authentication, request logging and metrics.
func (c *Client) AddReceivingMiddleware(middleware ...Middleware) {
	c.mu.Lock()
	defer c.mu.Unlock()
	addMiddleware(&c.receivingMethodHandler_, middleware)
}

// clientMethodInfos maps from the RPC method name to serverMethodInfos.
//
// The 'allowMissingParams' values are extracted from the protocol schema.
// TODO(rfindley): actually load and validate the protocol schema, rather than
// curating these method flags.
var clientMethodInfos = map[string]methodInfo{
	methodComplete:                  newClientMethodInfo(clientSessionMethod((*ClientSession).Complete), 0),
	methodPing:                      newClientMethodInfo(clientSessionMethod((*ClientSession).ping), missingParamsOK),
	methodListRoots:                 newClientMethodInfo(clientMethod((*Client).listRoots), missingParamsOK),
	methodCreateMessage:             newClientMethodInfo(clientMethod((*Client).createMessage), 0),
	methodElicit:                    newClientMethodInfo(clientMethod((*Client).elicit), missingParamsOK),
	notificationCancelled:           newClientMethodInfo(clientSessionMethod((*ClientSession).cancel), notification|missingParamsOK),
	notificationToolListChanged:     newClientMethodInfo(clientMethod((*Client).callToolChangedHandler), notification|missingParamsOK),
	notificationPromptListChanged:   newClientMethodInfo(clientMethod((*Client).callPromptChangedHandler), notification|missingParamsOK),
	notificationResourceListChanged: newClientMethodInfo(clientMethod((*Client).callResourceChangedHandler), notification|missingParamsOK),
	notificationResourceUpdated:     newClientMethodInfo(clientMethod((*Client).callResourceUpdatedHandler), notification|missingParamsOK),
	notificationLoggingMessage:      newClientMethodInfo(clientMethod((*Client).callLoggingHandler), notification),
	notificationProgress:            newClientMethodInfo(clientSessionMethod((*ClientSession).callProgressNotificationHandler), notification),
	notificationElicitationComplete: newClientMethodInfo(clientMethod((*Client).callElicitationCompleteHandler), notification|missingParamsOK),
}

func (cs *ClientSession) sendingMethodInfos() map[string]methodInfo {
	return serverMethodInfos
}

func (cs *ClientSession) receivingMethodInfos() map[string]methodInfo {
	return clientMethodInfos
}

func (cs *ClientSession) handle(ctx context.Context, req *jsonrpc.Request) (any, error) {
	if req.IsCall() {
		jsonrpc2.Async(ctx)
	}
	return handleReceive(ctx, cs, req)
}

func (cs *ClientSession) sendingMethodHandler() MethodHandler {
	cs.client.mu.Lock()
	defer cs.client.mu.Unlock()
	return cs.client.sendingMethodHandler_
}

func (cs *ClientSession) receivingMethodHandler() MethodHandler {
	cs.client.mu.Lock()
	defer cs.client.mu.Unlock()
	return cs.client.receivingMethodHandler_
}

// getConn implements [Session.getConn].
func (cs *ClientSession) getConn() *jsonrpc2.Connection { return cs.conn }

func (*ClientSession) ping(context.Context, *PingParams) (*emptyResult, error) {
	return &emptyResult{}, nil
}

// cancel is a placeholder: cancellation is handled the jsonrpc2 package.
//
// It should never be invoked in practice because cancellation is preempted,
// but having its signature here facilitates the construction of methodInfo
// that can be used to validate incoming cancellation notifications.
func (*ClientSession) cancel(context.Context, *CancelledParams) (Result, error) {
	return nil, nil
}

func newClientRequest[P Params](cs *ClientSession, params P) *ClientRequest[P] {
	return &ClientRequest[P]{Session: cs, Params: params}
}

// Ping makes an MCP "ping" request to the server.
func (cs *ClientSession) Ping(ctx context.Context, params *PingParams) error {
	_, err := handleSend[*emptyResult](ctx, methodPing, newClientRequest(cs, orZero[Params](params)))
	return err
}

// ListPrompts lists prompts that are currently available on the server.
func (cs *ClientSession) ListPrompts(ctx context.Context, params *ListPromptsParams) (*ListPromptsResult, error) {
	return handleSend[*ListPromptsResult](ctx, methodListPrompts, newClientRequest(cs, orZero[Params](params)))
}

// GetPrompt gets a prompt from the server.
func (cs *ClientSession) GetPrompt(ctx context.Context, params *GetPromptParams) (*GetPromptResult, error) {
	return handleSend[*GetPromptResult](ctx, methodGetPrompt, newClientRequest(cs, orZero[Params](params)))
}

// ListTools lists tools that are currently available on the server.
func (cs *ClientSession) ListTools(ctx context.Context, params *ListToolsParams) (*ListToolsResult, error) {
	return handleSend[*ListToolsResult](ctx, methodListTools, newClientRequest(cs, orZero[Params](params)))
}

// CallTool calls the tool with the given parameters.
//
// The params.Arguments can be any value that marshals into a JSON object.
func (cs *ClientSession) CallTool(ctx context.Context, params *CallToolParams) (*CallToolResult, error) {
	if params == nil {
		params = new(CallToolParams)
	}
	if params.Arguments == nil {
		// Avoid sending nil over the wire.
		params.Arguments = map[string]any{}
	}
	return handleSend[*CallToolResult](ctx, methodCallTool, newClientRequest(cs, orZero[Params](params)))
}

func (cs *ClientSession) SetLoggingLevel(ctx context.Context, params *SetLoggingLevelParams) error {
	_, err := handleSend[*emptyResult](ctx, methodSetLevel, newClientRequest(cs, orZero[Params](params)))
	return err
}

// ListResources lists the resources that are currently available on the server.
func (cs *ClientSession) ListResources(ctx context.Context, params *ListResourcesParams) (*ListResourcesResult, error) {
	return handleSend[*ListResourcesResult](ctx, methodListResources, newClientRequest(cs, orZero[Params](params)))
}

// ListResourceTemplates lists the resource templates that are currently available on the server.
func (cs *ClientSession) ListResourceTemplates(ctx context.Context, params *ListResourceTemplatesParams) (*ListResourceTemplatesResult, error) {
	return handleSend[*ListResourceTemplatesResult](ctx, methodListResourceTemplates, newClientRequest(cs, orZero[Params](params)))
}

// ReadResource asks the server to read a resource and return its contents.
func (cs *ClientSession) ReadResource(ctx context.Context, params *ReadResourceParams) (*ReadResourceResult, error) {
	return handleSend[*ReadResourceResult](ctx, methodReadResource, newClientRequest(cs, orZero[Params](params)))
}

func (cs *ClientSession) Complete(ctx context.Context, params *CompleteParams) (*CompleteResult, error) {
	return handleSend[*CompleteResult](ctx, methodComplete, newClientRequest(cs, orZero[Params](params)))
}

// Subscribe sends a "resources/subscribe" request to the server, asking for
// notifications when the specified resource changes.
func (cs *ClientSession) Subscribe(ctx context.Context, params *SubscribeParams) error {
	_, err := handleSend[*emptyResult](ctx, methodSubscribe, newClientRequest(cs, orZero[Params](params)))
	return err
}

// Unsubscribe sends a "resources/unsubscribe" request to the server, cancelling
// a previous subscription.
func (cs *ClientSession) Unsubscribe(ctx context.Context, params *UnsubscribeParams) error {
	_, err := handleSend[*emptyResult](ctx, methodUnsubscribe, newClientRequest(cs, orZero[Params](params)))
	return err
}

func (c *Client) callToolChangedHandler(ctx context.Context, req *ToolListChangedRequest) (Result, error) {
	if h := c.opts.ToolListChangedHandler; h != nil {
		h(ctx, req)
	}
	return nil, nil
}

func (c *Client) callPromptChangedHandler(ctx context.Context, req *PromptListChangedRequest) (Result, error) {
	if h := c.opts.PromptListChangedHandler; h != nil {
		h(ctx, req)
	}
	return nil, nil
}

func (c *Client) callResourceChangedHandler(ctx context.Context, req *ResourceListChangedRequest) (Result, error) {
	if h := c.opts.ResourceListChangedHandler; h != nil {
		h(ctx, req)
	}
	return nil, nil
}

func (c *Client) callResourceUpdatedHandler(ctx context.Context, req *ResourceUpdatedNotificationRequest) (Result, error) {
	if h := c.opts.ResourceUpdatedHandler; h != nil {
		h(ctx, req)
	}
	return nil, nil
}

func (c *Client) callLoggingHandler(ctx context.Context, req *LoggingMessageRequest) (Result, error) {
	if h := c.opts.LoggingMessageHandler; h != nil {
		h(ctx, req)
	}
	return nil, nil
}

func (cs *ClientSession) callProgressNotificationHandler(ctx context.Context, params *ProgressNotificationParams) (Result, error) {
	if h := cs.client.opts.ProgressNotificationHandler; h != nil {
		h(ctx, clientRequestFor(cs, params))
	}
	return nil, nil
}

func (c *Client) callElicitationCompleteHandler(ctx context.Context, req *ElicitationCompleteNotificationRequest) (Result, error) {
	// Check if there's a pending elicitation waiting for this notification.
	if cs, ok := req.GetSession().(*ClientSession); ok {
		cs.pendingElicitationsMu.Lock()
		if ch, exists := cs.pendingElicitations[req.Params.ElicitationID]; exists {
			select {
			case ch <- struct{}{}:
			default:
				// Channel already signaled.
			}
		}
		cs.pendingElicitationsMu.Unlock()
	}

	// Call the user's handler if provided.
	if h := c.opts.ElicitationCompleteHandler; h != nil {
		h(ctx, req)
	}
	return nil, nil
}

// NotifyProgress sends a progress notification from the client to the server
// associated with this session.
// This can be used if the client is performing a long-running task that was
// initiated by the server.
func (cs *ClientSession) NotifyProgress(ctx context.Context, params *ProgressNotificationParams) error {
	return handleNotify(ctx, notificationProgress, newClientRequest(cs, orZero[Params](params)))
}

// Tools provides an iterator for all tools available on the server,
// automatically fetching pages and managing cursors.
// The params argument can set the initial cursor.
// Iteration stops at the first encountered error, which will be yielded.
func (cs *ClientSession) Tools(ctx context.Context, params *ListToolsParams) iter.Seq2[*Tool, error] {
	if params == nil {
		params = &ListToolsParams{}
	}
	return paginate(ctx, params, cs.ListTools, func(res *ListToolsResult) []*Tool {
		return res.Tools
	})
}

// Resources provides an iterator for all resources available on the server,
// automatically fetching pages and managing cursors.
// The params argument can set the initial cursor.
// Iteration stops at the first encountered error, which will be yielded.
func (cs *ClientSession) Resources(ctx context.Context, params *ListResourcesParams) iter.Seq2[*Resource, error] {
	if params == nil {
		params = &ListResourcesParams{}
	}
	return paginate(ctx, params, cs.ListResources, func(res *ListResourcesResult) []*Resource {
		return res.Resources
	})
}

// ResourceTemplates provides an iterator for all resource templates available on the server,
// automatically fetching pages and managing cursors.
// The params argument can set the initial cursor.
// Iteration stops at the first encountered error, which will be yielded.
func (cs *ClientSession) ResourceTemplates(ctx context.Context, params *ListResourceTemplatesParams) iter.Seq2[*ResourceTemplate, error] {
	if params == nil {
		params = &ListResourceTemplatesParams{}
	}
	return paginate(ctx, params, cs.ListResourceTemplates, func(res *ListResourceTemplatesResult) []*ResourceTemplate {
		return res.ResourceTemplates
	})
}

// Prompts provides an iterator for all prompts available on the server,
// automatically fetching pages and managing cursors.
// The params argument can set the initial cursor.
// Iteration stops at the first encountered error, which will be yielded.
func (cs *ClientSession) Prompts(ctx context.Context, params *ListPromptsParams) iter.Seq2[*Prompt, error] {
	if params == nil {
		params = &ListPromptsParams{}
	}
	return paginate(ctx, params, cs.ListPrompts, func(res *ListPromptsResult) []*Prompt {
		return res.Prompts
	})
}

// paginate is a generic helper function to provide a paginated iterator.
func paginate[P listParams, R listResult[T], T any](ctx context.Context, params P, listFunc func(context.Context, P) (R, error), items func(R) []*T) iter.Seq2[*T, error] {
	return func(yield func(*T, error) bool) {
		for {
			res, err := listFunc(ctx, params)
			if err != nil {
				yield(nil, err)
				return
			}
			for _, r := range items(res) {
				if !yield(r, nil) {
					return
				}
			}
			nextCursorVal := res.nextCursorPtr()
			if nextCursorVal == nil || *nextCursorVal == "" {
				return
			}
			*params.cursorPtr() = *nextCursorVal
		}
	}
}
