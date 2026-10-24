package ui

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/vqivs/agent-deck/internal/team"
)

// ParseTeamDoD accepts one strict YAML criterion list; blank input clears the optional team policy.
func ParseTeamDoD(text string) (team.DefinitionOfDone, error) {
	if len(text) > 256<<10 {
		return nil, fmt.Errorf("team definition of done exceeds 256 KiB")
	}
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	var definition team.DefinitionOfDone
	decoder := yaml.NewDecoder(strings.NewReader(text))
	decoder.KnownFields(true)
	if err := decoder.Decode(&definition); err != nil {
		return nil, fmt.Errorf("parse team definition of done: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("team definition of done needs one YAML document")
	}
	if len(definition) == 0 {
		return nil, nil
	}
	if err := definition.Validate(); err != nil {
		return nil, fmt.Errorf("team definition of done: %w", err)
	}
	return definition, nil
}

func FormatTeamDoD(definition team.DefinitionOfDone) (string, error) {
	if len(definition) == 0 {
		return "", nil
	}
	if err := definition.Validate(); err != nil {
		return "", fmt.Errorf("format team definition of done: %w", err)
	}
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(definition); err != nil {
		return "", fmt.Errorf("encode team definition of done: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return "", fmt.Errorf("close team definition of done YAML: %w", err)
	}
	return buffer.String(), nil
}

// ApplyTeamDoDForm validates before changing the draft, preserving it on invalid/read-only submissions.
func ApplyTeamDoDForm(draft *team.Team, text string, readOnly bool) error {
	if draft == nil || readOnly {
		return fmt.Errorf("team definition of done editing is unavailable")
	}
	definition, err := ParseTeamDoD(text)
	if err != nil {
		return err
	}
	draft.DefinitionOfDone = definition
	return nil
}
