package team

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBuiltinRoleTemplates(t *testing.T) {
	templates := BuiltinRoleTemplates()
	if len(templates) != 4 {
		t.Fatal("missing built-ins")
	}
	for _, template := range templates {
		if err := template.Validate(); err != nil {
			t.Fatalf("%s: %v", template.Name, err)
		}
		for _, role := range template.Members {
			if role.System == "" || role.Brief == "" {
				t.Fatalf("missing role guidance: %s/%s", template.Name, role.Name)
			}
		}
	}
	templates[0].Members[0].Name = "changed"
	if BuiltinRoleTemplates()[0].Members[0].Name != "master" {
		t.Fatal("built-in calls share mutable state")
	}
}

func TestLoadTemplateOverride(t *testing.T) {
	dir := t.TempDir()
	override := BuiltinRoleTemplates()[3]
	override.Description = "User custom recipe"
	b, err := yaml.Marshal(override)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "custom.yaml"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	templates, err := LoadRoleTemplates(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(templates) != 4 || templates[1].Name != "custom" || templates[1].Description != override.Description {
		t.Fatalf("override did not replace built-in: %+v", templates)
	}
	if err = os.WriteFile(filepath.Join(dir, "duplicate.yml"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadRoleTemplates(context.Background(), dir); err == nil {
		t.Fatal("duplicate user names accepted")
	}
	if _, err = LoadRoleTemplates(context.Background(), filepath.Join(dir, "missing")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = LoadRoleTemplates(ctx, filepath.Join(dir, "missing")); err == nil {
		t.Fatal("canceled load succeeded")
	}
}

func TestTemplateStrictSchema(t *testing.T) {
	for _, data := range []string{
		"version: 2\nname: custom\ndescription: test\nmembers: []",
		"version: 1\nname: custom\ndescription: test\nmembers: []",
		"version: 1\nname: custom\ndescription: test\nunknown: value",
		"version: 1\nname: custom\ndescription: test\n---\nname: extra",
	} {
		if _, err := ParseRoleTemplate([]byte(data)); err == nil {
			t.Errorf("invalid template accepted: %s", data)
		}
	}
}
