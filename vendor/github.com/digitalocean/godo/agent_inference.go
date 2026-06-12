package godo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Agent Inference API (https://docs.digitalocean.com/reference/api/reference/agent-inference/).
// Each provisioned agent has its own base URL (e.g. https://abc123.agents.do-ai.run)
// and an agent_access_key that is NOT a dop_v1_* token, so this API lives on its
// own *AgentInferenceClient (one per agent endpoint)

const (
	agentInferenceChatCompletionsPath = "api/v1/chat/completions"
	agentInferenceQueryAgent          = "agent"
)

type AgentInferenceClient struct {
	Chat *AgentChatService

	baseURL    *url.URL
	accessKey  string
	httpClient *http.Client
	userAgent  string

	headers            map[string]string
	onRequestCompleted RequestCompletionCallback
}

// AgentInferenceClientOpt is a functional option for an AgentInferenceClient.
type AgentInferenceClientOpt func(*AgentInferenceClient) error

type AgentChatService struct {
	Completions *AgentChatCompletionService
}

// AgentChatCompletionService exposes POST /api/v1/chat/completions?agent=true.
type AgentChatCompletionService struct {
	parent *AgentInferenceClient
}

func NewAgentInferenceClient(baseURL, accessKey string, opts ...AgentInferenceClientOpt) (*AgentInferenceClient, error) {
	if baseURL == "" {
		return nil, errors.New("agent inference: baseURL is required")
	}
	if accessKey == "" {
		return nil, errors.New("agent inference: accessKey is required")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("agent inference: parse baseURL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("agent inference: baseURL %q is missing scheme or host", baseURL)
	}
	if !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}

	c := &AgentInferenceClient{
		baseURL:    u,
		accessKey:  accessKey,
		httpClient: &http.Client{},
		userAgent:  userAgent,
	}
	c.Chat = &AgentChatService{Completions: &AgentChatCompletionService{parent: c}}

	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// SetAgentHTTPClient overrides the http.Client used for all requests.
func SetAgentHTTPClient(hc *http.Client) AgentInferenceClientOpt {
	return func(c *AgentInferenceClient) error {
		if hc == nil {
			return errors.New("agent inference: http client must not be nil")
		}
		c.httpClient = hc
		return nil
	}
}

// SetAgentUserAgent prepends ua to the User-Agent header.
func SetAgentUserAgent(ua string) AgentInferenceClientOpt {
	return func(c *AgentInferenceClient) error {
		c.userAgent = fmt.Sprintf("%s %s", ua, c.userAgent)
		return nil
	}
}

// SetAgentRequestHeaders adds default request headers. Authorization, Content-Type,
// Accept, and User-Agent are reserved and overwritten by the client.
func SetAgentRequestHeaders(headers map[string]string) AgentInferenceClientOpt {
	return func(c *AgentInferenceClient) error {
		if len(headers) == 0 {
			return nil
		}
		if c.headers == nil {
			c.headers = make(map[string]string, len(headers))
		}
		for k, v := range headers {
			c.headers[k] = v
		}
		return nil
	}
}

// OnRequestCompleted registers a callback fired after each HTTP request.
func (c *AgentInferenceClient) OnRequestCompleted(rc RequestCompletionCallback) {
	c.onRequestCompleted = rc
}

// BaseURL returns the (normalised) agent base URL.
func (c *AgentInferenceClient) BaseURL() *url.URL {
	u := *c.baseURL
	return &u
}

// New creates a non-streaming agent chat completion.
func (s *AgentChatCompletionService) New(ctx context.Context, body *ChatCompletionNewParams) (*ChatCompletion, *Response, error) {
	if body == nil {
		return nil, nil, errors.New("agent inference: body is required")
	}
	req, err := s.parent.newRequest(ctx, http.MethodPost, agentInferenceChatCompletionsPath, body)
	if err != nil {
		return nil, nil, err
	}
	root := new(ChatCompletion)
	resp, err := s.parent.do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// NewStreaming opens an SSE stream of chat completion chunks; body.Stream is forced to true.
// Callers MUST Close the returned stream.
func (s *AgentChatCompletionService) NewStreaming(ctx context.Context, body *ChatCompletionNewParams) (*ChatCompletionStream, *Response, error) {
	if body == nil {
		return nil, nil, errors.New("agent inference: body is required")
	}
	body.Stream = PtrTo(true)
	req, err := s.parent.newRequest(ctx, http.MethodPost, agentInferenceChatCompletionsPath, body)
	if err != nil {
		return nil, nil, err
	}
	raw, resp, err := s.parent.doStream(ctx, req)
	if err != nil {
		return nil, resp, err
	}
	return &ChatCompletionStream{raw: raw}, resp, nil
}

// newRequest builds a request against the agent base URL with ?agent=true and bearer auth.
func (c *AgentInferenceClient) newRequest(ctx context.Context, method, path string, body interface{}) (*http.Request, error) {
	rel, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("agent inference: parse path: %w", err)
	}
	u := c.baseURL.ResolveReference(rel)

	q := u.Query()
	q.Set(agentInferenceQueryAgent, "true")
	u.RawQuery = q.Encode()

	var buf io.ReadWriter
	if body != nil {
		buf = &bytes.Buffer{}
		enc := json.NewEncoder(buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(body); err != nil {
			return nil, fmt.Errorf("agent inference: encode body: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), buf)
	if err != nil {
		return nil, fmt.Errorf("agent inference: build request: %w", err)
	}

	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.accessKey)
	req.Header.Set("User-Agent", c.userAgent)
	return req, nil
}

// do executes a non-streaming request and decodes a 2xx body into v.
func (c *AgentInferenceClient) do(ctx context.Context, req *http.Request, v interface{}) (*Response, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		return nil, err
	}
	defer resp.Body.Close()

	response := newResponse(resp)
	if c.onRequestCompleted != nil {
		c.onRequestCompleted(req, resp)
	}

	if err := CheckResponse(resp); err != nil {
		return response, err
	}

	if v != nil {
		if w, ok := v.(io.Writer); ok {
			_, err = io.Copy(w, resp.Body)
		} else {
			err = json.NewDecoder(resp.Body).Decode(v)
			if errors.Is(err, io.EOF) {
				err = nil
			}
		}
	}
	return response, err
}

// doStream executes a streaming request and wraps the open body in an *InferenceStream.
func (c *AgentInferenceClient) doStream(ctx context.Context, req *http.Request) (*InferenceStream, *Response, error) {
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		default:
		}
		return nil, nil, err
	}

	response := newResponse(resp)
	if c.onRequestCompleted != nil {
		c.onRequestCompleted(req, resp)
	}

	if err := CheckResponse(resp); err != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return nil, response, err
	}

	return &InferenceStream{
		SSEReader: NewSSEReader(resp.Body),
		body:      resp.Body,
	}, response, nil
}
