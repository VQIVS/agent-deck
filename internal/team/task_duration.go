package team

import (
	"encoding/json"
	"fmt"
	"time"
)

func (e TaskEstimate) MarshalJSON() ([]byte, error) {
	type alias TaskEstimate
	data, err := json.Marshal(struct {
		Duration string `json:"duration"`
		alias
	}{Duration: e.Duration.String(), alias: alias(e)})
	if err != nil {
		return nil, fmt.Errorf("encode task estimate: %w", err)
	}
	return data, nil
}

func (e *TaskEstimate) UnmarshalJSON(data []byte) error {
	type alias TaskEstimate
	var value struct {
		Duration json.RawMessage `json:"duration"`
		*alias
	}
	value.alias = (*alias)(e)
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("decode task estimate: %w", err)
	}
	if len(value.Duration) == 0 {
		return nil
	}
	var duration string
	if json.Unmarshal(value.Duration, &duration) == nil {
		parsed, err := time.ParseDuration(duration)
		if err != nil {
			return fmt.Errorf("parse estimate duration: %w", err)
		}
		e.Duration = parsed
		return nil
	}
	if err := json.Unmarshal(value.Duration, &e.Duration); err != nil {
		return fmt.Errorf("decode legacy estimate duration: %w", err)
	}
	return nil
}
