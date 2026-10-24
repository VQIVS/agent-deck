package team

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// CommandAllowlist matches entire literal argv lists, never command prefixes or patterns.
type CommandAllowlist struct {
	commands [][]string
}

// ParseLiteralCommand accepts plain words and quoted literals, with no shell expansion or operators.
func ParseLiteralCommand(command string) ([]string, error) {
	if strings.ContainsAny(command, "\x00\r\n;&|<>$`(){}[]*?~#!\\") {
		return nil, fmt.Errorf("command contains shell syntax")
	}
	var args []string
	var word strings.Builder
	var quote rune
	started := false
	for _, r := range command {
		if invalidCommandRune(r) {
			return nil, fmt.Errorf("command contains control characters")
		}
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, started = r, true
		case r == ' ' || r == '\t':
			if started {
				args = append(args, word.String())
				word.Reset()
				started = false
			}
		default:
			word.WriteRune(r)
			started = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("command has an unterminated quote")
	}
	if started {
		args = append(args, word.String())
	}
	if !validExecutable(args) {
		return nil, fmt.Errorf("command needs a bare executable name")
	}
	return args, nil
}

func validExecutable(args []string) bool {
	return len(args) > 0 && args[0] != "" && !strings.ContainsAny(args[0], "/=:")
}

func invalidCommandRune(r rune) bool {
	return unicode.IsControl(r) && r != '\t'
}

func NewCommandAllowlist(commands []string) (CommandAllowlist, error) {
	list := CommandAllowlist{}
	for _, command := range commands {
		args, err := ParseLiteralCommand(command)
		if err != nil {
			return CommandAllowlist{}, fmt.Errorf("allowlist command %q: %w", command, err)
		}
		list.commands = append(list.commands, args)
	}
	return list, nil
}

func (a CommandAllowlist) Allows(command string) bool {
	args, err := ParseLiteralCommand(command)
	if err != nil {
		return false
	}
	for _, allowed := range a.commands {
		if slices.Equal(args, allowed) {
			return true
		}
	}
	return false
}

// AllowsRequest requires complete command strings; truncated permission patterns must not be supplied.
func (a CommandAllowlist) AllowsRequest(permission string, commands []string, readOnly bool) bool {
	if readOnly || permission != "bash" || len(commands) == 0 {
		return false
	}
	for _, command := range commands {
		if !a.Allows(command) {
			return false
		}
	}
	return true
}
