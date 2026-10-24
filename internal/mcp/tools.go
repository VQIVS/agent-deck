package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

func tools() []map[string]any {
	text := map[string]any{"type": "string"}
	return []map[string]any{
		tool("team_members", "Read the current team roster", nil, nil),
		tool("channel_read", "Read visible messages after a message ID", map[string]any{"since": text}, nil),
		tool("channel_post", "Post to the channel; only explicit mentions request delivery", map[string]any{
			"text": text, "reply_to": text, "mentions": map[string]any{"type": "array", "items": text},
		}, []string{"text"}),
		tool("dm", "Send a private message to one member", map[string]any{
			"text": text, "member": text, "reply_to": text,
		}, []string{"member", "text"}),
	}
}

func tool(name, description string, properties map[string]any, required []string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) != 0 {
		schema["required"] = required
	}
	return map[string]any{"name": name, "description": description, "inputSchema": schema}
}

func (s *Server) call(ctx context.Context, identity Identity, params json.RawMessage) (any, error) {
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
		Meta      json.RawMessage `json:"_meta,omitempty"`
	}
	if err := decode(bytes.NewReader(params), &call); err != nil {
		return nil, err
	}
	if len(call.Arguments) == 0 {
		call.Arguments = json.RawMessage(`{}`)
	}
	switch call.Name {
	case "propose_workers", "assign_task", "retire_worker":
		if s.Scheduler == nil {
			return nil, fmt.Errorf("pool scheduling is not configured")
		}
		return s.Scheduler.Call(ctx, identity, call.Name, call.Arguments)
	case "submit_report":
		if s.SubmitReport == nil {
			return nil, fmt.Errorf("report submission is not configured")
		}
		result, err := s.SubmitReport(ctx, identity, call.Arguments)
		if err != nil {
			return nil, fmt.Errorf("submit authenticated report: %w", err)
		}
		return result, nil
	case "team_members":
		if err := decode(bytes.NewReader(call.Arguments), &struct{}{}); err != nil {
			return nil, err
		}
		return s.Service.Members(identity)
	case "channel_read":
		var arguments struct {
			Since string `json:"since"`
		}
		if err := decode(bytes.NewReader(call.Arguments), &arguments); err != nil {
			return nil, err
		}
		return s.Service.Read(ctx, identity, arguments.Since)
	case "channel_post", "dm":
		return s.post(ctx, identity, call.Name, call.Arguments)
	default:
		return nil, fmt.Errorf("unknown tool")
	}
}

func (s *Server) post(ctx context.Context, identity Identity, name string, arguments json.RawMessage) (any, error) {
	var post Post
	if err := decode(bytes.NewReader(arguments), &post); err != nil {
		return nil, err
	}
	return s.Service.Post(ctx, identity, post, name == "dm")
}
