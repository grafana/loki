package godo

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	defaultServerlessInferenceBaseURL = "https://inference.do-ai.run/"

	serverlessInferenceAsyncInvokePath   = "v1/async-invoke"
	serverlessInferenceAudioSpeechPath   = "v1/audio/speech"
	serverlessInferenceChatCompletions   = "v1/chat/completions"
	serverlessInferenceEmbeddingsPath    = "v1/embeddings"
	serverlessInferenceImagesGenerations = "v1/images/generations"
	serverlessInferenceMessagesPath      = "v1/messages"
	serverlessInferenceModelsPath        = "v1/models"
	serverlessInferenceResponsesPath     = "v1/responses"
	serverlessInferenceVideosPath        = "v1/videos"
)

// Serverless Inference resources at https://inference.do-ai.run are exposed
// as top-level fields on godo.Client (Models, Chat, Embeddings, ImageGenerations,
// Messages, Responses, AsyncInvocations, Audio, Video)
// See https://docs.digitalocean.com/reference/api/reference/serverless-inference/.

func newInferenceTransport(client *Client, baseURL *url.URL) *inferenceTransport {
	return &inferenceTransport{client: client, baseURL: baseURL}
}

// inferenceTransport is the shared request layer embedded by every inference service.
type inferenceTransport struct {
	client  *Client
	baseURL *url.URL
}

func (t *inferenceTransport) newRequest(ctx context.Context, method, path string, body interface{}) (*http.Request, error) {
	rel, err := url.Parse(path)
	if err != nil {
		return nil, err
	}
	u := t.baseURL.ResolveReference(rel)
	return t.client.NewRequest(ctx, method, u.String(), body)
}

// stream issues an SSE POST and wraps the live body in an InferenceStream; caller must Close.
func (t *inferenceTransport) stream(ctx context.Context, path string, body interface{}) (*InferenceStream, *Response, error) {
	req, err := t.newRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")

	resp, err := t.client.DoStream(ctx, req)
	if err != nil {
		return nil, resp, err
	}
	return &InferenceStream{
		SSEReader: NewSSEReader(resp.Body),
		body:      resp.Body,
	}, resp, nil
}

// InferenceTag is a free-form key/value tag attached to an inference request.
type InferenceTag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// InferenceError is the error envelope returned by the /v1/messages endpoint
// for client-side validation failures (HTTP 400).
type InferenceError struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// InferenceStream is the live SSE stream returned by every *Streaming method; caller must Close.
type InferenceStream struct {
	*SSEReader
	body io.ReadCloser
}

// Close releases the underlying HTTP response body.
func (s *InferenceStream) Close() error {
	if s.body == nil {
		return nil
	}
	return s.body.Close()
}

// =====================================================================
// /v1/async-invoke
// =====================================================================

// AsyncInvocationService exposes POST /v1/async-invoke for async fal model invocations.
type AsyncInvocationService struct {
	*inferenceTransport
}

// AsyncInvocationNewParams is the request body for POST /v1/async-invoke.
// Input carries model-specific parameters (prompt, text, seconds_total, ...).
type AsyncInvocationNewParams struct {
	ModelID string                 `json:"model_id"`
	Input   map[string]interface{} `json:"input"`
	Tags    []InferenceTag         `json:"tags,omitempty"`
}

// AsyncInvocation is the response body of POST /v1/async-invoke. Status is
// one of QUEUED, IN_PROGRESS, COMPLETED, or FAILED.
type AsyncInvocation struct {
	RequestID   string                 `json:"request_id"`
	ModelID     string                 `json:"model_id"`
	Status      string                 `json:"status"`
	CreatedAt   string                 `json:"created_at"`
	StartedAt   *string                `json:"started_at,omitempty"`
	CompletedAt *string                `json:"completed_at,omitempty"`
	Output      map[string]interface{} `json:"output,omitempty"`
	Error       *string                `json:"error,omitempty"`
}

// New starts an asynchronous fal model invocation.
func (s *AsyncInvocationService) New(ctx context.Context, body *AsyncInvocationNewParams) (*AsyncInvocation, *Response, error) {
	if body == nil {
		return nil, nil, errors.New("serverless inference: body is required")
	}
	req, err := s.newRequest(ctx, http.MethodPost, serverlessInferenceAsyncInvokePath, body)
	if err != nil {
		return nil, nil, err
	}
	root := new(AsyncInvocation)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// Get fetches the current status of an async invocation; Output/Error are populated only on terminal states.
func (s *AsyncInvocationService) Get(ctx context.Context, requestID string) (*AsyncInvocation, *Response, error) {
	path := fmt.Sprintf("%s/%s", serverlessInferenceAsyncInvokePath, requestID)
	req, err := s.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(AsyncInvocation)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// =====================================================================
// /v1/videos (+ /v1/videos/{id}/content)
// =====================================================================

// VideoService hosts the Generations sub-service for async video generation.
type VideoService struct {
	Generations *VideoGenerationService
}

// VideoGenerationService exposes async video create/poll and content download
// against /v1/videos.
type VideoGenerationService struct {
	*inferenceTransport
}

// VideoGenerateParams is the request body for POST /v1/videos.
type VideoGenerateParams struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Size   string `json:"size,omitempty"`
	FPS    *int   `json:"fps,omitempty"`
}

// VideoGenerationOutput holds a completed video download location when present.
type VideoGenerationOutput struct {
	URL string `json:"url,omitempty"`
}

// VideoGeneration is returned by video create and poll endpoints.
type VideoGeneration struct {
	ID         string                 `json:"id"`
	Object     string                 `json:"object,omitempty"`
	CreatedAt  int64                  `json:"created_at,omitempty"`
	Model      string                 `json:"model,omitempty"`
	Status     string                 `json:"status"`
	Output     *VideoGenerationOutput `json:"output,omitempty"`
	Error      json.RawMessage        `json:"error,omitempty"`
	XRequestID *string                `json:"x_request_id,omitempty"`
}

// Create starts an asynchronous video generation job (POST /v1/videos).
func (s *VideoGenerationService) Create(ctx context.Context, body *VideoGenerateParams) (*VideoGeneration, *Response, error) {
	if body == nil {
		return nil, nil, errors.New("serverless inference: body is required")
	}
	req, err := s.newRequest(ctx, http.MethodPost, serverlessInferenceVideosPath, body)
	if err != nil {
		return nil, nil, err
	}
	root := new(VideoGeneration)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// Get fetches the current status of a video generation job (GET /v1/videos/{id}).
func (s *VideoGenerationService) Get(ctx context.Context, id string) (*VideoGeneration, *Response, error) {
	if id == "" {
		return nil, nil, errors.New("serverless inference: id is required")
	}
	path := fmt.Sprintf("%s/%s", serverlessInferenceVideosPath, id)
	req, err := s.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(VideoGeneration)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// Content downloads the generated MP4 bytes for a completed video.
func (s *VideoGenerationService) Content(ctx context.Context, id string) ([]byte, *Response, error) {
	if id == "" {
		return nil, nil, errors.New("serverless inference: id is required")
	}
	path := fmt.Sprintf("%s/%s/content", serverlessInferenceVideosPath, id)
	req, err := s.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	var buf bytes.Buffer
	resp, err := s.client.Do(ctx, req, &buf)
	if err != nil {
		return nil, resp, err
	}
	return buf.Bytes(), resp, nil
}

// =====================================================================
// /v1/chat/completions
// =====================================================================

// ChatService hosts the Completions sub-service to mirror openai-go's namespace.
type ChatService struct {
	Completions *ChatCompletionService
}

// ChatCompletionService exposes POST /v1/chat/completions.
type ChatCompletionService struct {
	*inferenceTransport
}

// ChatCompletionMessage is a single message in the chat conversation.
type ChatCompletionMessage struct {
	Role             string                        `json:"role"`
	Content          *ChatCompletionMessageContent `json:"content,omitempty"`
	ReasoningContent *string                       `json:"reasoning_content,omitempty"`
	Refusal          *string                       `json:"refusal,omitempty"`
	ToolCallID       string                        `json:"tool_call_id,omitempty"`
	ToolCalls        []ChatCompletionToolCall      `json:"tool_calls,omitempty"`
}

// ChatCompletionMessageContent is message content: a string or multimodal parts.
// Use StringContent or ContentParts (or the UserMessage* helpers) to construct it.
type ChatCompletionMessageContent struct {
	text  *string
	parts []ChatCompletionContentPart
}

// StringContent builds string message content.
func StringContent(content string) *ChatCompletionMessageContent {
	return &ChatCompletionMessageContent{text: &content}
}

// ContentParts builds multimodal message content from text and/or image parts.
func ContentParts(parts ...ChatCompletionContentPart) *ChatCompletionMessageContent {
	return &ChatCompletionMessageContent{parts: parts}
}

// Text returns the string content when the value is a string, or concatenates
// text parts. Returns "" when content is nil or has no text.
func (c *ChatCompletionMessageContent) Text() string {
	if c == nil {
		return ""
	}
	if c.text != nil {
		return *c.text
	}
	var b bytes.Buffer
	for _, part := range c.parts {
		if part.Type == "text" {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

// Parts returns multimodal content parts when present.
func (c *ChatCompletionMessageContent) Parts() []ChatCompletionContentPart {
	if c == nil {
		return nil
	}
	return c.parts
}

// MarshalJSON encodes content as a JSON string or as a content-part array.
func (c ChatCompletionMessageContent) MarshalJSON() ([]byte, error) {
	if c.parts != nil {
		return json.Marshal(c.parts)
	}
	if c.text != nil {
		return json.Marshal(*c.text)
	}
	return []byte("null"), nil
}

// UnmarshalJSON accepts a JSON string or an array of content parts.
func (c *ChatCompletionMessageContent) UnmarshalJSON(data []byte) error {
	*c = ChatCompletionMessageContent{}
	if bytes.Equal(data, []byte("null")) {
		return nil
	}
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		c.text = &s
		return nil
	}
	var parts []ChatCompletionContentPart
	if err := json.Unmarshal(data, &parts); err != nil {
		return err
	}
	c.parts = parts
	return nil
}

// ChatCompletionContentPart is one entry in multimodal chat message content.
type ChatCompletionContentPart struct {
	Type     string                      `json:"type"`
	Text     string                      `json:"text,omitempty"`
	ImageURL *ChatCompletionContentImage `json:"image_url,omitempty"`
}

// ChatCompletionContentImage holds an image URL or data URI for VLM inputs.
type ChatCompletionContentImage struct {
	URL string `json:"url"`
}

// TextPart builds a text content part.
func TextPart(text string) ChatCompletionContentPart {
	return ChatCompletionContentPart{Type: "text", Text: text}
}

// ImageURLPart builds an image_url content part from an HTTPS URL or data URI.
func ImageURLPart(url string) ChatCompletionContentPart {
	return ChatCompletionContentPart{Type: "image_url", ImageURL: &ChatCompletionContentImage{URL: url}}
}

// UserMessage builds a ChatCompletionMessage with role "user".
func UserMessage(content string) ChatCompletionMessage {
	return ChatCompletionMessage{Role: "user", Content: StringContent(content)}
}

// UserMessageParts builds a multimodal user message for vision-language models.
func UserMessageParts(parts ...ChatCompletionContentPart) ChatCompletionMessage {
	return ChatCompletionMessage{Role: "user", Content: ContentParts(parts...)}
}

// SystemMessage builds a ChatCompletionMessage with role "system".
func SystemMessage(content string) ChatCompletionMessage {
	return ChatCompletionMessage{Role: "system", Content: StringContent(content)}
}

// AssistantMessage builds a ChatCompletionMessage with role "assistant".
func AssistantMessage(content string) ChatCompletionMessage {
	return ChatCompletionMessage{Role: "assistant", Content: StringContent(content)}
}

// ChatCompletionToolCall is a tool invocation produced by the model.
type ChatCompletionToolCall struct {
	ID       string                     `json:"id"`
	Type     string                     `json:"type"`
	Function ChatCompletionToolCallFunc `json:"function"`
}

// ChatCompletionToolCallFunc holds the function name and JSON-encoded
// arguments for a tool call.
type ChatCompletionToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatCompletionTool is a tool definition exposed to the model. Type selects
// the tool kind: "function" for client-side function calling, or a DigitalOcean
// server-side tool such as "web_search", "web_fetch", "mcp",
// "knowledge_base_retrieval", "model_synthesis", or "tool_search".
type ChatCompletionTool struct {
	Type     string                      `json:"type"`
	Function *ChatCompletionToolFunction `json:"function,omitempty"`

	// DeferLoading marks function or MCP tools for on-demand loading with tool_search.
	DeferLoading *bool `json:"defer_loading,omitempty"`

	// web_search / web_fetch
	MaxUses    *int `json:"max_uses,omitempty"`
	MaxResults *int `json:"max_results,omitempty"`

	// mcp
	ServerLabel   string   `json:"server_label,omitempty"`
	ServerURL     string   `json:"server_url,omitempty"`
	Authorization string   `json:"authorization,omitempty"`
	AllowedTools  []string `json:"allowed_tools,omitempty"`

	// knowledge_base_retrieval
	KnowledgeBaseID string `json:"knowledge_base_id,omitempty"`

	// model_synthesis
	Preset          string   `json:"preset,omitempty"`
	Model           string   `json:"model,omitempty"`
	AnalysisModels  []string `json:"analysis_models,omitempty"`
	ReasoningEffort string   `json:"reasoning_effort,omitempty"`
	MaxToolCalls    *int     `json:"max_tool_calls,omitempty"`
	MaxTokens       *int     `json:"max_tokens,omitempty"`
	AllowedDomains  []string `json:"allowed_domains,omitempty"`
	BlockedDomains  []string `json:"blocked_domains,omitempty"`
}

// ChatCompletionToolFunction describes a callable function tool.
type ChatCompletionToolFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

// ChatCompletionStreamOptions toggles streaming-only response options.
type ChatCompletionStreamOptions struct {
	IncludeUsage *bool `json:"include_usage,omitempty"`
}

// ChatCompletionNewParams is the request body for POST /v1/chat/completions.
// Stop and ToolChoice are json.RawMessage to accept either a string or a structured value.
type ChatCompletionNewParams struct {
	Model               string                       `json:"model"`
	Messages            []ChatCompletionMessage      `json:"messages"`
	FrequencyPenalty    *float64                     `json:"frequency_penalty,omitempty"`
	LogitBias           map[string]int               `json:"logit_bias,omitempty"`
	Logprobs            *bool                        `json:"logprobs,omitempty"`
	TopLogprobs         *int                         `json:"top_logprobs,omitempty"`
	MaxTokens           *int                         `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int                         `json:"max_completion_tokens,omitempty"`
	Metadata            map[string]string            `json:"metadata,omitempty"`
	N                   *int                         `json:"n,omitempty"`
	PresencePenalty     *float64                     `json:"presence_penalty,omitempty"`
	ReasoningEffort     *string                      `json:"reasoning_effort,omitempty"`
	Seed                *int                         `json:"seed,omitempty"`
	Stop                json.RawMessage              `json:"stop,omitempty"`
	Stream              *bool                        `json:"stream,omitempty"`
	StreamOptions       *ChatCompletionStreamOptions `json:"stream_options,omitempty"`
	Temperature         *float64                     `json:"temperature,omitempty"`
	ToolChoice          json.RawMessage              `json:"tool_choice,omitempty"`
	Tools               []ChatCompletionTool         `json:"tools,omitempty"`
	TopP                *float64                     `json:"top_p,omitempty"`
	User                string                       `json:"user,omitempty"`
}

// ChatCompletionLogprob is a single token's log-probability information.
type ChatCompletionLogprob struct {
	Token       string                  `json:"token"`
	Logprob     float64                 `json:"logprob"`
	Bytes       []int                   `json:"bytes"`
	TopLogprobs []ChatCompletionLogprob `json:"top_logprobs"`
}

// ChatCompletionChoiceLogprobs is the logprobs payload on a chat choice.
type ChatCompletionChoiceLogprobs struct {
	Content []ChatCompletionLogprob `json:"content"`
	Refusal []ChatCompletionLogprob `json:"refusal"`
}

// ChatCompletionChoice is a single choice in a chat completion response.
type ChatCompletionChoice struct {
	Index        int                           `json:"index"`
	FinishReason string                        `json:"finish_reason"`
	Message      ChatCompletionMessage         `json:"message"`
	Logprobs     *ChatCompletionChoiceLogprobs `json:"logprobs,omitempty"`
}

// ChatCompletionCacheCreation breaks prompt cache writes down by TTL.
type ChatCompletionCacheCreation struct {
	Ephemeral1hInputTokens int `json:"ephemeral_1h_input_tokens"`
	Ephemeral5mInputTokens int `json:"ephemeral_5m_input_tokens"`
}

// ChatCompletionUsage holds token accounting for a chat completion.
type ChatCompletionUsage struct {
	PromptTokens            int                          `json:"prompt_tokens"`
	CompletionTokens        int                          `json:"completion_tokens"`
	TotalTokens             int                          `json:"total_tokens"`
	CacheCreatedInputTokens int                          `json:"cache_created_input_tokens,omitempty"`
	CacheReadInputTokens    int                          `json:"cache_read_input_tokens,omitempty"`
	CacheCreation           *ChatCompletionCacheCreation `json:"cache_creation,omitempty"`
}

// ChatCompletion is returned by POST /v1/chat/completions when stream is false.
type ChatCompletion struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   *ChatCompletionUsage   `json:"usage,omitempty"`
}

// New creates a non-streaming chat completion.
func (s *ChatCompletionService) New(ctx context.Context, body *ChatCompletionNewParams) (*ChatCompletion, *Response, error) {
	if body == nil {
		return nil, nil, errors.New("serverless inference: body is required")
	}
	req, err := s.newRequest(ctx, http.MethodPost, serverlessInferenceChatCompletions, body)
	if err != nil {
		return nil, nil, err
	}
	root := new(ChatCompletion)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// ChatCompletionChunk is one streamed event from POST /v1/chat/completions when stream=true.
type ChatCompletionChunk struct {
	ID                string                      `json:"id"`
	Object            string                      `json:"object"`
	Created           int64                       `json:"created"`
	Model             string                      `json:"model"`
	SystemFingerprint string                      `json:"system_fingerprint,omitempty"`
	Choices           []ChatCompletionChunkChoice `json:"choices"`
	Usage             *ChatCompletionUsage        `json:"usage,omitempty"`
}

// ChatCompletionChunkChoice is a single choice within a streamed chunk.
type ChatCompletionChunkChoice struct {
	Index        int                            `json:"index"`
	Delta        ChatCompletionChunkChoiceDelta `json:"delta"`
	Logprobs     *ChatCompletionChoiceLogprobs  `json:"logprobs,omitempty"`
	FinishReason *string                        `json:"finish_reason"`
}

// ChatCompletionChunkChoiceDelta holds the incremental fields delivered in a streamed chunk.
type ChatCompletionChunkChoiceDelta struct {
	Role             string                   `json:"role,omitempty"`
	Content          string                   `json:"content,omitempty"`
	ReasoningContent string                   `json:"reasoning_content,omitempty"`
	Refusal          string                   `json:"refusal,omitempty"`
	ToolCalls        []ChatCompletionToolCall `json:"tool_calls,omitempty"`
}

// ChatCompletionStream is a typed iterator over a /v1/chat/completions SSE stream.
type ChatCompletionStream struct {
	raw     *InferenceStream
	current ChatCompletionChunk
	err     error
	done    bool
}

// Next advances to the next chunk; returns false on EOF, "[DONE]", or error.
func (s *ChatCompletionStream) Next() bool {
	if s.done || s.err != nil {
		return false
	}
	ev, err := s.raw.Next()
	if errors.Is(err, io.EOF) {
		s.done = true
		return false
	}
	if err != nil {
		s.err = err
		return false
	}
	if bytes.Equal(ev.Data, []byte("[DONE]")) {
		s.done = true
		return false
	}
	var chunk ChatCompletionChunk
	if err := json.Unmarshal(ev.Data, &chunk); err != nil {
		s.err = err
		return false
	}
	s.current = chunk
	return true
}

// Current returns the most recent chunk produced by Next.
func (s *ChatCompletionStream) Current() ChatCompletionChunk { return s.current }

// Err returns any non-EOF error encountered during iteration.
func (s *ChatCompletionStream) Err() error { return s.err }

// Close releases the underlying HTTP response body. Always call Close.
func (s *ChatCompletionStream) Close() error { return s.raw.Close() }

// NewStreaming opens an SSE stream of chat completion chunks; body.Stream is forced to true.
// Callers MUST Close the returned stream.
func (s *ChatCompletionService) NewStreaming(ctx context.Context, body *ChatCompletionNewParams) (*ChatCompletionStream, *Response, error) {
	if body == nil {
		return nil, nil, errors.New("serverless inference: body is required")
	}
	body.Stream = PtrTo(true)
	raw, resp, err := s.stream(ctx, serverlessInferenceChatCompletions, body)
	if err != nil {
		return nil, resp, err
	}
	return &ChatCompletionStream{raw: raw}, resp, nil
}

// =====================================================================
// /v1/embeddings
// =====================================================================

// EmbeddingService exposes POST /v1/embeddings.
type EmbeddingService struct {
	*inferenceTransport
}

// EmbeddingNewParams is the request body for POST /v1/embeddings; Input is a string or []string.
type EmbeddingNewParams struct {
	Model          string      `json:"model"`
	Input          interface{} `json:"input"`
	EncodingFormat string      `json:"encoding_format,omitempty"`
	User           string      `json:"user,omitempty"`
}

// Embedding is a single embedding entry; Embedding is raw JSON because the API
// returns []float32 or a base64 string depending on encoding_format.
type Embedding struct {
	Object    string          `json:"object"`
	Index     int             `json:"index"`
	Embedding json.RawMessage `json:"embedding"`
}

// EmbeddingsUsage holds prompt/total token counts for an embeddings call.
type EmbeddingsUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// CreateEmbeddingResponse is returned by POST /v1/embeddings.
type CreateEmbeddingResponse struct {
	Object string          `json:"object"`
	Model  string          `json:"model"`
	Data   []Embedding     `json:"data"`
	Usage  EmbeddingsUsage `json:"usage"`
}

// New creates one or more embedding vectors. There is no streaming variant.
func (s *EmbeddingService) New(ctx context.Context, body *EmbeddingNewParams) (*CreateEmbeddingResponse, *Response, error) {
	if body == nil {
		return nil, nil, errors.New("serverless inference: body is required")
	}
	req, err := s.newRequest(ctx, http.MethodPost, serverlessInferenceEmbeddingsPath, body)
	if err != nil {
		return nil, nil, err
	}
	root := new(CreateEmbeddingResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// =====================================================================
// /v1/images/generations
// =====================================================================

// ImageGenerationService exposes POST /v1/images/generations
type ImageGenerationService struct {
	*inferenceTransport
}

// ImageGenerateParams is the request body for POST /v1/images/generations.
type ImageGenerateParams struct {
	Model             string  `json:"model"`
	Prompt            string  `json:"prompt"`
	N                 int     `json:"n"`
	Background        *string `json:"background,omitempty"`
	Moderation        *string `json:"moderation,omitempty"`
	OutputCompression *int    `json:"output_compression,omitempty"`
	OutputFormat      *string `json:"output_format,omitempty"`
	PartialImages     *int    `json:"partial_images,omitempty"`
	Quality           *string `json:"quality,omitempty"`
	Size              *string `json:"size,omitempty"`
	Stream            *bool   `json:"stream,omitempty"`
	User              *string `json:"user,omitempty"`
}

// GeneratedImage is a single image entry returned by an image generation call.
type GeneratedImage struct {
	B64JSON       string `json:"b64_json"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

// ImageGenerationUsageDetails breaks down image-generation input tokens.
type ImageGenerationUsageDetails struct {
	ImageTokens int `json:"image_tokens"`
	TextTokens  int `json:"text_tokens"`
}

// ImageGenerationUsage is the usage block for an image-generation response.
type ImageGenerationUsage struct {
	InputTokens        int                          `json:"input_tokens"`
	OutputTokens       int                          `json:"output_tokens"`
	TotalTokens        int                          `json:"total_tokens"`
	InputTokensDetails *ImageGenerationUsageDetails `json:"input_tokens_details,omitempty"`
}

// ImagesResponse is returned by POST /v1/images/generations when stream is false.
type ImagesResponse struct {
	Created      int64                 `json:"created"`
	Data         []GeneratedImage      `json:"data"`
	Background   string                `json:"background,omitempty"`
	OutputFormat string                `json:"output_format,omitempty"`
	Quality      string                `json:"quality,omitempty"`
	Size         string                `json:"size,omitempty"`
	Usage        *ImageGenerationUsage `json:"usage,omitempty"`
}

// Generate creates one or more images from a text prompt.
func (s *ImageGenerationService) Generate(ctx context.Context, body *ImageGenerateParams) (*ImagesResponse, *Response, error) {
	if body == nil {
		return nil, nil, errors.New("serverless inference: body is required")
	}
	req, err := s.newRequest(ctx, http.MethodPost, serverlessInferenceImagesGenerations, body)
	if err != nil {
		return nil, nil, err
	}
	root := new(ImagesResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// ImageGenerationStreamEvent is one streamed event from /v1/images/generations
// (Type is e.g. "image_generation.partial_image" or "image_generation.completed").
type ImageGenerationStreamEvent struct {
	Type              string                `json:"type"`
	CreatedAt         int64                 `json:"created_at,omitempty"`
	Size              string                `json:"size,omitempty"`
	Quality           string                `json:"quality,omitempty"`
	Background        string                `json:"background,omitempty"`
	OutputFormat      string                `json:"output_format,omitempty"`
	PartialImageIndex int                   `json:"partial_image_index,omitempty"`
	B64JSON           string                `json:"b64_json,omitempty"`
	Usage             *ImageGenerationUsage `json:"usage,omitempty"`
}

// ImageGenerationStream is a typed iterator over a /v1/images/generations SSE stream.
type ImageGenerationStream struct {
	raw     *InferenceStream
	current ImageGenerationStreamEvent
	err     error
	done    bool
}

// Next advances the stream to the next event. Returns false on EOF or error.
func (s *ImageGenerationStream) Next() bool {
	if s.done || s.err != nil {
		return false
	}
	ev, err := s.raw.Next()
	if errors.Is(err, io.EOF) {
		s.done = true
		return false
	}
	if err != nil {
		s.err = err
		return false
	}
	if bytes.Equal(ev.Data, []byte("[DONE]")) {
		s.done = true
		return false
	}
	var event ImageGenerationStreamEvent
	if err := json.Unmarshal(ev.Data, &event); err != nil {
		s.err = err
		return false
	}
	s.current = event
	return true
}

// Current returns the most recent event produced by Next.
func (s *ImageGenerationStream) Current() ImageGenerationStreamEvent { return s.current }

// Err returns any non-EOF error encountered during iteration.
func (s *ImageGenerationStream) Err() error { return s.err }

// Close releases the underlying HTTP response body. Always call Close.
func (s *ImageGenerationStream) Close() error { return s.raw.Close() }

// GenerateStreaming opens an SSE stream of partial-image then completed events.
// body.Stream is forced to true; callers MUST Close the returned stream.
func (s *ImageGenerationService) GenerateStreaming(ctx context.Context, body *ImageGenerateParams) (*ImageGenerationStream, *Response, error) {
	if body == nil {
		return nil, nil, errors.New("serverless inference: body is required")
	}
	body.Stream = PtrTo(true)
	raw, resp, err := s.stream(ctx, serverlessInferenceImagesGenerations, body)
	if err != nil {
		return nil, resp, err
	}
	return &ImageGenerationStream{raw: raw}, resp, nil
}

// =====================================================================
// /v1/audio/speech
// =====================================================================

// AudioService hosts the Speech sub-service for text-to-speech.
type AudioService struct {
	Speech *SpeechService
}

// SpeechService exposes POST /v1/audio/speech.
type SpeechService struct {
	*inferenceTransport
}

// SpeechNewParams is the request body for POST /v1/audio/speech.
type SpeechNewParams struct {
	Model          string `json:"model"`
	Input          string `json:"input"`
	Voice          string `json:"voice,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
	Instructions   string `json:"instructions,omitempty"`
}

// SpeechAudio is a single audio entry in a JSON speech envelope.
type SpeechAudio struct {
	B64JSON string `json:"b64_json"`
}

// SpeechResponse is returned by POST /v1/audio/speech.
// The live endpoint returns raw audio bytes (Content-Type audio/mpeg, audio/wav,
// etc.). Some docs also describe a JSON data[].b64_json envelope; both are accepted.
type SpeechResponse struct {
	ContentType string
	Audio       []byte

	Created int64         `json:"created,omitempty"`
	Data    []SpeechAudio `json:"data,omitempty"`
}

// Bytes returns the decoded audio payload from a binary or JSON speech response.
func (r *SpeechResponse) Bytes() ([]byte, error) {
	if r == nil {
		return nil, errors.New("serverless inference: nil speech response")
	}
	if len(r.Audio) > 0 {
		return r.Audio, nil
	}
	if len(r.Data) == 0 || r.Data[0].B64JSON == "" {
		return nil, errors.New("serverless inference: no audio data returned")
	}
	return base64.StdEncoding.DecodeString(r.Data[0].B64JSON)
}

// New creates speech audio from text input.
func (s *SpeechService) New(ctx context.Context, body *SpeechNewParams) (*SpeechResponse, *Response, error) {
	if body == nil {
		return nil, nil, errors.New("serverless inference: body is required")
	}
	req, err := s.newRequest(ctx, http.MethodPost, serverlessInferenceAudioSpeechPath, body)
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.client.DoStream(ctx, req)
	if err != nil {
		return nil, resp, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp, err
	}

	root := &SpeechResponse{ContentType: resp.Header.Get("Content-Type")}
	ct := strings.ToLower(root.ContentType)
	if strings.Contains(ct, "application/json") || (len(raw) > 0 && raw[0] == '{') {
		if err := json.Unmarshal(raw, root); err != nil {
			return nil, resp, err
		}
		if audio, err := root.Bytes(); err == nil {
			root.Audio = audio
		}
		return root, resp, nil
	}

	root.Audio = raw
	return root, resp, nil
}

// =====================================================================
// /v1/messages
// =====================================================================

// MessageService exposes POST /v1/messages (Anthropic-style conversational messages).
type MessageService struct {
	*inferenceTransport
}

// MessageParam is a single turn; Content is a string or an array of content blocks.
type MessageParam struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// MessageNewParamsMetadata is the optional metadata block on a /v1/messages request.
type MessageNewParamsMetadata struct {
	UserID string `json:"user_id,omitempty"`
}

// MessageNewParamsSystemBlock is a single block in a structured system prompt.
type MessageNewParamsSystemBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// MessageNewParamsThinking configures extended-thinking behavior on supported models.
type MessageNewParamsThinking struct {
	Type string `json:"type"`
}

// MessageNewParamsToolChoice selects how the model uses tools.
type MessageNewParamsToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

// MessageNewParamsTool is a tool definition for the /v1/messages endpoint.
type MessageNewParamsTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// MessageNewParams is the request body for POST /v1/messages.
// System and ToolChoice are json.RawMessage to accept either a string or a structured value.
type MessageNewParams struct {
	Model           string                    `json:"model"`
	MaxTokens       int                       `json:"max_tokens"`
	Messages        []MessageParam            `json:"messages"`
	Metadata        *MessageNewParamsMetadata `json:"metadata,omitempty"`
	ReasoningEffort *string                   `json:"reasoning_effort,omitempty"`
	Speed           *string                   `json:"speed,omitempty"`
	StopSequences   []string                  `json:"stop_sequences,omitempty"`
	Stream          *bool                     `json:"stream,omitempty"`
	System          json.RawMessage           `json:"system,omitempty"`
	Temperature     *float64                  `json:"temperature,omitempty"`
	Thinking        *MessageNewParamsThinking `json:"thinking,omitempty"`
	ToolChoice      json.RawMessage           `json:"tool_choice,omitempty"`
	Tools           []MessageNewParamsTool    `json:"tools,omitempty"`
	TopK            *int                      `json:"top_k,omitempty"`
	TopP            *float64                  `json:"top_p,omitempty"`
}

// MessageContentBlock is one assistant output block ("text" or "tool_use").
type MessageContentBlock struct {
	Type  string                 `json:"type"`
	Text  string                 `json:"text,omitempty"`
	ID    string                 `json:"id,omitempty"`
	Name  string                 `json:"name,omitempty"`
	Input map[string]interface{} `json:"input,omitempty"`
}

// MessageUsage holds token usage for a non-streaming /v1/messages response.
type MessageUsage struct {
	InputTokens              int     `json:"input_tokens"`
	OutputTokens             int     `json:"output_tokens"`
	CacheCreationInputTokens int     `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int     `json:"cache_read_input_tokens,omitempty"`
	Speed                    *string `json:"speed,omitempty"`
}

// Message is returned by POST /v1/messages when stream is false.
type Message struct {
	ID           string                `json:"id"`
	Type         string                `json:"type"`
	Role         string                `json:"role"`
	Model        string                `json:"model"`
	Content      []MessageContentBlock `json:"content"`
	StopReason   *string               `json:"stop_reason"`
	StopSequence *string               `json:"stop_sequence,omitempty"`
	Usage        MessageUsage          `json:"usage"`
}

// New creates a non-streaming message.
func (s *MessageService) New(ctx context.Context, body *MessageNewParams) (*Message, *Response, error) {
	if body == nil {
		return nil, nil, errors.New("serverless inference: body is required")
	}
	req, err := s.newRequest(ctx, http.MethodPost, serverlessInferenceMessagesPath, body)
	if err != nil {
		return nil, nil, err
	}
	root := new(Message)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// MessageStreamEventDelta carries the incremental fields in a /v1/messages SSE event.
type MessageStreamEventDelta struct {
	Type         string  `json:"type,omitempty"`
	Text         string  `json:"text,omitempty"`
	PartialJSON  string  `json:"partial_json,omitempty"`
	StopReason   *string `json:"stop_reason,omitempty"`
	StopSequence *string `json:"stop_sequence,omitempty"`
}

// MessageStreamEvent is one Anthropic-style SSE event from /v1/messages
// (message_start, content_block_start, content_block_delta, content_block_stop,
// message_delta, message_stop, ping).
type MessageStreamEvent struct {
	Type         string                  `json:"type"`
	Index        int                     `json:"index,omitempty"`
	Message      *Message                `json:"message,omitempty"`
	ContentBlock *MessageContentBlock    `json:"content_block,omitempty"`
	Delta        MessageStreamEventDelta `json:"delta,omitempty"`
	Usage        *MessageUsage           `json:"usage,omitempty"`
}

// MessageStream is a typed iterator over a /v1/messages SSE stream.
type MessageStream struct {
	raw     *InferenceStream
	current MessageStreamEvent
	err     error
	done    bool
}

// Next advances the stream to the next event. Returns false on EOF or error.
func (s *MessageStream) Next() bool {
	if s.done || s.err != nil {
		return false
	}
	ev, err := s.raw.Next()
	if errors.Is(err, io.EOF) {
		s.done = true
		return false
	}
	if err != nil {
		s.err = err
		return false
	}
	if bytes.Equal(ev.Data, []byte("[DONE]")) {
		s.done = true
		return false
	}
	var event MessageStreamEvent
	if err := json.Unmarshal(ev.Data, &event); err != nil {
		s.err = err
		return false
	}
	s.current = event
	return true
}

// Current returns the most recent event produced by Next.
func (s *MessageStream) Current() MessageStreamEvent { return s.current }

// Err returns any non-EOF error encountered during iteration.
func (s *MessageStream) Err() error { return s.err }

// Close releases the underlying HTTP response body. Always call Close.
func (s *MessageStream) Close() error { return s.raw.Close() }

// NewStreaming opens an SSE stream of /v1/messages events; body.Stream is forced to true.
// Callers MUST Close the returned stream.
func (s *MessageService) NewStreaming(ctx context.Context, body *MessageNewParams) (*MessageStream, *Response, error) {
	if body == nil {
		return nil, nil, errors.New("serverless inference: body is required")
	}
	body.Stream = PtrTo(true)
	raw, resp, err := s.stream(ctx, serverlessInferenceMessagesPath, body)
	if err != nil {
		return nil, resp, err
	}
	return &MessageStream{raw: raw}, resp, nil
}

// =====================================================================
// /v1/models
// =====================================================================

// ModelService exposes GET /v1/models.
type ModelService struct {
	*inferenceTransport
}

// InferenceModel is a single model entry returned by GET /v1/models.
type InferenceModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// ModelList is returned by GET /v1/models.
type ModelList struct {
	Object string           `json:"object"`
	Data   []InferenceModel `json:"data"`
}

// List returns the catalogue of models reachable from the caller's inference key.
func (s *ModelService) List(ctx context.Context) (*ModelList, *Response, error) {
	req, err := s.newRequest(ctx, http.MethodGet, serverlessInferenceModelsPath, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(ModelList)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// =====================================================================
// /v1/responses
// =====================================================================

// ResponseService exposes POST /v1/responses.
type ResponseService struct {
	*inferenceTransport
}

// ResponseInputMessage is an input message for the Responses API.
type ResponseInputMessage struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content"`
}

// ResponseStreamOptions toggles streaming-only response options.
type ResponseStreamOptions struct {
	IncludeUsage *bool `json:"include_usage,omitempty"`
}

// ResponseTool is a tool definition for the Responses API. Type selects the
// tool kind: "function" for client-side function calling, or a DigitalOcean
// server-side tool such as "web_search", "web_fetch", "mcp",
// "knowledge_base_retrieval", "model_synthesis", or "tool_search".
type ResponseTool struct {
	Type        string                 `json:"type"`
	Name        string                 `json:"name,omitempty"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`

	// DeferLoading marks function or MCP tools for on-demand loading with tool_search.
	DeferLoading *bool `json:"defer_loading,omitempty"`

	// web_search / web_fetch
	MaxUses    *int `json:"max_uses,omitempty"`
	MaxResults *int `json:"max_results,omitempty"`

	// mcp
	ServerLabel   string   `json:"server_label,omitempty"`
	ServerURL     string   `json:"server_url,omitempty"`
	Authorization string   `json:"authorization,omitempty"`
	AllowedTools  []string `json:"allowed_tools,omitempty"`

	// knowledge_base_retrieval
	KnowledgeBaseID string `json:"knowledge_base_id,omitempty"`

	// model_synthesis
	Preset          string   `json:"preset,omitempty"`
	Model           string   `json:"model,omitempty"`
	AnalysisModels  []string `json:"analysis_models,omitempty"`
	ReasoningEffort string   `json:"reasoning_effort,omitempty"`
	MaxToolCalls    *int     `json:"max_tool_calls,omitempty"`
	MaxTokens       *int     `json:"max_tokens,omitempty"`
	AllowedDomains  []string `json:"allowed_domains,omitempty"`
	BlockedDomains  []string `json:"blocked_domains,omitempty"`
}

// FunctionTool builds a Responses API function tool.
func FunctionTool(name, description string, parameters map[string]interface{}) ResponseTool {
	return ResponseTool{Type: "function", Name: name, Description: description, Parameters: parameters}
}

// ChatFunctionTool builds a Chat Completions function tool.
func ChatFunctionTool(name, description string, parameters map[string]interface{}) ChatCompletionTool {
	return ChatCompletionTool{
		Type:     "function",
		Function: &ChatCompletionToolFunction{Name: name, Description: description, Parameters: parameters},
	}
}

// WebSearchTool builds a Responses API web_search tool.
func WebSearchTool(maxUses, maxResults *int) ResponseTool {
	return ResponseTool{Type: "web_search", MaxUses: maxUses, MaxResults: maxResults}
}

// ChatWebSearchTool builds a Chat Completions web_search tool.
func ChatWebSearchTool(maxUses, maxResults *int) ChatCompletionTool {
	return ChatCompletionTool{Type: "web_search", MaxUses: maxUses, MaxResults: maxResults}
}

// WebFetchTool builds a Responses API web_fetch tool.
func WebFetchTool(maxUses *int) ResponseTool {
	return ResponseTool{Type: "web_fetch", MaxUses: maxUses}
}

// ChatWebFetchTool builds a Chat Completions web_fetch tool.
func ChatWebFetchTool(maxUses *int) ChatCompletionTool {
	return ChatCompletionTool{Type: "web_fetch", MaxUses: maxUses}
}

// MCPToolParams holds fields for an MCP server-side tool.
type MCPToolParams struct {
	ServerLabel   string
	ServerURL     string
	Authorization string
	AllowedTools  []string
	DeferLoading  *bool
}

// MCPTool builds a Responses API mcp tool.
func MCPTool(p MCPToolParams) ResponseTool {
	return ResponseTool{
		Type:          "mcp",
		ServerLabel:   p.ServerLabel,
		ServerURL:     p.ServerURL,
		Authorization: p.Authorization,
		AllowedTools:  p.AllowedTools,
		DeferLoading:  p.DeferLoading,
	}
}

// ChatMCPTool builds a Chat Completions mcp tool.
func ChatMCPTool(p MCPToolParams) ChatCompletionTool {
	return ChatCompletionTool{
		Type:          "mcp",
		ServerLabel:   p.ServerLabel,
		ServerURL:     p.ServerURL,
		Authorization: p.Authorization,
		AllowedTools:  p.AllowedTools,
		DeferLoading:  p.DeferLoading,
	}
}

// KnowledgeBaseRetrievalTool builds a Responses API knowledge_base_retrieval tool.
func KnowledgeBaseRetrievalTool(knowledgeBaseID string) ResponseTool {
	return ResponseTool{Type: "knowledge_base_retrieval", KnowledgeBaseID: knowledgeBaseID}
}

// ChatKnowledgeBaseRetrievalTool builds a Chat Completions knowledge_base_retrieval tool.
func ChatKnowledgeBaseRetrievalTool(knowledgeBaseID string) ChatCompletionTool {
	return ChatCompletionTool{Type: "knowledge_base_retrieval", KnowledgeBaseID: knowledgeBaseID}
}

// ModelSynthesisToolParams holds fields for a model_synthesis tool.
type ModelSynthesisToolParams struct {
	Preset          string
	Model           string
	AnalysisModels  []string
	ReasoningEffort string
	MaxToolCalls    *int
	MaxTokens       *int
	AllowedDomains  []string
	BlockedDomains  []string
}

// ModelSynthesisTool builds a Responses API model_synthesis tool.
func ModelSynthesisTool(p ModelSynthesisToolParams) ResponseTool {
	return ResponseTool{
		Type:            "model_synthesis",
		Preset:          p.Preset,
		Model:           p.Model,
		AnalysisModels:  p.AnalysisModels,
		ReasoningEffort: p.ReasoningEffort,
		MaxToolCalls:    p.MaxToolCalls,
		MaxTokens:       p.MaxTokens,
		AllowedDomains:  p.AllowedDomains,
		BlockedDomains:  p.BlockedDomains,
	}
}

// ChatModelSynthesisTool builds a Chat Completions model_synthesis tool.
func ChatModelSynthesisTool(p ModelSynthesisToolParams) ChatCompletionTool {
	return ChatCompletionTool{
		Type:            "model_synthesis",
		Preset:          p.Preset,
		Model:           p.Model,
		AnalysisModels:  p.AnalysisModels,
		ReasoningEffort: p.ReasoningEffort,
		MaxToolCalls:    p.MaxToolCalls,
		MaxTokens:       p.MaxTokens,
		AllowedDomains:  p.AllowedDomains,
		BlockedDomains:  p.BlockedDomains,
	}
}

// ToolSearchTool builds a Responses API tool_search tool.
func ToolSearchTool() ResponseTool {
	return ResponseTool{Type: "tool_search"}
}

// ChatToolSearchTool builds a Chat Completions tool_search tool.
func ChatToolSearchTool() ChatCompletionTool {
	return ChatCompletionTool{Type: "tool_search"}
}

// ResponseNewParams is the request body for POST /v1/responses.
// Input is a string, []ResponseInputMessage, or json.RawMessage.
// Stop and ToolChoice are json.RawMessage to accept either a string or a structured value.
type ResponseNewParams struct {
	Model           string                 `json:"model"`
	Input           interface{}            `json:"input"`
	Instructions    *string                `json:"instructions,omitempty"`
	MaxOutputTokens *int                   `json:"max_output_tokens,omitempty"`
	Metadata        map[string]string      `json:"metadata,omitempty"`
	Stop            json.RawMessage        `json:"stop,omitempty"`
	Stream          *bool                  `json:"stream,omitempty"`
	StreamOptions   *ResponseStreamOptions `json:"stream_options,omitempty"`
	Temperature     *float64               `json:"temperature,omitempty"`
	ToolChoice      json.RawMessage        `json:"tool_choice,omitempty"`
	Tools           []ResponseTool         `json:"tools,omitempty"`
	TopP            *float64               `json:"top_p,omitempty"`
	User            *string                `json:"user,omitempty"`
}

// ResponseOutputContent is a single content part inside a Responses output item.
type ResponseOutputContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ResponseOutputItem is a single output item in a Responses response.
type ResponseOutputItem struct {
	Type      string                  `json:"type"`
	ID        string                  `json:"id,omitempty"`
	Role      *string                 `json:"role,omitempty"`
	Status    *string                 `json:"status,omitempty"`
	Content   []ResponseOutputContent `json:"content,omitempty"`
	CallID    *string                 `json:"call_id,omitempty"`
	Name      *string                 `json:"name,omitempty"`
	Arguments *string                 `json:"arguments,omitempty"`
}

// ResponseTokensDetails breaks input or output tokens into sub-categories.
type ResponseTokensDetails struct {
	CachedTokens     int `json:"cached_tokens,omitempty"`
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"`
	ToolOutputTokens int `json:"tool_output_tokens,omitempty"`
}

// ResponseUsage holds usage statistics for a Responses API call.
type ResponseUsage struct {
	InputTokens         int                   `json:"input_tokens"`
	OutputTokens        int                   `json:"output_tokens"`
	TotalTokens         int                   `json:"total_tokens"`
	InputTokensDetails  ResponseTokensDetails `json:"input_tokens_details"`
	OutputTokensDetails ResponseTokensDetails `json:"output_tokens_details"`
}

// ResponsesResponse is the result of POST /v1/responses (named to avoid colliding with godo.Response).
// ToolChoice is json.RawMessage because the API echoes back a string or a structured object.
type ResponsesResponse struct {
	ID                string               `json:"id"`
	Object            string               `json:"object"`
	Created           int64                `json:"created"`
	Model             string               `json:"model"`
	Output            []ResponseOutputItem `json:"output"`
	MaxOutputTokens   *int                 `json:"max_output_tokens,omitempty"`
	ParallelToolCalls *bool                `json:"parallel_tool_calls,omitempty"`
	Status            *string              `json:"status,omitempty"`
	Temperature       *float64             `json:"temperature,omitempty"`
	ToolChoice        json.RawMessage      `json:"tool_choice,omitempty"`
	Tools             []ResponseTool       `json:"tools,omitempty"`
	TopP              *float64             `json:"top_p,omitempty"`
	User              *string              `json:"user,omitempty"`
	Usage             ResponseUsage        `json:"usage"`
}

// OutputText concatenates every "output_text" content part across the response.
func (r *ResponsesResponse) OutputText() string {
	if r == nil {
		return ""
	}
	var b bytes.Buffer
	for _, item := range r.Output {
		for _, part := range item.Content {
			if part.Type == "output_text" {
				b.WriteString(part.Text)
			}
		}
	}
	return b.String()
}

// New creates a non-streaming Responses API result.
func (s *ResponseService) New(ctx context.Context, body *ResponseNewParams) (*ResponsesResponse, *Response, error) {
	if body == nil {
		return nil, nil, errors.New("serverless inference: body is required")
	}
	req, err := s.newRequest(ctx, http.MethodPost, serverlessInferenceResponsesPath, body)
	if err != nil {
		return nil, nil, err
	}
	root := new(ResponsesResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// ResponseStreamEvent is one SSE event from /v1/responses (response.created,
// response.output_text.delta, response.output_text.done, response.completed, ...).
// Delta is populated on delta events; Text is populated on the matching .done event.
type ResponseStreamEvent struct {
	Type           string                 `json:"type"`
	SequenceNumber int                    `json:"sequence_number,omitempty"`
	OutputIndex    int                    `json:"output_index,omitempty"`
	ContentIndex   int                    `json:"content_index,omitempty"`
	ItemID         string                 `json:"item_id,omitempty"`
	Delta          string                 `json:"delta,omitempty"`
	Text           string                 `json:"text,omitempty"`
	Response       *ResponsesResponse     `json:"response,omitempty"`
	Item           *ResponseOutputItem    `json:"item,omitempty"`
	Part           *ResponseOutputContent `json:"part,omitempty"`
}

// ResponseStream is a typed iterator over a /v1/responses SSE stream.
type ResponseStream struct {
	raw     *InferenceStream
	current ResponseStreamEvent
	err     error
	done    bool
}

// Next advances the stream to the next event. Returns false on EOF or error.
func (s *ResponseStream) Next() bool {
	if s.done || s.err != nil {
		return false
	}
	ev, err := s.raw.Next()
	if errors.Is(err, io.EOF) {
		s.done = true
		return false
	}
	if err != nil {
		s.err = err
		return false
	}
	if bytes.Equal(ev.Data, []byte("[DONE]")) {
		s.done = true
		return false
	}
	var event ResponseStreamEvent
	if err := json.Unmarshal(ev.Data, &event); err != nil {
		s.err = err
		return false
	}
	s.current = event
	return true
}

// Current returns the most recent event produced by Next.
func (s *ResponseStream) Current() ResponseStreamEvent { return s.current }

// Err returns any non-EOF error encountered during iteration.
func (s *ResponseStream) Err() error { return s.err }

// Close releases the underlying HTTP response body. Always call Close.
func (s *ResponseStream) Close() error { return s.raw.Close() }

// NewStreaming opens an SSE stream of /v1/responses events; body.Stream is forced to true.
// Callers MUST Close the returned stream.
func (s *ResponseService) NewStreaming(ctx context.Context, body *ResponseNewParams) (*ResponseStream, *Response, error) {
	if body == nil {
		return nil, nil, errors.New("serverless inference: body is required")
	}
	body.Stream = PtrTo(true)
	raw, resp, err := s.stream(ctx, serverlessInferenceResponsesPath, body)
	if err != nil {
		return nil, resp, err
	}
	return &ResponseStream{raw: raw}, resp, nil
}
