package ui

import (
	"testing"
)

func TestWorkerRoleTemplateAppliesCreationDefaults(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := requestModel("http://unused", false)
	values := map[string]string{"Role template": "review-pair/reviewer"}
	if err := m.applyRoleTemplate(values); err != nil {
		t.Fatal(err)
	}
	if values["Permission preset"] != "reviewer" || values["System prompt"] == "" || values["Brief"] == "" {
		t.Fatal(values)
	}
}
