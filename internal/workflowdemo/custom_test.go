package workflowdemo

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vqivs/agent-deck/internal/team"
)

func customWorkflow() team.WorkflowDefinition {
	return team.WorkflowDefinition{
		Stages: []team.WorkflowStage{
			{Name: "Ideas", Color: "#929BAA", Category: "todo"},
			{Name: "Queue", Color: "#B7A3F5", Category: "ready"},
			{Name: "Building", Color: "#E5BB79", Category: "active"},
			{Name: "Checking", Color: "#DDA2CD", Category: "review"},
			{Name: "Accepted", Color: "#89C9A0", Category: "done"},
			{Name: "Delivered", Color: "#89C9A0", Category: "shipped"},
			{Name: "Waiting", Color: "#EF9393", Category: "blocked"},
			{Name: "Dropped", Color: "#929BAA", Category: team.TaskCancelled},
		},
		Transitions: map[string]team.WorkflowTransition{
			"Delivered": {From: []string{"Accepted"}, By: []string{"release-manager", "user"}},
		},
	}
}

func TestCustomLifecycleShipmentPreservesAcceptance(t *testing.T) {
	board := team.TaskBoard{Version: 2}
	roster := team.Team{Members: []team.Member{
		{Name: "Lead", Role: "master"},
		{Name: "release-manager", Role: "worker"},
		{Name: "Builder", Role: "worker"},
		{Name: "Reviewer", Role: "worker"},
	}}
	now := time.Date(2026, 10, 25, 13, 0, 0, 0, time.UTC)
	user := team.TaskMutation{Actor: team.TaskActor{User: true}, Now: now}
	if err := board.SetWorkflow(customWorkflow(), user); err != nil {
		t.Fatal(err)
	}
	task := team.Task{
		ID: "custom", Title: "Custom lifecycle fixture", Assignee: "Builder", Reviewer: "Reviewer",
		Paths: []string{"custom-fixture/**"}, Criteria: []team.TaskCriterion{{ID: "c1", Text: "Fixture observed"}},
	}
	if err := board.CreateTask(&task, user); err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"Queue", "Building"} {
		if err := board.MoveTask(task.ID, stage, user); err != nil {
			t.Fatal(err)
		}
	}
	if !team.TaskActive(&board.Tasks[0]) {
		t.Fatal("custom active mutation left stale category cache")
	}
	for _, stage := range []string{"Accepted", "Delivered", "undefined-stage"} {
		before, _ := json.Marshal(board)
		if err := board.MoveTaskRelease(task.ID, stage, &team.TaskRelease{Version: "v-unreviewed"}, user); err == nil {
			t.Fatalf("unfinished work entered %q without acceptance", stage)
		}
		after, _ := json.Marshal(board)
		if !bytes.Equal(before, after) {
			t.Fatal("rejected unfinished/undefined transition mutated board")
		}
	}
	worker := team.TaskMutation{Actor: team.TaskActor{Member: "Builder", Role: "worker"}, Now: now.Add(time.Minute)}
	if err := board.MoveTask(task.ID, "Checking", worker); err != nil {
		t.Fatal(err)
	}
	reviewer := team.TaskMutation{Actor: team.TaskActor{Member: "Reviewer", Role: "worker"}, Now: now.Add(2 * time.Minute)}
	beforeAcceptance := board
	if err := board.ReviewTask(task.ID, "accept", "Observed fixture acceptance", reviewer); err != nil {
		t.Fatal(err)
	}
	accepted := board.Tasks[0]
	acceptedNotices := team.TaskNotices(&beforeAcceptance, &board, &roster, reviewer.Actor)
	if !slices.ContainsFunc(acceptedNotices, func(notice team.TaskNotice) bool {
		return notice.To == "release-manager" && notice.Event != "" && notice.Task == task.ID
	}) {
		t.Fatal("custom accepted stage did not produce attributable release queue notice")
	}
	if accepted.State != "Accepted" || accepted.Review == nil || accepted.CompletedAt.IsZero() {
		t.Fatal("custom review failed to record accepted completion")
	}
	if !team.TaskAccepted(&board.Tasks[0]) || team.TaskActive(&board.Tasks[0]) {
		t.Fatal("review acceptance left stale category cache")
	}
	store := team.BoardStore{Root: t.TempDir(), Team: "metric-fixture"}
	if err := team.RecordEstimationHistory(t.Context(), store, &board); err != nil {
		t.Fatal(err)
	}
	beforeRename := board.Tasks[0]
	if err := board.RenameStage("Accepted", "Ready to ship", user); err != nil {
		t.Fatal(err)
	}
	if board.Tasks[0].State != "Ready to ship" || !reflect.DeepEqual(board.Tasks[0].History, beforeRename.History) ||
		!reflect.DeepEqual(board.Tasks[0].Review, beforeRename.Review) {
		t.Fatal("occupied rename changed acceptance/history instead of only placement")
	}
	if rule := board.Workflow.Transitions["Delivered"]; len(rule.From) != 1 || rule.From[0] != "Ready to ship" {
		t.Fatal("occupied rename did not update transition reference")
	}
	beforePreview, _ := json.Marshal(board)
	if moves := board.ValidMoves(task.ID, user); len(moves) != 1 || moves[0].Name != "Delivered" {
		t.Fatalf("shipment discovery needs no fabricated version: %+v", moves)
	}
	afterPreview, _ := json.Marshal(board)
	if !bytes.Equal(beforePreview, afterPreview) {
		t.Fatal("valid-moves discovery mutated accepted task")
	}
	for _, actor := range []team.TaskActor{
		{Member: "Lead", Role: "master"}, {Member: "Builder", Role: "worker"}, team.AutomationActor,
	} {
		before, _ := json.Marshal(board)
		mutation := team.TaskMutation{Actor: actor, Now: now.Add(3 * time.Minute)}
		if err := board.MoveTaskRelease(task.ID, "Delivered", &team.TaskRelease{Version: "v-fixture"}, mutation); err == nil {
			t.Fatalf("unauthorized shipment accepted for %+v", actor)
		}
		after, _ := json.Marshal(board)
		if !bytes.Equal(before, after) {
			t.Fatal("denied shipment mutated board")
		}
	}
	release := team.TaskMutation{
		Actor: team.TaskActor{Member: "release-manager", Role: "worker"}, Now: now.Add(4 * time.Minute),
	}
	beforeShipment := board
	if err := board.MoveTask(task.ID, "Delivered", release); err == nil {
		t.Fatal("shipment without version accepted")
	}
	if err := board.MoveTaskRelease(task.ID, "Delivered", &team.TaskRelease{Version: "v-fixture"}, release); err != nil {
		t.Fatal(err)
	}
	shipped := board.Tasks[0]
	shipmentNotices := team.TaskNotices(&beforeShipment, &board, &roster, release.Actor)
	if !slices.ContainsFunc(shipmentNotices, func(notice team.TaskNotice) bool {
		return notice.To == "Lead" && notice.Event != "" && strings.Contains(notice.Text, "v-fixture")
	}) {
		t.Fatal("custom shipped stage did not notify lead with version/event")
	}
	if !shipped.CompletedAt.Equal(accepted.CompletedAt) || !reflect.DeepEqual(shipped.Review, accepted.Review) {
		t.Fatal("shipment changed implementation completion or acceptance evidence")
	}
	if shipped.ReleaseVersion != "v-fixture" || shipped.ShippedAt.IsZero() || team.TaskActive(&shipped) {
		t.Fatal("shipment version/time/terminal category incorrect")
	}
	if !team.TaskAccepted(&shipped) || !team.TaskShipped(&shipped) {
		t.Fatal("shipment mutation left stale category cache")
	}
	saved := team.BoardState{Version: 2, Board: board}
	if err := store.Save(t.Context(), &saved, 0); err != nil {
		t.Fatal(err)
	}
	reopened, err := (team.BoardStore{Root: store.Root, Team: store.Team}).Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reopened.Board, saved.Board) || !team.TaskShipped(&reopened.Board.Tasks[0]) {
		t.Fatal("restart lost custom definition, shipment evidence or category binding")
	}
	for range 2 {
		if err := team.RecordEstimationHistory(t.Context(), store, &board); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(filepath.Join(store.Root, store.Team, "estimation-history.json"))
	if err != nil {
		t.Fatal(err)
	}
	var history struct {
		Samples []team.EstimationSample `json:"samples"`
	}
	if err := json.Unmarshal(data, &history); err != nil || len(history.Samples) != 1 {
		t.Fatalf("shipment double-counted implementation metrics: samples=%d error=%v", len(history.Samples), err)
	}
	if err := board.MoveTask(task.ID, "Queue", user); err == nil {
		t.Fatal("shipped task reopened")
	}
	dependent := team.Task{
		ID: "dependent", Title: "Shipment still satisfies ordinary dependency", Dependencies: []string{task.ID},
		Paths: []string{"dependent/**"}, Criteria: []team.TaskCriterion{{ID: "dependency", Text: "Dependency is accepted"}},
	}
	if err := board.CreateTask(&dependent, user); err != nil {
		t.Fatal(err)
	}
	if err := board.MoveTask(dependent.ID, "Queue", user); err != nil {
		t.Fatalf("shipment broke completed dependency: %v", err)
	}
}
