package team

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"
)

func TestWorkflowViewPreservesActorGuardsAndReadOnlyObservation(t *testing.T) {
	board, mutation := workflowFixture(t)
	before, err := json.Marshal(board)
	if err != nil {
		t.Fatal(err)
	}
	view, err := ReadWorkflowView(board, "task", mutation)
	if err != nil || view.Task != "task" || !view.ReleaseVersionRequired || !slices.Contains(view.ValidMoves, "ready") {
		t.Fatal(view, err)
	}
	mutation.Actor = TaskActor{Member: "outsider", Role: "worker"}
	denied, err := ReadWorkflowView(board, "task", mutation)
	if err != nil || len(denied.ValidMoves) != 0 {
		t.Fatal("workflow read bypassed move guards", denied, err)
	}
	after, err := json.Marshal(board)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("workflow observation mutated board", err)
	}
	if _, err := ReadWorkflowView(board, "missing", mutation); err == nil {
		t.Fatal("missing task accepted")
	}
	definition, err := ReadWorkflowView(board, "", mutation)
	if err != nil || definition.ValidMoves == nil {
		t.Fatal(definition, err)
	}
	data, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 3 || fields["workflow"] == nil || string(fields["valid_moves"]) != "[]" ||
		string(fields["release_version_required"]) != "true" {
		t.Fatal("workflow response schema changed", string(data))
	}
}
