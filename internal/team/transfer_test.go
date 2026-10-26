package team

import (
	"reflect"
	"strings"
	"testing"
)

const legacySessions = `server: http://localhost:4096
sessions:
  - {name: Lead, role: lead, session: session-lead, dir: /tmp/project, base: develop}
  - {name: Worker, session: session-worker, dir: /tmp/worker}
`

const adoptedTeam = `version: 1
name: Example
server: http://localhost:4096
repo: /work/agent-deck
base: develop
capacity: 1
members:
  - {name: Lead, role: master, session: session-lead, dir: /work/agent-deck, mode: adopt, capability: inherited}
  - {name: Worker, role: worker, session: session-worker, dir: /work/worker, mode: adopt, capability: inherited}
`

func TestTransferRoundTrip(t *testing.T) {
	source, err := Import([]byte(adoptedTeam), ImportOptions{Name: "Example"})
	if err != nil {
		t.Fatal(err)
	}
	source.Members[0].System = "Coordinate carefully"
	source.Members[1].Branch = "example/worker"
	for _, format := range []string{"yaml", "json"} {
		b, err := Export(&source, format, false)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Import(b, ImportOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, source) {
			t.Fatalf("%s lost fields: %+v", format, got)
		}
		b, err = Export(&source, format, true)
		if err != nil {
			t.Fatal(err)
		}
		bindings := []string{"session-lead", "session-worker", "/work/agent-deck", "/work/worker", "owned_worktree"}
		for _, forbidden := range bindings {
			if strings.Contains(string(b), forbidden) {
				t.Fatalf("portable leaked %s", forbidden)
			}
		}
		if _, err = Import(b, ImportOptions{}); err == nil {
			t.Fatal("missing local binding accepted")
		}
		got, err = Import(b, ImportOptions{Repo: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		if got.Members[0].System != "Coordinate carefully" ||
			got.Members[0].Role != "master" ||
			got.Members[1].Capability != "inherited" ||
			len(got.Members[1].Permissions) != 0 {
			t.Fatal(got)
		}
		if source.Members[0].Session != "session-lead" || source.Members[1].Branch != "example/worker" {
			t.Fatal("export mutated source")
		}
	}
}

func TestLegacyFormatsAndValidation(t *testing.T) {
	masterWorkers := `{"server":"http://localhost:4096",
"master":{"name":"Lead","session":"session-lead","dir":"/tmp/project"},
"workers":[{"name":"Worker","session":"session-worker","dir":"/tmp/worker"}]}`
	for _, source := range []string{legacySessions, masterWorkers} {
		if _, err := Import([]byte(source), ImportOptions{Name: "Example"}); err == nil ||
			!strings.Contains(err.Error(), "legacy deck files are no longer supported") {
			t.Fatal("legacy deck did not fail clearly", err)
		}
	}
	for _, source := range []string{
		strings.Replace(legacySessions, "role: lead,", "", 1),
		strings.Replace(legacySessions, "name: Worker", "name: Lead", 1),
		strings.Replace(legacySessions, "name: Worker,", "name: Worker, role: lead,", 1),
		legacySessions + "---\n{}",
	} {
		if _, err := Import([]byte(source), ImportOptions{Name: "Example"}); err == nil {
			t.Fatalf("accepted invalid input %s", source)
		}
	}
	v, _ := Import([]byte(adoptedTeam), ImportOptions{Name: "Example"})
	v.Members[0].Capability = "typo"
	if err := v.Validate(); err == nil {
		t.Fatal("unknown capability accepted")
	}
	v.Members[0].Capability = "coder"
	v.Members[0].Permissions = Preset("coder")
	b, _ := Export(&v, "yaml", false)
	b = []byte(strings.Replace(string(b), "capability: coder", "capability: typo", 1))
	if _, err := Import(b, ImportOptions{}); err == nil {
		t.Fatal("import accepted unknown capability")
	}
}

func TestImportConflictAndExportOverwrite(t *testing.T) {
	v, _ := Import([]byte(adoptedTeam), ImportOptions{Name: "Example"})
	s := Store{Root: t.TempDir()}
	old, err := s.Save(t.Context(), &v, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(t.Context(), &v, nil); err == nil {
		t.Fatal("unconfirmed overwrite")
	}
	if !strings.Contains(Preview(&v, old), "--- Before") {
		t.Fatal("no replacement preview")
	}
	p := s.Root + "/export.json"
	if err := WriteExport(t.Context(), p, []byte("first"), false); err != nil {
		t.Fatal(err)
	}
	if err := WriteExport(t.Context(), p, []byte("second"), false); err == nil {
		t.Fatal("clobbered export")
	}
}
