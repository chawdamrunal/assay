// Package claude wraps the Anthropic SDK behind a Client interface so the
// scanner can be tested without hitting the real API. Only one place in
// the codebase imports the real SDK (this file); tests inject FakeClient.
package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/chawdamrunal/assay/internal/auth"
)

// Request is the Assay-facing shape of an Anthropic message request.
type Request struct {
	Model       string
	System      string
	Messages    []Message
	Tools       []ToolDef
	MaxTokens   int
	Temperature float64

	// CacheBreakpoints marks indices into Messages/Tools that should be
	// flagged with cache_control: ephemeral for prompt caching.
	CacheBreakpoints []CacheBreakpoint
}

// Content is one piece of a message (text, tool_use, or tool_result).
type Content struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	ToolUseID string `json:"tool_use_id,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
	// Name and Input carry the tool name + arguments when this Content is an
	// assistant tool_use block (Type == "tool_use"). They are required so the
	// agent loop can reconstruct the assistant turn that requested a tool,
	// which must precede the user turn carrying the matching tool_result —
	// otherwise the API rejects the request with 400 "unexpected tool_use_id
	// in tool_result blocks". Unused for text and tool_result blocks.
	Name  string         `json:"name,omitempty"`
	Input map[string]any `json:"input,omitempty"`
}

// Message is a role + content list.
type Message struct {
	Role    string    `json:"role"`
	Content []Content `json:"content"`
}

// ToolDef declares a tool the model may call.
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// CacheBreakpoint identifies what to cache.
type CacheBreakpoint struct {
	Kind  string
	Index int
}

// Response is the agent-facing shape of a completion.
type Response struct {
	Text     string
	ToolUses []ToolUse
	Stop     string
	Usage    Usage
}

// ToolUse is one tool invocation requested by the model.
type ToolUse struct {
	ID    string
	Name  string
	Input map[string]any
}

// Usage captures token counts for budget tracking.
type Usage struct {
	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int
}

// Client is the narrow interface used by the scanner.
type Client interface {
	Complete(ctx context.Context, req Request) (Response, error)
}

// RealClient forwards to the official Anthropic SDK.
type RealClient struct {
	inner anthropic.Client
}

// NewRealClient constructs a RealClient using an API key (legacy convenience).
// New code should prefer NewRealClientFromCredentials for OAuth-bearer support.
func NewRealClient(apiKey string, httpClient *http.Client) (*RealClient, error) {
	if apiKey == "" {
		return nil, errors.New("anthropic api key is empty")
	}
	return NewRealClientFromCredentials(&auth.Credentials{
		Kind:   auth.KindAPIKey,
		APIKey: apiKey,
		Source: auth.MethodAssayKey,
	}, httpClient)
}

// NewRealClientFromCredentials constructs a RealClient from an auth.Credentials.
// API keys go on the x-api-key header; bearer tokens go on Authorization: Bearer.
func NewRealClientFromCredentials(creds *auth.Credentials, httpClient *http.Client) (*RealClient, error) {
	if creds == nil {
		return nil, errors.New("anthropic credentials are nil")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	opts := []option.RequestOption{option.WithHTTPClient(httpClient)}
	switch creds.Kind {
	case auth.KindAPIKey:
		if creds.APIKey == "" {
			return nil, errors.New("anthropic api key is empty")
		}
		opts = append(opts, option.WithAPIKey(creds.APIKey))
	case auth.KindBearer:
		if creds.BearerToken == "" {
			return nil, errors.New("anthropic bearer token is empty")
		}
		// SDK v1.43.0: option.WithAuthToken sets "Authorization: Bearer <token>".
		opts = append(opts, option.WithAuthToken(creds.BearerToken))
	default:
		return nil, fmt.Errorf("unknown credential kind: %s", creds.Kind)
	}
	inner := anthropic.NewClient(opts...)
	return &RealClient{inner: inner}, nil
}

// Complete forwards the request to the Anthropic SDK and returns a translated Response.
//
// CacheBreakpoint.Kind values:
//   - "system": cache the system prompt block
//   - "tools":  cache after Tools[Index] (so prefix tools are cached)
//   - "message": cache the last content block of Messages[Index]
func (r *RealClient) Complete(ctx context.Context, req Request) (Response, error) {
	maxTokens := int64(req.MaxTokens)
	if maxTokens == 0 {
		maxTokens = 4096
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: maxTokens,
	}
	if req.Temperature != 0 {
		params.Temperature = anthropic.Float(req.Temperature)
	}

	// System prompt — single TextBlockParam.
	if req.System != "" {
		sysBlock := anthropic.TextBlockParam{Text: req.System}
		params.System = []anthropic.TextBlockParam{sysBlock}
	}

	// Tools.
	if len(req.Tools) > 0 {
		params.Tools = make([]anthropic.ToolUnionParam, len(req.Tools))
		for i, t := range req.Tools {
			tp := anthropic.ToolParam{
				Name:        t.Name,
				InputSchema: schemaToParam(t.InputSchema),
			}
			if t.Description != "" {
				tp.Description = anthropic.String(t.Description)
			}
			params.Tools[i] = anthropic.ToolUnionParam{OfTool: &tp}
		}
	}

	// Messages.
	if len(req.Messages) > 0 {
		params.Messages = make([]anthropic.MessageParam, len(req.Messages))
		for i, m := range req.Messages {
			blocks := make([]anthropic.ContentBlockParamUnion, 0, len(m.Content))
			for _, c := range m.Content {
				switch c.Type {
				case "text", "":
					blocks = append(blocks, anthropic.NewTextBlock(c.Text))
				case "tool_result":
					blocks = append(blocks, anthropic.NewToolResultBlock(c.ToolUseID, c.Text, c.IsError))
				case "tool_use":
					blocks = append(blocks, anthropic.NewToolUseBlock(c.ToolUseID, c.Input, c.Name))
				default:
					// Unknown content type — treat as text to avoid losing data.
					blocks = append(blocks, anthropic.NewTextBlock(c.Text))
				}
			}
			role := anthropic.MessageParamRoleUser
			if m.Role == "assistant" {
				role = anthropic.MessageParamRoleAssistant
			}
			params.Messages[i] = anthropic.MessageParam{Role: role, Content: blocks}
		}
	}

	// Apply cache-control breakpoints.
	for _, bp := range req.CacheBreakpoints {
		applyCacheBreakpoint(&params, bp)
	}

	resp, err := r.inner.Messages.New(ctx, params)
	if err != nil {
		return Response{}, err
	}
	if resp == nil {
		return Response{}, errors.New("anthropic SDK returned nil message")
	}

	out := Response{
		Stop: string(resp.StopReason),
		Usage: Usage{
			InputTokens:         int(resp.Usage.InputTokens),
			OutputTokens:        int(resp.Usage.OutputTokens),
			CacheCreationTokens: int(resp.Usage.CacheCreationInputTokens),
			CacheReadTokens:     int(resp.Usage.CacheReadInputTokens),
		},
	}
	for _, blk := range resp.Content {
		switch blk.Type {
		case "text":
			out.Text += blk.Text
		case "tool_use":
			var input map[string]any
			if len(blk.Input) > 0 {
				_ = json.Unmarshal(blk.Input, &input)
			}
			out.ToolUses = append(out.ToolUses, ToolUse{
				ID:    blk.ID,
				Name:  blk.Name,
				Input: input,
			})
		}
	}
	return out, nil
}

// schemaToParam converts the Assay tool input schema (map[string]any) into the
// SDK's ToolInputSchemaParam shape.
func schemaToParam(schema map[string]any) anthropic.ToolInputSchemaParam {
	p := anthropic.ToolInputSchemaParam{}
	if schema == nil {
		return p
	}
	if props, ok := schema["properties"]; ok {
		p.Properties = props
	}
	if req, ok := schema["required"].([]string); ok {
		p.Required = req
	} else if reqAny, ok := schema["required"].([]any); ok {
		req := make([]string, 0, len(reqAny))
		for _, v := range reqAny {
			if s, ok := v.(string); ok {
				req = append(req, s)
			}
		}
		p.Required = req
	}
	return p
}

// applyCacheBreakpoint attaches a CacheControl ephemeral marker at the spot
// indicated by the breakpoint Kind/Index.
func applyCacheBreakpoint(params *anthropic.MessageNewParams, bp CacheBreakpoint) {
	cc := anthropic.NewCacheControlEphemeralParam()
	switch bp.Kind {
	case "system":
		if len(params.System) > 0 {
			params.System[len(params.System)-1].CacheControl = cc
		}
	case "tools":
		if bp.Index >= 0 && bp.Index < len(params.Tools) {
			if t := params.Tools[bp.Index].OfTool; t != nil {
				t.CacheControl = cc
			}
		}
	case "message":
		if bp.Index >= 0 && bp.Index < len(params.Messages) {
			content := params.Messages[bp.Index].Content
			if len(content) == 0 {
				return
			}
			last := &content[len(content)-1]
			switch {
			case last.OfText != nil:
				last.OfText.CacheControl = cc
			case last.OfToolResult != nil:
				last.OfToolResult.CacheControl = cc
			case last.OfToolUse != nil:
				last.OfToolUse.CacheControl = cc
			}
		}
	}
}
