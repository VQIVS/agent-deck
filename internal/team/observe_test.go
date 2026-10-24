package team

import (
	"testing"

	api "github.com/qivs/agent-deck/internal/opencode"
)

func TestContextAssistantFallsBackWhileBusy(t *testing.T) {
	messages := make([]api.Message, 2)
	messages[0].Info.Role = "assistant"
	messages[0].Info.ID = "completed"
	messages[0].Info.Time.Created = 1
	messages[0].Info.Time.Completed = 2
	messages[0].Info.Tokens.Input = 42
	messages[1].Info.Role = "assistant"
	messages[1].Info.Time.Created = 3
	if got := ContextAssistant(messages); got == nil || got.Info.ID != "completed" {
		t.Fatal("busy message erased known context")
	}
	messages[1].Info.Tokens.Input = 90
	if got := ContextAssistant(messages); got.Info.Tokens.Input != 90 {
		t.Fatal("new usage ignored")
	}
}
