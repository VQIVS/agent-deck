package team

import (
	"context"
	"fmt"
)

// EstimationHistoryNotices preserves the committed mutation and normal notices on
// history failures. The warning is observable; later mutations/reports repair history.
func EstimationHistoryNotices(ctx context.Context, store BoardStore, board *TaskBoard, t *Team) []TaskNotice {
	if err := RecordEstimationHistory(ctx, store, board); err != nil {
		detail := err.Error()
		if len(detail) > 500 {
			detail = detail[:500]
		}
		return []TaskNotice{{To: teamMaster(t), Task: "estimation-history", Text: fmt.Sprintf(
			"Task change saved; sizing history recording failed and will retry on the next observation: %s", detail)}}
	}
	return nil
}
