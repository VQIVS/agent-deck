package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/vqivs/agent-deck/internal/team"
)

func (m *Model) roleTemplateNames() []string {
	templates, err := team.LoadRoleTemplates(m.context(), filepath.Join(filepath.Dir(team.DefaultRoot()), "templates"))
	if err != nil {
		m.note = err.Error()
		return nil
	}
	names := []string{""}
	for _, template := range templates {
		for _, role := range template.Members {
			if role.Role == "worker" {
				names = append(names, template.Name+"/"+role.Name)
			}
		}
	}
	return names
}

func (m *Model) applyRoleTemplate(values map[string]string) error {
	choice := values["Role template"]
	if choice == "" {
		return nil
	}
	templates, err := team.LoadRoleTemplates(m.context(), filepath.Join(filepath.Dir(team.DefaultRoot()), "templates"))
	if err != nil {
		return fmt.Errorf("load role template: %w", err)
	}
	name, roleName, _ := strings.Cut(choice, "/")
	for _, template := range templates {
		for _, role := range template.Members {
			if template.Name != name || role.Name != roleName || role.Role != "worker" {
				continue
			}
			values["Permission preset"], values["System prompt"] = role.Capability, role.System
			if values["Brief"] == "" {
				values["Brief"] = role.Brief
			}
			if role.Agent != "" {
				values["OpenCode agent"] = role.Agent
			}
			if role.Model != "" {
				values["Model"] = role.Model
			}
			return nil
		}
	}
	return fmt.Errorf("unknown worker role template %q", choice)
}
