package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	api "github.com/vqivs/agent-deck/internal/opencode"
	"github.com/vqivs/agent-deck/internal/team"
)

func teamStatus(ctx context.Context, manager *team.Manager, opts *teamFlags, out io.Writer) error {
	statuses := make([]team.MemberStatus, 0, len(manager.Team.Members))
	for i := range manager.Team.Members {
		statuses = append(statuses, manager.Observe(ctx, i))
	}
	if opts.JSON {
		return writeJSON(out, statuses)
	}
	for i := range statuses {
		s := &statuses[i]
		activity := "unknown"
		if s.LastActivity > 0 {
			activity = time.UnixMilli(s.LastActivity).UTC().Format(time.RFC3339)
		}
		line := fmt.Sprintf("%s\t%s\t%s\t%s\tpending=%d\tcontext=%d\t%s",
			s.Name, s.Role, s.Session, s.State, s.Pending, s.ContextTokens, activity)
		if s.Error != "" {
			line += "\t" + s.Error
		}
		if err := writeText(out, line); err != nil {
			return err
		}
	}
	return nil
}

func teamRequests(ctx context.Context, manager *team.Manager, opts *teamFlags, out io.Writer) error {
	requests, err := manager.Requests(ctx)
	if err != nil {
		return fmt.Errorf("read team requests: %w", err)
	}
	if opts.JSON {
		actions, poolErr := poolRequests(ctx, manager)
		if poolErr != nil {
			return poolErr
		}
		if len(actions) == 0 {
			return writeJSON(out, requests)
		}
		return writeJSON(out, map[string]any{"requests": requests, "pool_actions": actions})
	}
	actions, err := poolRequests(ctx, manager)
	if err != nil {
		return err
	}
	if err := writePoolRequests(out, actions); err != nil {
		return err
	}
	for i := range requests {
		r := &requests[i]
		detail := ""
		if r.Permission != nil {
			detail = r.Permission.Permission + " " + strings.Join(r.Permission.Patterns, ", ")
		}
		if r.Question != nil {
			for _, question := range r.Question.Questions {
				detail += question.Header + " " + question.Question + " "
			}
		}
		if err := writeText(out, fmt.Sprintf("%s\t%s\t%s\t%s\t%s", r.Member, r.Kind, r.ID, r.Directory, detail)); err != nil {
			return err
		}
	}
	return nil
}

func writePoolRequests(out io.Writer, actions []team.ReconcileAction) error {
	for i := range actions {
		action := &actions[i]
		encoded, err := json.Marshal(action)
		if err != nil {
			return fmt.Errorf("encode pool details: %w", err)
		}
		line := fmt.Sprintf("pool\t%s\tpool:%s\t%s\t%s", action.Kind, action.ID, action.Worker, encoded)
		if err := writeText(out, line); err != nil {
			return err
		}
	}
	return nil
}

func teamReply(ctx context.Context, command string, manager *team.Manager, opts *teamFlags, out io.Writer) error {
	if strings.HasPrefix(opts.ID, "pool:") {
		return poolReply(ctx, manager, strings.TrimPrefix(opts.ID, "pool:"), command == "approve", out)
	}
	if opts.ReadOnly {
		return fmt.Errorf("read-only: permission replies disabled")
	}
	if opts.ID == "" {
		return fmt.Errorf("--id is required")
	}
	requests, err := manager.Requests(ctx)
	if err != nil {
		return fmt.Errorf("resolve request: %w", err)
	}
	var target *team.PendingRequest
	for i := range requests {
		if requests[i].ID == opts.ID {
			if target != nil {
				return fmt.Errorf("request ID %q is ambiguous", opts.ID)
			}
			target = &requests[i]
		}
	}
	if target == nil {
		return fmt.Errorf("request %q is not pending for this team", opts.ID)
	}
	if target.Kind != "permission" {
		return fmt.Errorf("request %q is a question; answer it through OpenCode attach", opts.ID)
	}
	reply := permissionReply(command, opts.Always)
	client := api.New(manager.Team.Server, manager.ReadOnly)
	if err := client.ReplyPermission(ctx, target.Directory, target.ID, reply, opts.Message); err != nil {
		return fmt.Errorf("reply to permission %q: %w", opts.ID, err)
	}
	if opts.JSON {
		return writeJSON(out, map[string]string{"id": target.ID, "member": target.Member, "reply": reply})
	}
	return writeText(out, fmt.Sprintf("%s: %s %s", target.Member, target.ID, reply))
}

func permissionReply(command string, always bool) string {
	if command == "reject" {
		return "reject"
	}
	if always {
		return "always"
	}
	return "once"
}

func teamLast(ctx context.Context, manager *team.Manager, index int, opts *teamFlags, out io.Writer) error {
	member := &manager.Team.Members[index]
	messages, err := api.New(manager.Team.Server, true).Messages(ctx, member.Dir, member.Session)
	if err != nil {
		return fmt.Errorf("read member report: %w", err)
	}
	text := team.AssistantText(messages)
	if opts.JSON {
		return writeJSON(out, map[string]string{"member": member.Name, "session": member.Session, "text": text})
	}
	return writeText(out, text)
}
