package mcp

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestTaskToolsParallelSchemaAllowsNoChange(t *testing.T) {
	tools := &TaskTools{}
	found := map[string]bool{}
	for _, definition := range tools.Definitions() {
		name, _ := definition["name"].(string)
		if name != "create_task" && name != "update_task" {
			continue
		}
		found[name] = true
		schema := definition["inputSchema"].(map[string]any)
		properties := schema["properties"].(map[string]any)
		parallel := properties["parallel"].(map[string]any)
		types, ok := parallel["type"].([]string)
		if !ok || !slices.Contains(types, "boolean") || !slices.Contains(types, "null") {
			t.Fatalf("%s cannot express no-change in required-property clients: %+v", name, parallel)
		}
		if required, ok := schema["required"].([]string); ok && slices.Contains(required, "parallel") {
			t.Fatalf("%s unnecessarily requires parallel", name)
		}
		if name == "update_task" {
			required, ok := schema["required"].([]string)
			if !ok || !slices.Equal(required, []string{"id"}) {
				t.Fatalf("update_task required=%v, want exactly [id]", schema["required"])
			}
		}
		if description, _ := parallel["description"].(string); !strings.Contains(description, "preserve") ||
			!strings.Contains(description, "defaults to true") {
			t.Fatalf("%s missing parallel edit/create semantics: %+v", name, parallel)
		}
	}
	for _, name := range []string{"create_task", "update_task"} {
		if !found[name] {
			t.Fatalf("missing %s definition", name)
		}
	}
}

func TestTaskToolsParallelPartialEditPersistence(t *testing.T) {
	for _, initial := range []bool{true, false} {
		t.Run(map[bool]string{true: "parallel", false: "serial"}[initial], func(t *testing.T) {
			tools, _ := taskToolsFixture(t)
			creation, _ := json.Marshal(map[string]any{
				"title": "Original", "criteria": []string{"Pass"}, "paths": []string{"src/**"},
				"parallel": initial, "estimate": "10m", "tokens": 100,
			})
			if _, err := callTask(t, tools, "lead", "create_task", string(creation)); err != nil {
				t.Fatal(err)
			}
			for _, step := range []struct {
				input string
				want  bool
			}{
				{`{"id":"T-1","title":"Renamed"}`, initial},
				{`{"id":"T-1","estimate":"20m","tokens":200}`, initial},
				{`{"id":"T-1","parallel":null,"description":"Nullable omission"}`, initial},
				{`{"id":"T-1","parallel":false}`, false},
				{`{"id":"T-1","title":"Still serial"}`, false},
				{`{"id":"T-1","parallel":true}`, true},
				{`{"id":"T-1","title":"Still parallel"}`, true},
			} {
				if _, err := callTask(t, tools, "lead", "update_task", step.input); err != nil {
					t.Fatalf("%s: %v", step.input, err)
				}
				_, store, err := tools.Provider(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				loaded, err := store.Load(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				if got := loaded.Board.Tasks[0].Estimate.Parallel; got != step.want {
					t.Fatalf("after %s: persisted parallel=%v, want %v", step.input, got, step.want)
				}
			}
		})
	}
}

func TestTaskToolsParallelCreateDefaults(t *testing.T) {
	for _, input := range []string{
		`{"title":"Default","criteria":["Pass"],"paths":["src/**"]}`,
		`{"title":"Nullable default","criteria":["Pass"],"paths":["src/**"],"parallel":null}`,
	} {
		tools, _ := taskToolsFixture(t)
		if _, err := callTask(t, tools, "lead", "create_task", input); err != nil {
			t.Fatal(err)
		}
		_, store, err := tools.Provider(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := store.Load(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if !loaded.Board.Tasks[0].Estimate.Parallel {
			t.Fatalf("creation did not default to parallel: %s", input)
		}
	}
}
