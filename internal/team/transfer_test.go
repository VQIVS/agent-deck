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

func TestTransferRoundTrip(t *testing.T) {
	source, err := Import([]byte(legacySessions), ImportOptions{Name: "Example"})
	if err != nil {
		t.Fatal(err)
	}
	source.Members[0].System = "Coordinate carefully"
	source.Members[0].Model = "provider/model"
	source.Members[1].Owned = true
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
		bindings := []string{"session-lead", "session-worker", "/tmp/project", "/tmp/worker", "owned_worktree"}
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
		if got.Members[0].Model != "provider/model" || got.Members[0].System != "Coordinate carefully" ||
			got.Members[0].Role != "master" ||
			got.Members[1].Capability != "inherited" ||
			len(got.Members[1].Permissions) != 0 {
			t.Fatal(got)
		}
		if source.Members[0].Session != "session-lead" || !source.Members[1].Owned {
			t.Fatal("export mutated source")
		}
	}
}

func TestLegacyFormatsAndValidation(t *testing.T) {
	masterWorkers := `{"server":"http://localhost:4096",
"master":{"name":"Lead","session":"session-lead","dir":"/tmp/project"},
"workers":[{"name":"Worker","session":"session-worker","dir":"/tmp/worker"}]}`
	for _, source := range []string{legacySessions, masterWorkers} {
		v, err := Import([]byte(source), ImportOptions{Name: "Example"})
		if err != nil {
			t.Fatal(err)
		}
		if len(v.Members) != 2 || v.Members[0].Mode != "adopt" || v.Members[0].Role != "master" || v.Members[0].Owned {
			t.Fatal(v)
		}
		for _, member := range v.Members {
			if member.Capability != "inherited" || len(member.Permissions) != 0 {
				t.Fatal("adoption invented rules", member)
			}
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
	v, _ := Import([]byte(legacySessions), ImportOptions{Name: "Example"})
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
	v, _ := Import([]byte(legacySessions), ImportOptions{Name: "Example"})
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
