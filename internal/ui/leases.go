package ui

import "strings"

func parseLeases(text string) []string {
	var leases []string
	for _, line := range strings.Split(text, "\n") {
		if value := strings.TrimSpace(line); value != "" {
			leases = append(leases, value)
		}
	}
	return leases
}

func (m *Model) leasedName(index int) string {
	agent := m.panes[index].Agent
	if task := m.activeTask(agent.Name); task != "" {
		agent.Name += " · " + task
	}
	if leases := m.memberLeases(agent.Session); leases != "" {
		return agent.Name + " · " + leases
	}
	return agent.Name
}

func (m *Model) memberLeases(session string) string {
	if m.teams == nil || m.teams.Manager == nil {
		return ""
	}
	for i := range m.teams.Manager.Team.Members {
		member := &m.teams.Manager.Team.Members[i]
		if member.Session == session {
			return strings.TrimSpace(strings.Join(member.Leases, ", ") + " " + m.teams.Manager.Team.LeaseStatus(member))
		}
	}
	return ""
}
