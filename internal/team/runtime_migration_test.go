package team

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRuntimeMigrationOnlyEnabledTeam(t *testing.T) {
	store := Store{Root: t.TempDir()}
	for _, enabled := range []bool{false, true} {
		tm := example()
		tm.LeasePool = enabled
		data, err := yaml.Marshal(tm)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(store.Path(tm.Name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		loaded, _, err := store.LoadRuntimeTeam(t.Context(), tm.Name)
		if err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(store.Path(tm.Name))
		if err != nil {
			t.Fatal(err)
		}
		if enabled && loaded.Members[0].ID == "" {
			t.Fatal("runtime not migrated")
		}
		if !enabled && !bytes.Equal(after, data) {
			t.Fatal("unrelated team rewritten")
		}
	}
}

func TestImportStripsExportedMembershipIDs(t *testing.T) {
	tm := example()
	tm.Members[0].ID = "old-membership"
	data, err := Export(&tm, "yaml", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "old-membership") {
		t.Fatal("fixture missing ID")
	}
	imported, err := Import(data, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if imported.Members[0].ID != "" {
		t.Fatal("import retained membership")
	}
	if _, err := (Store{Root: t.TempDir()}).Save(t.Context(), &imported, nil); err != nil {
		t.Fatal(err)
	}
}
