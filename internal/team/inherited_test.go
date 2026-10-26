package team

import (
	"context"
	"strings"
	"testing"
)

func TestInheritedPermissions(t *testing.T) {
	v, err := Import([]byte(adoptedTeam), ImportOptions{Name: "Example"})
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"yaml", "json"} {
		b, err := Export(&v, format, false)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "permissions") || !strings.Contains(string(b), "inherited from session") {
			t.Fatal(string(b))
		}
		got, err := Import(b, ImportOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got.Members[0].Capability != "inherited" || len(got.Members[0].Permissions) != 0 {
			t.Fatal(got)
		}
	}
	v.Members[0].Permissions = Preset("full")
	if err := v.Validate(); err == nil {
		t.Fatal("inherited with recorded rules accepted")
	}
	v.Members[0].Permissions = nil
	v.Members[0].Session = ""
	v.Members[0].Mode = "shared"
	m := Manager{Team: v}
	if err := m.Start(context.Background(), 0); err == nil ||
		!strings.Contains(err.Error(), "explicit creation permissions") {
		t.Fatal(err)
	}
	v.Members[0].Mode = "adopt"
	v.Members[0].Session = "session-lead"
	v.Members[0].Capability = "coder"
	v.Members[0].Permissions = Preset("coder")
	if err := v.Validate(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(v.Members[0].PermissionSummary(), "applies only to newly created sessions") {
		t.Fatal(v.Members[0].PermissionSummary())
	}
}
