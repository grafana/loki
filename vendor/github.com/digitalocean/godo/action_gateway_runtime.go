package godo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
)

const (
	actionGatewaySearchTool          = "action_search"
	actionGatewayInvokeTool          = "action_invoke"
	actionGatewayCodeTool            = "action_code"
	actionGatewayMCPVersion          = "2025-06-18"
	actionGatewayMaxRPCResponseBytes = 32 << 20
)

// WithActionGatewayMCPBaseURL replaces only the origin of returned session MCP
// URLs. HTTP is permitted for loopback development servers only.
func WithActionGatewayMCPBaseURL(rawURL string) ClientOpt {
	return func(client *Client) error {
		endpoint, err := url.Parse(rawURL)
		if err != nil {
			return err
		}
		if err = validateActionGatewayURL(endpoint, true); err != nil {
			return err
		}
		client.actionGatewayMCPBaseURL = endpoint
		return nil
	}
}

func validateActionGatewayURL(endpoint *url.URL, allowLoopback bool) error {
	if endpoint == nil || endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" || endpoint.Opaque != "" {
		return errors.New("action gateway: invalid MCP URL")
	}
	if endpoint.Scheme == "https" {
		return nil
	}
	if allowLoopback && endpoint.Scheme == "http" {
		host := endpoint.Hostname()
		if host == "localhost" || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()) {
			return nil
		}
	}
	return errors.New("action gateway: MCP URL must use HTTPS")
}

// ActionGatewaySession is a control-plane session bound to its MCP endpoint.
type ActionGatewaySession struct {
	Record    *ActionGatewaySessionRecord
	MCPURL    string
	ActorID   string
	Tools     *ActionGatewaySessionTools
	Code      *ActionGatewaySessionCode
	client    *Client
	endpoint  *url.URL
	actorID   string
	sessionID string
	requestID atomic.Int64
}

func newActionGatewaySession(client *Client, created *ActionGatewaySessionCreateResponse, actorID string) (*ActionGatewaySession, error) {
	if created == nil || created.Session == nil || created.Session.SessionURN == "" || created.MCPURL == "" {
		return nil, &ActionGatewayProtocolError{Message: "session create response lacks a session URN or MCP URL"}
	}
	endpoint, err := url.Parse(created.MCPURL)
	if err != nil {
		return nil, err
	}
	if err = validateActionGatewayURL(endpoint, false); err != nil {
		return nil, err
	}
	if client.actionGatewayMCPBaseURL == nil && (!strings.EqualFold(endpoint.Hostname(), "actions.do-ai.run") || (endpoint.Port() != "" && endpoint.Port() != "443")) {
		return nil, &ActionGatewayProtocolError{Message: "untrusted MCP URL origin"}
	}
	if client.actionGatewayMCPBaseURL != nil {
		endpoint.Scheme = client.actionGatewayMCPBaseURL.Scheme
		endpoint.Host = client.actionGatewayMCPBaseURL.Host
	}
	if err = validateActionGatewayURL(endpoint, client.actionGatewayMCPBaseURL != nil); err != nil {
		return nil, err
	}
	parts := strings.Split(created.Session.SessionURN, ":")
	session := &ActionGatewaySession{Record: created.Session, MCPURL: endpoint.String(), ActorID: actorID, client: client, endpoint: endpoint, actorID: actorID, sessionID: parts[len(parts)-1]}
	session.Tools = &ActionGatewaySessionTools{session: session}
	session.Code = &ActionGatewaySessionCode{session: session}
	return session, nil
}

// ActionGatewayProtocolError reports a malformed or failed MCP JSON-RPC response.
type ActionGatewayProtocolError struct {
	Message string
	Code    int
	Data    json.RawMessage
}

func (err *ActionGatewayProtocolError) Error() string {
	return "action gateway protocol: " + err.Message
}

// ActionGatewayToolError reports an individual failed tool invocation.
type ActionGatewayToolError struct {
	Message      string          `json:"message"`
	Class        string          `json:"class,omitempty"`
	Retriable    bool            `json:"retriable,omitempty"`
	RecoveryHint string          `json:"recovery_hint,omitempty"`
	InvocationID string          `json:"invocation_id,omitempty"`
	Meta         json.RawMessage `json:"_meta,omitempty"`
}

func (err *ActionGatewayToolError) Error() string { return "action gateway tool: " + err.Message }

func (session *ActionGatewaySession) headers(request *http.Request) {
	request.Header.Set("MCP-Protocol-Version", actionGatewayMCPVersion)
	request.Header.Set("X-Actor-Id", session.actorID)
	request.Header.Set("X-Session-Id", session.sessionID)
}

func (session *ActionGatewaySession) do(ctx context.Context, endpoint string, body interface{}, accept string, readBody func(*http.Response) ([]byte, error)) ([]byte, *Response, error) {
	request, err := session.client.NewRequest(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return nil, nil, err
	}
	session.headers(request)
	request.Header.Set("Accept", accept)
	client := *session.client.HTTPClient
	previousRedirect := client.CheckRedirect
	client.CheckRedirect = func(redirect *http.Request, via []*http.Request) error {
		if redirect.URL.Scheme != request.URL.Scheme || redirect.URL.Host != request.URL.Host {
			return errors.New("action gateway: cross-origin redirect refused")
		}
		if previousRedirect != nil {
			return previousRedirect(redirect, via)
		}
		if len(via) >= 10 {
			return errors.New("action gateway: too many redirects")
		}
		return nil
	}
	if session.client.rateLimiter != nil {
		if err = session.client.rateLimiter.Wait(ctx); err != nil {
			return nil, nil, err
		}
	}
	actual, err := DoRequestWithClient(ctx, &client, request)
	if err != nil {
		return nil, nil, err
	}
	defer actual.Body.Close()
	if session.client.onRequestCompleted != nil {
		session.client.onRequestCompleted(request, actual)
	}
	response := newResponse(actual)
	session.client.ratemtx.Lock()
	session.client.Rate = response.Rate
	session.client.ratemtx.Unlock()
	if err = CheckResponse(actual); err != nil {
		return nil, response, err
	}
	if readBody == nil {
		readBody = func(response *http.Response) ([]byte, error) { return io.ReadAll(response.Body) }
	}
	data, err := readBody(actual)
	return data, response, err
}

func (session *ActionGatewaySession) rpc(ctx context.Context, method string, params interface{}) (json.RawMessage, *Response, error) {
	requestID := session.requestID.Add(1)
	message := map[string]interface{}{"jsonrpc": "2.0", "id": requestID, "method": method}
	if params != nil {
		message["params"] = params
	}
	data, response, err := session.do(ctx, session.endpoint.String(), message, "application/json, text/event-stream", func(actual *http.Response) ([]byte, error) {
		mediaType, _, _ := mime.ParseMediaType(actual.Header.Get("Content-Type"))
		if mediaType == "text/event-stream" {
			limited := &io.LimitedReader{R: actual.Body, N: actionGatewayMaxRPCResponseBytes + 1}
			reader := NewSSEReader(limited)
			for {
				event, err := reader.Next()
				if limited.N == 0 {
					return nil, &ActionGatewayProtocolError{Message: "SSE response exceeds size limit"}
				}
				if errors.Is(err, io.EOF) {
					return nil, &ActionGatewayProtocolError{Message: "SSE stream ended without a matching JSON-RPC response"}
				}
				if err != nil {
					return nil, err
				}
				var candidate struct {
					ID     json.RawMessage `json:"id"`
					Result json.RawMessage `json:"result"`
					Error  json.RawMessage `json:"error"`
				}
				if json.Unmarshal(event.Data, &candidate) != nil || (len(candidate.Result) == 0 && len(candidate.Error) == 0) {
					continue
				}
				var eventID int64
				if json.Unmarshal(candidate.ID, &eventID) != nil {
					return nil, &ActionGatewayProtocolError{Message: "missing or invalid JSON-RPC response ID"}
				}
				if eventID != requestID {
					return nil, &ActionGatewayProtocolError{Message: "mismatched JSON-RPC response ID"}
				}
				return append([]byte(nil), event.Data...), nil
			}
		}
		data, err := io.ReadAll(io.LimitReader(actual.Body, actionGatewayMaxRPCResponseBytes+1))
		if len(data) > actionGatewayMaxRPCResponseBytes {
			return nil, &ActionGatewayProtocolError{Message: "JSON-RPC response exceeds size limit"}
		}
		return data, err
	})
	if err != nil {
		return nil, response, err
	}
	var envelope struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code    int             `json:"code"`
			Message string          `json:"message"`
			Data    json.RawMessage `json:"data"`
		} `json:"error"`
	}
	if err = json.Unmarshal(data, &envelope); err != nil {
		return nil, response, &ActionGatewayProtocolError{Message: "malformed JSON-RPC response"}
	}
	if envelope.JSONRPC != "2.0" {
		return nil, response, &ActionGatewayProtocolError{Message: "missing JSON-RPC version"}
	}
	if !bytes.Equal(bytes.TrimSpace(envelope.ID), []byte(strconv.FormatInt(requestID, 10))) {
		return nil, response, &ActionGatewayProtocolError{Message: "mismatched JSON-RPC response ID"}
	}
	if envelope.Error != nil {
		return nil, response, &ActionGatewayProtocolError{Message: envelope.Error.Message, Code: envelope.Error.Code, Data: envelope.Error.Data}
	}
	if len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		return nil, response, &ActionGatewayProtocolError{Message: "missing JSON-RPC result"}
	}
	return envelope.Result, response, nil
}

// ActionGatewayMCPTool is a session-visible MCP tool definition.
type ActionGatewayMCPTool struct {
	Name         string          `json:"name"`
	Title        string          `json:"title"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema"`
}

// ActionGatewaySessionTools discovers and invokes bound session tools.
type ActionGatewaySessionTools struct{ session *ActionGatewaySession }

// List retrieves the three meta-tools by default; includeAll also retrieves concrete tools.
func (tools *ActionGatewaySessionTools) List(ctx context.Context, includeAll bool) ([]ActionGatewayMCPTool, *Response, error) {
	result, response, err := tools.session.rpc(ctx, "tools/list", nil)
	if err != nil {
		return nil, response, err
	}
	var catalog struct {
		Tools []ActionGatewayMCPTool `json:"tools"`
	}
	if err = json.Unmarshal(result, &catalog); err != nil {
		return nil, response, err
	}
	if includeAll {
		return catalog.Tools, response, nil
	}
	meta := make([]ActionGatewayMCPTool, 0, 3)
	for _, tool := range catalog.Tools {
		if tool.Name == actionGatewaySearchTool || tool.Name == actionGatewayInvokeTool || tool.Name == actionGatewayCodeTool {
			meta = append(meta, tool)
		}
	}
	return meta, response, nil
}

// ActionGatewayToolQuery describes a use case to search for tools.
type ActionGatewayToolQuery struct {
	UseCase     string `json:"use_case"`
	KnownFields string `json:"known_fields,omitempty"`
}

// ActionGatewayToolSearchRequest specifies use cases and optional filters.
type ActionGatewayToolSearchRequest struct {
	Queries   []ActionGatewayToolQuery `json:"queries"`
	Providers []string                 `json:"providers,omitempty"`
	Tags      []string                 `json:"tags,omitempty"`
	Limit     *int                     `json:"limit,omitempty"`
}

// Search discovers catalog tools by use case.
func (tools *ActionGatewaySessionTools) Search(ctx context.Context, query *ActionGatewayToolSearchRequest) (json.RawMessage, *Response, error) {
	if query == nil || len(query.Queries) < 1 || len(query.Queries) > 5 {
		return nil, nil, NewArgError("queries", "must contain 1 to 5 use cases")
	}
	for _, entry := range query.Queries {
		if strings.TrimSpace(entry.UseCase) == "" {
			return nil, nil, NewArgError("useCase", "cannot be empty")
		}
	}
	return tools.Call(ctx, actionGatewaySearchTool, query)
}

// ActionGatewayInvokeTool describes one invocation and its JSON arguments.
type ActionGatewayInvokeTool struct {
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
}

// ActionGatewayInvokeResult contains one item in a batch invocation.
type ActionGatewayInvokeResult struct {
	Tool   string `json:"tool"`
	Result struct {
		Status string                  `json:"status"`
		Output json.RawMessage         `json:"output"`
		Error  *ActionGatewayToolError `json:"error"`
		Meta   json.RawMessage         `json:"_meta"`
	} `json:"result"`
}

// ActionGatewayInvokeResponse contains the full invocation batch envelope.
type ActionGatewayInvokeResponse struct {
	TotalCount   int                         `json:"total_count"`
	SuccessCount int                         `json:"success_count"`
	ErrorCount   int                         `json:"error_count"`
	Results      []ActionGatewayInvokeResult `json:"results"`
}

// Invoke runs one to ten tools and retains individual failures in the envelope.
func (tools *ActionGatewaySessionTools) Invoke(ctx context.Context, entries []ActionGatewayInvokeTool, rationale string) (*ActionGatewayInvokeResponse, *Response, error) {
	if len(entries) < 1 || len(entries) > 10 {
		return nil, nil, NewArgError("tools", "must contain 1 to 10 tools")
	}
	for _, entry := range entries {
		if entry.Tool == "" {
			return nil, nil, NewArgError("tool", "cannot be empty")
		}
	}
	entries = append([]ActionGatewayInvokeTool(nil), entries...)
	for index := range entries {
		entries[index].Arguments = actionGatewayArguments(entries[index].Arguments)
	}
	arguments := map[string]interface{}{"tools": entries}
	if rationale != "" {
		arguments["rationale"] = rationale
	}
	result, response, err := tools.Call(ctx, actionGatewayInvokeTool, arguments)
	if err != nil {
		return nil, response, err
	}
	var batch ActionGatewayInvokeResponse
	if err = json.Unmarshal(result, &batch); err != nil {
		return nil, response, err
	}
	return &batch, response, nil
}

// InvokeOne invokes a single tool and returns its output or individual failure.
func (tools *ActionGatewaySessionTools) InvokeOne(ctx context.Context, name string, arguments json.RawMessage, rationale string) (json.RawMessage, *Response, error) {
	batch, response, err := tools.Invoke(ctx, []ActionGatewayInvokeTool{{Tool: name, Arguments: arguments}}, rationale)
	if err != nil {
		return nil, response, err
	}
	if len(batch.Results) == 0 {
		return nil, response, &ActionGatewayProtocolError{Message: "invocation returned no results"}
	}
	first := batch.Results[0].Result
	if first.Status != "" && first.Status != "succeeded" {
		if first.Error != nil {
			first.Error.Meta = first.Meta
			return nil, response, first.Error
		}
		return nil, response, &ActionGatewayToolError{Message: "tool invocation failed"}
	}
	return first.Output, response, nil
}

// Call invokes one MCP tool directly, including the gateway meta-tools.
func (tools *ActionGatewaySessionTools) Call(ctx context.Context, name string, arguments interface{}) (json.RawMessage, *Response, error) {
	if strings.TrimSpace(name) == "" {
		return nil, nil, NewArgError("name", "cannot be empty")
	}
	if arguments == nil {
		arguments = map[string]interface{}{}
	}
	result, response, err := tools.session.rpc(ctx, "tools/call", map[string]interface{}{"name": name, "arguments": arguments})
	if err != nil {
		return nil, response, err
	}
	var call struct {
		IsError    bool            `json:"isError"`
		Structured json.RawMessage `json:"structuredContent"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Meta json.RawMessage `json:"_meta"`
	}
	if err = json.Unmarshal(result, &call); err != nil {
		return nil, response, err
	}
	output := call.Structured
	if len(output) == 0 || string(output) == "null" {
		var text strings.Builder
		for _, block := range call.Content {
			if block.Type == "text" {
				if text.Len() != 0 {
					text.WriteByte('\n')
				}
				text.WriteString(block.Text)
			}
		}
		output = []byte(text.String())
		if !json.Valid(output) {
			output, _ = json.Marshal(text.String())
		}
	}
	if call.IsError {
		failure := &ActionGatewayToolError{Message: "tool call failed", Meta: call.Meta}
		var details struct {
			Error        *ActionGatewayToolError `json:"error"`
			InvocationID string                  `json:"invocation_id"`
			Message      string                  `json:"message"`
		}
		if json.Unmarshal(output, &details) == nil {
			if details.Error != nil {
				failure = details.Error
				failure.Meta = call.Meta
			}
			if details.Message != "" {
				failure.Message = details.Message
			}
			if details.InvocationID != "" {
				failure.InvocationID = details.InvocationID
			}
		}
		return nil, response, failure
	}
	return output, response, nil
}

// ActionGatewaySessionCode executes sandboxed Python through action_code.
type ActionGatewaySessionCode struct{ session *ActionGatewaySession }

// Execute runs code in the gateway sandbox.
func (code *ActionGatewaySessionCode) Execute(ctx context.Context, source, thought string) (json.RawMessage, *Response, error) {
	if source == "" {
		return nil, nil, NewArgError("source", "cannot be empty")
	}
	arguments := map[string]string{"code": source}
	if thought != "" {
		arguments["thought"] = thought
	}
	return code.session.Tools.Call(ctx, actionGatewayCodeTool, arguments)
}

// Approve approves a pending tool invocation.
func (session *ActionGatewaySession) Approve(ctx context.Context, approvalID string) (json.RawMessage, *Response, error) {
	return session.decide(ctx, approvalID, "approve")
}

// Deny denies a pending tool invocation.
func (session *ActionGatewaySession) Deny(ctx context.Context, approvalID string) (json.RawMessage, *Response, error) {
	return session.decide(ctx, approvalID, "deny")
}

func (session *ActionGatewaySession) decide(ctx context.Context, approvalID, decision string) (json.RawMessage, *Response, error) {
	if strings.TrimSpace(approvalID) == "" {
		return nil, nil, NewArgError("approvalID", "cannot be empty")
	}
	endpoint := *session.endpoint
	endpoint.Path = "/approvals/" + approvalID
	endpoint.RawPath = "/approvals/" + url.PathEscape(approvalID)
	endpoint.RawQuery = ""
	data, response, err := session.do(ctx, endpoint.String(), map[string]string{"decision": decision}, "application/json", nil)
	if err != nil {
		return nil, response, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, response, nil
	}
	if !json.Valid(data) {
		return nil, response, &ActionGatewayProtocolError{Message: fmt.Sprintf("invalid approval response: %q", data)}
	}
	return data, response, nil
}
