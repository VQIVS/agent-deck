package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/qivs/agent-deck/internal/server"
	"github.com/qivs/agent-deck/internal/team"
)

func openTeamPool(ctx context.Context, manager *team.Manager) (*team.PoolRuntime, error) {
	root, err := server.StateRoot()
	if err != nil {
		return nil, fmt.Errorf("pool state: %w", err)
	}
	pool, err := team.OpenPool(ctx, manager.Store, manager.Team.Name, team.BoardStore{
		Root: filepath.Join(root, "boards"), Team: manager.Team.Name, ReadOnly: manager.ReadOnly,
	})
	if err != nil {
		return nil, fmt.Errorf("open team pool: %w", err)
	}
	return pool, nil
}

func poolRequests(ctx context.Context, manager *team.Manager) ([]team.ReconcileAction, error) {
	if manager.ReadOnly {
		return []team.ReconcileAction{}, nil
	}
	pool, err := openTeamPool(ctx, manager)
	if err != nil {
		return nil, err
	}
	actions, err := pool.Pending(ctx, team.ReconcileActor{User: true})
	if err != nil {
		return nil, fmt.Errorf("read pool requests: %w", err)
	}
	return actions, nil
}

func poolReply(ctx context.Context, manager *team.Manager, id string, approve bool, out io.Writer) error {
	if manager.ReadOnly {
		return fmt.Errorf("read-only: pool approval disabled")
	}
	pool, err := openTeamPool(ctx, manager)
	if err != nil {
		return err
	}
	actor := team.ReconcileActor{User: true}
	if !approve {
		if err := pool.Reject(ctx, actor, id); err != nil {
			return fmt.Errorf("reject pool action: %w", err)
		}
		return writeText(out, "Pool action rejected")
	}
	actions, err := pool.Pending(ctx, actor)
	if err != nil {
		return fmt.Errorf("inspect approval: %w", err)
	}
	detail := ""
	for i := range actions {
		if actions[i].ID == id {
			encoded, _ := json.MarshalIndent(actions[i], "", "  ")
			detail = string(encoded)
		}
	}
	if detail == "" {
		return fmt.Errorf("pool action no longer pending")
	}
	if err := confirmPoolAction(os.Stdin, out, detail); err != nil {
		return err
	}
	if err := pool.Approve(ctx, actor, id); err != nil {
		return fmt.Errorf("approve pool action: %w", err)
	}
	result, err := pool.Controller(actor).Apply(ctx, actor, id)
	if err != nil {
		return fmt.Errorf("apply approved pool action: %w", err)
	}
	return writeJSON(out, result)
}

func poolBoard(ctx context.Context, manager *team.Manager, opts *teamFlags, out io.Writer) error {
	pool, err := openTeamPool(ctx, manager)
	if err != nil {
		return err
	}
	state, err := pool.Status(ctx)
	if err != nil {
		return fmt.Errorf("read board: %w", err)
	}
	if opts.File != "" {
		if manager.ReadOnly {
			return fmt.Errorf("read-only: board configuration disabled")
		}
		data, err := os.ReadFile(opts.File)
		if err != nil {
			return fmt.Errorf("read board input: %w", err)
		}
		var next team.BoardState
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&next); err != nil {
			return fmt.Errorf("decode board input: %w", err)
		}
		if err := confirmPoolAction(os.Stdin, out, string(data)); err != nil {
			return err
		}
		if err := pool.Configure(ctx, team.ReconcileActor{User: true}, &next, state.Revision); err != nil {
			return fmt.Errorf("configure board: %w", err)
		}
		state = next
	}
	return writeJSON(out, state)
}

func poolRecover(ctx context.Context, manager *team.Manager, opts *teamFlags, out io.Writer) error {
	if opts.ID == "" {
		return fmt.Errorf("--id is required")
	}
	root, err := server.StateRoot()
	if err != nil {
		return fmt.Errorf("recovery state: %w", err)
	}
	transport := team.LifecycleTransport{Store: team.BoardStore{
		Root: filepath.Join(root, "boards"),
		Team: manager.Team.Name, ReadOnly: manager.ReadOnly,
	}, LoadManager: func(context.Context) (*team.Manager, error) { return manager, nil }}
	actor := team.ReconcileActor{User: true}
	recovery, err := transport.Inspect(ctx, actor, opts.ID)
	if err != nil {
		return fmt.Errorf("inspect pool recovery: %w", err)
	}
	if opts.Text == "inspect" {
		return writeJSON(out, recovery)
	}
	if manager.ReadOnly {
		return fmt.Errorf("read-only: recovery disabled")
	}
	data, _ := json.MarshalIndent(recovery, "", "  ")
	if err := confirmPoolAction(os.Stdin, out, string(data)+"\nDecision: "+opts.Text); err != nil {
		return err
	}
	if err := transport.Resolve(ctx, actor, opts.ID, opts.Text); err != nil {
		return fmt.Errorf("resolve pool recovery: %w", err)
	}
	return writeText(out, "Pool recovery resolved")
}
