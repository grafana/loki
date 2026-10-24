package godo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ActionGatewayInferenceToolsOptions selects session tools for an inference request.
// By default it selects the three meta-tools.
type ActionGatewayInferenceToolsOptions struct {
	IncludeAll bool
	Names      []string
	Search     *ActionGatewayToolSearchRequest
}

func (session *ActionGatewaySession) inferenceTools(ctx context.Context, opt *ActionGatewayInferenceToolsOptions) ([]ActionGatewayMCPTool, *Response, error) {
	if opt != nil && opt.Search != nil {
		result, response, err := session.Tools.Search(ctx, opt.Search)
		if err != nil {
			return nil, response, err
		}
		var found struct {
			Results []struct {
				Results []ActionGatewayMCPTool `json:"results"`
			} `json:"results"`
		}
		if err = json.Unmarshal(result, &found); err != nil {
			return nil, response, err
		}
		seen := make(map[string]bool)
		var tools []ActionGatewayMCPTool
		for _, group := range found.Results {
			for _, tool := range group.Results {
				if tool.Name != "" && !seen[tool.Name] {
					tools = append(tools, tool)
					seen[tool.Name] = true
				}
			}
		}
		return tools, response, nil
	}
	includeAll := opt != nil && (opt.IncludeAll || len(opt.Names) > 0)
	tools, response, err := session.Tools.List(ctx, includeAll)
	if err != nil {
		return nil, response, err
	}
	if opt == nil || len(opt.Names) == 0 {
		return tools, response, nil
	}
	wanted := make(map[string]bool, len(opt.Names))
	for _, name := range opt.Names {
		wanted[name] = true
	}
	selected := make([]ActionGatewayMCPTool, 0, len(wanted))
	for _, tool := range tools {
		if wanted[tool.Name] {
			selected = append(selected, tool)
			delete(wanted, tool.Name)
		}
	}
	if len(wanted) > 0 {
		return nil, response, fmt.Errorf("action gateway: requested tools not in session catalog: %v", wanted)
	}
	return selected, response, nil
}

func actionGatewayToolSchema(schema json.RawMessage) map[string]interface{} {
	result := make(map[string]interface{})
	if len(schema) > 0 {
		_ = json.Unmarshal(schema, &result)
	}
	for _, key := range []string{"oneOf", "allOf", "anyOf", "enum", "const", "not"} {
		delete(result, key)
	}
	if result["type"] == nil {
		result["type"] = "object"
	}
	if result["type"] == "object" && result["properties"] == nil {
		result["properties"] = map[string]interface{}{}
	}
	return result
}

// ChatTools returns tools ready for ChatCompletionNewParams.Tools.
func (session *ActionGatewaySession) ChatTools(ctx context.Context, opt *ActionGatewayInferenceToolsOptions) ([]ChatCompletionTool, *Response, error) {
	tools, response, err := session.inferenceTools(ctx, opt)
	if err != nil {
		return nil, response, err
	}
	result := make([]ChatCompletionTool, 0, len(tools))
	for _, tool := range tools {
		result = append(result, ChatCompletionTool{Type: "function", Function: &ChatCompletionToolFunction{Name: tool.Name, Description: tool.Description, Parameters: actionGatewayToolSchema(tool.InputSchema)}})
	}
	return result, response, nil
}

// MessageTools returns tools ready for MessageNewParams.Tools.
func (session *ActionGatewaySession) MessageTools(ctx context.Context, opt *ActionGatewayInferenceToolsOptions) ([]MessageNewParamsTool, *Response, error) {
	tools, response, err := session.inferenceTools(ctx, opt)
	if err != nil {
		return nil, response, err
	}
	result := make([]MessageNewParamsTool, 0, len(tools))
	for _, tool := range tools {
		result = append(result, MessageNewParamsTool{Name: tool.Name, Description: tool.Description, InputSchema: actionGatewayToolSchema(tool.InputSchema)})
	}
	return result, response, nil
}

// ResponseTools returns tools ready for ResponseNewParams.Tools.
func (session *ActionGatewaySession) ResponseTools(ctx context.Context, opt *ActionGatewayInferenceToolsOptions) ([]ResponseTool, *Response, error) {
	tools, response, err := session.inferenceTools(ctx, opt)
	if err != nil {
		return nil, response, err
	}
	result := make([]ResponseTool, 0, len(tools))
	for _, tool := range tools {
		result = append(result, ResponseTool{Type: "function", Name: tool.Name, Description: tool.Description, Parameters: actionGatewayToolSchema(tool.InputSchema)})
	}
	return result, response, nil
}

// ActionGatewayToolCall is a normalized model-requested tool invocation.
type ActionGatewayToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

func actionGatewayArguments(arguments json.RawMessage) json.RawMessage {
	if len(arguments) == 0 {
		return json.RawMessage(`{}`)
	}
	return arguments
}

func actionGatewayFailure(err error) json.RawMessage {
	failure := map[string]interface{}{"message": err.Error()}
	payload := map[string]interface{}{"error": failure}
	var toolError *ActionGatewayToolError
	if errors.As(err, &toolError) {
		failure["message"] = toolError.Message
		failure["class"] = toolError.Class
		failure["retriable"] = toolError.Retriable
		failure["recovery_hint"] = toolError.RecoveryHint
		if len(toolError.Meta) > 0 {
			payload["_meta"] = toolError.Meta
		}
	}
	result, _ := json.Marshal(payload)
	return result
}

func actionGatewayNormalizeInvoke(arguments interface{}) (interface{}, error) {
	object, ok := arguments.(map[string]interface{})
	if !ok {
		return arguments, nil
	}
	entries, present := object["tools"]
	if !present {
		return arguments, nil
	}
	tools, ok := entries.([]interface{})
	if !ok {
		tools = []interface{}{entries}
	}
	normalized := make([]map[string]interface{}, 0, len(tools))
	for _, entry := range tools {
		spec, ok := entry.(map[string]interface{})
		if !ok {
			return nil, errors.New("invoke entry must be an object")
		}
		name, _ := spec["tool"].(string)
		if name == "" {
			name, _ = spec["tool_slug"].(string)
		}
		if name == "" {
			name, _ = spec["name"].(string)
		}
		value := spec["arguments"]
		if function, ok := spec["function"].(map[string]interface{}); ok {
			if functionName, ok := function["name"].(string); ok && functionName != "" {
				name = functionName
			}
			if function["arguments"] != nil {
				value = function["arguments"]
			}
		}
		if name == "" {
			return nil, errors.New("invoke entry requires a tool name")
		}
		if value == nil {
			hoisted := make(map[string]interface{})
			for key, field := range spec {
				switch key {
				case "tool", "tool_slug", "name", "function", "type", "id", "arguments":
				default:
					hoisted[key] = field
				}
			}
			value = hoisted
		}
		if text, ok := value.(string); ok {
			if strings.TrimSpace(text) == "" {
				value = map[string]interface{}{}
			} else if err := json.Unmarshal([]byte(text), &value); err != nil {
				return nil, err
			}
		}
		normalized = append(normalized, map[string]interface{}{"tool": name, "arguments": value})
	}
	copy := make(map[string]interface{}, len(object))
	for key, value := range object {
		copy[key] = value
	}
	copy["tools"] = normalized
	return copy, nil
}

// ExecuteToolCalls executes meta-tools and batches concrete tool calls. Individual
// failures are returned as error payloads for the model; transport failures return an error.
func (session *ActionGatewaySession) ExecuteToolCalls(ctx context.Context, calls []ActionGatewayToolCall, rationale string) ([]json.RawMessage, *Response, error) {
	results := make([]json.RawMessage, len(calls))
	var response *Response
	var indices []int
	for index, call := range calls {
		if !json.Valid(actionGatewayArguments(call.Arguments)) {
			results[index] = actionGatewayFailure(errors.New("invalid tool arguments JSON"))
			continue
		}
		switch call.Name {
		case actionGatewaySearchTool, actionGatewayInvokeTool, actionGatewayCodeTool:
			var arguments interface{}
			_ = json.Unmarshal(actionGatewayArguments(call.Arguments), &arguments)
			if call.Name == actionGatewayInvokeTool {
				var normalizeError error
				arguments, normalizeError = actionGatewayNormalizeInvoke(arguments)
				if normalizeError != nil {
					results[index] = actionGatewayFailure(normalizeError)
					continue
				}
			}
			output, current, err := session.Tools.Call(ctx, call.Name, arguments)
			response = current
			if err != nil {
				var toolError *ActionGatewayToolError
				if !errors.As(err, &toolError) {
					return nil, response, err
				}
				output = actionGatewayFailure(err)
			}
			results[index] = output
		default:
			indices = append(indices, index)
		}
	}
	for start := 0; start < len(indices); start += 10 {
		end := start + 10
		if end > len(indices) {
			end = len(indices)
		}
		entries := make([]ActionGatewayInvokeTool, 0, end-start)
		for _, index := range indices[start:end] {
			entries = append(entries, ActionGatewayInvokeTool{Tool: calls[index].Name, Arguments: actionGatewayArguments(calls[index].Arguments)})
		}
		batch, current, err := session.Tools.Invoke(ctx, entries, rationale)
		response = current
		if err != nil {
			return nil, response, err
		}
		for position, index := range indices[start:end] {
			if position >= len(batch.Results) {
				results[index] = actionGatewayFailure(errors.New("no result returned for tool call"))
				continue
			}
			item := batch.Results[position].Result
			if item.Status != "" && item.Status != "succeeded" {
				if item.Error != nil {
					item.Error.Meta = item.Meta
					results[index] = actionGatewayFailure(item.Error)
				} else {
					results[index] = actionGatewayFailure(errors.New("tool invocation failed"))
				}
			} else {
				results[index] = item.Output
			}
		}
	}
	return results, response, nil
}

func actionGatewayContent(output json.RawMessage) string {
	var text string
	if json.Unmarshal(output, &text) == nil {
		return text
	}
	if len(output) == 0 {
		return "null"
	}
	return string(output)
}

// HandleChatToolCalls returns Chat Completions tool messages for the first choice.
func (session *ActionGatewaySession) HandleChatToolCalls(ctx context.Context, completion *ChatCompletion) ([]ChatCompletionMessage, *Response, error) {
	if completion == nil || len(completion.Choices) == 0 {
		return nil, nil, nil
	}
	var calls []ActionGatewayToolCall
	for _, call := range completion.Choices[0].Message.ToolCalls {
		calls = append(calls, ActionGatewayToolCall{ID: call.ID, Name: call.Function.Name, Arguments: json.RawMessage(call.Function.Arguments)})
	}
	if len(calls) == 0 {
		return nil, nil, nil
	}
	results, response, err := session.ExecuteToolCalls(ctx, calls, "")
	if err != nil {
		return nil, response, err
	}
	messages := make([]ChatCompletionMessage, len(calls))
	for index, call := range calls {
		text := actionGatewayContent(results[index])
		messages[index] = ChatCompletionMessage{Role: "tool", ToolCallID: call.ID, Content: StringContent(text)}
	}
	return messages, response, nil
}

// HandleMessageToolCalls returns a single Messages user turn of tool results.
func (session *ActionGatewaySession) HandleMessageToolCalls(ctx context.Context, message *Message) ([]MessageParam, *Response, error) {
	if message == nil {
		return nil, nil, nil
	}
	var calls []ActionGatewayToolCall
	for _, block := range message.Content {
		if block.Type == "tool_use" {
			arguments := json.RawMessage(`{}`)
			if block.Input != nil {
				var err error
				arguments, err = json.Marshal(block.Input)
				if err != nil {
					return nil, nil, fmt.Errorf("action gateway: marshal tool input: %w", err)
				}
			}
			calls = append(calls, ActionGatewayToolCall{ID: block.ID, Name: block.Name, Arguments: arguments})
		}
	}
	if len(calls) == 0 {
		return nil, nil, nil
	}
	results, response, err := session.ExecuteToolCalls(ctx, calls, "")
	if err != nil {
		return nil, response, err
	}
	blocks := make([]map[string]string, len(calls))
	for index, call := range calls {
		blocks[index] = map[string]string{"type": "tool_result", "tool_use_id": call.ID, "content": actionGatewayContent(results[index])}
	}
	content, err := json.Marshal(blocks)
	if err != nil {
		return nil, response, err
	}
	return []MessageParam{{Role: "user", Content: content}}, response, nil
}

// ActionGatewayResponseToolOutput is a function_call_output item for Responses input.
type ActionGatewayResponseToolOutput struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

// HandleResponseToolCalls returns Responses function-call output items.
func (session *ActionGatewaySession) HandleResponseToolCalls(ctx context.Context, responseBody *ResponsesResponse) ([]ActionGatewayResponseToolOutput, *Response, error) {
	if responseBody == nil {
		return nil, nil, nil
	}
	var calls []ActionGatewayToolCall
	for _, item := range responseBody.Output {
		if item.Type == "function_call" {
			call := ActionGatewayToolCall{}
			if item.CallID != nil {
				call.ID = *item.CallID
			}
			if item.Name != nil {
				call.Name = *item.Name
			}
			if item.Arguments != nil {
				call.Arguments = json.RawMessage(*item.Arguments)
			}
			calls = append(calls, call)
		}
	}
	if len(calls) == 0 {
		return nil, nil, nil
	}
	results, response, err := session.ExecuteToolCalls(ctx, calls, "")
	if err != nil {
		return nil, response, err
	}
	items := make([]ActionGatewayResponseToolOutput, len(calls))
	for index, call := range calls {
		items[index] = ActionGatewayResponseToolOutput{Type: "function_call_output", CallID: call.ID, Output: actionGatewayContent(results[index])}
	}
	return items, response, nil
}
