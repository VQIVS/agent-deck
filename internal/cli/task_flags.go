package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/vqivs/agent-deck/internal/team"
)

type taskValues []string

func (v *taskValues) String() string         { return fmt.Sprint([]string(*v)) }
func (v *taskValues) Set(value string) error { *v = append(*v, value); return nil }

type taskFlags struct {
	Team, Server, File string
	JSON, ReadOnly     bool
	Call               team.WorkflowCall
}

func parseTaskFlags(command string, args []string) (taskFlags, error) {
	var options taskFlags
	f := flag.NewFlagSet("agent-deck task "+command, flag.ContinueOnError)
	f.StringVar(&options.Team, "team", "", "saved team name")
	f.StringVar(&options.Server, "server", "", "override OpenCode URL")
	f.BoolVar(&options.JSON, "json", false, "print JSON")
	f.BoolVar(&options.ReadOnly, "read-only", false, "disable mutations")
	f.StringVar(&options.Call.ID, "id", "", "task ID")
	if err := taskActionFlags(f, &options, command); err != nil {
		return options, err
	}
	if err := f.Parse(args); err != nil {
		return options, fmt.Errorf("parse task flags: %w", err)
	}
	if f.NArg() != 0 || options.Team == "" {
		return options, fmt.Errorf("--team required; unexpected positional arguments: %v", f.Args())
	}
	if options.File != "" {
		if err := mergeTaskFile(f, &options); err != nil {
			return options, err
		}
	}
	if command != "ls" && command != "add" && options.Call.ID == "" {
		return options, fmt.Errorf("--id is required")
	}
	return options, nil
}

func taskActionFlags(f *flag.FlagSet, options *taskFlags, command string) error {
	if command == "add" || command == "edit" {
		addTaskSpecFlags(f, options)
	} else {
		switch command {
		case "ls", "show":
		case "move":
			f.StringVar(&options.Call.State, "state", "", "target state")
		case "assign":
			f.StringVar(&options.Call.Assignee, "assignee", "", "current member name")
			f.StringVar(&options.Call.Reviewer, "reviewer", "", "reviewer name")
		case "comment":
			f.StringVar(&options.Call.Text, "text", "", "comment text")
		case "review":
			f.StringVar(&options.Call.Decision, "decision", "", "accept or changes")
			f.StringVar(&options.Call.Comment, "comment", "", "review comment")
		default:
			return fmt.Errorf("unknown task command %q", command)
		}
	}
	return nil
}

func addTaskSpecFlags(f *flag.FlagSet, o *taskFlags) {
	f.StringVar(&o.File, "file", "", "TaskSpec JSON file")
	f.StringVar(&o.Call.Title, "title", "", "task title")
	f.StringVar(&o.Call.Description, "description", "", "task description")
	f.Var((*taskValues)(&o.Call.Criteria), "criteria", "acceptance criterion (repeatable)")
	f.Var((*taskValues)(&o.Call.Paths), "path", "owned path (repeatable)")
	f.Var((*taskValues)(&o.Call.Dependencies), "dependency", "dependency ID (repeatable)")
	f.Var((*taskValues)(&o.Call.Labels), "label", "label (repeatable)")
	f.StringVar(&o.Call.Priority, "priority", "", "low, normal, high or urgent")
	f.StringVar(&o.Call.Estimate, "estimate", "", "duration such as 10m")
	f.Int64Var(&o.Call.Tokens, "tokens", 0, "estimated tokens")
	f.StringVar(&o.Call.Assignee, "assignee", "", "current member name")
	f.StringVar(&o.Call.Reviewer, "reviewer", "", "reviewer name")
	f.BoolVar(&o.Call.Ready, "ready", false, "create ready task")
}

func mergeTaskFile(f *flag.FlagSet, o *taskFlags) error {
	data, err := os.ReadFile(o.File)
	if err != nil {
		return fmt.Errorf("read task spec: %w", err)
	}
	if len(data) > 1<<20 {
		return fmt.Errorf("task spec exceeds 1 MiB")
	}
	var spec team.TaskSpec
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&spec); err != nil {
		return fmt.Errorf("decode task spec: %w", err)
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return fmt.Errorf("task spec must contain one JSON value")
	}
	flags := map[string]string{
		"id": "id", "title": "title", "description": "description", "criteria": "criteria",
		"path": "paths", "dependency": "dependencies", "label": "labels", "priority": "priority", "estimate": "estimate",
		"tokens": "tokens", "assignee": "assignee", "reviewer": "reviewer", "ready": "ready",
	}
	encoded, _ := json.Marshal(o.Call.TaskSpec)
	var overrides map[string]json.RawMessage
	_ = json.Unmarshal(encoded, &overrides)
	encoded, _ = json.Marshal(spec)
	var merged map[string]json.RawMessage
	_ = json.Unmarshal(encoded, &merged)
	f.Visit(func(field *flag.Flag) {
		if key, ok := flags[field.Name]; ok {
			if value, exists := overrides[key]; exists {
				merged[key] = value
			} else {
				delete(merged, key)
			}
		}
	})
	encoded, _ = json.Marshal(merged)
	if err := json.Unmarshal(encoded, &o.Call.TaskSpec); err != nil {
		return fmt.Errorf("merge task flags: %w", err)
	}
	return nil
}
