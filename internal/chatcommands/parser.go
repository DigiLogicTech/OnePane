package chatcommands

import (
	"errors"
	"strings"
	"unicode"
)

var (
	ErrNotCommand     = errors.New("not a slash command")
	ErrUnknownCommand = errors.New("unknown slash command")
	ErrUnclosedQuote  = errors.New("unclosed quote")
)

func Parse(input string) (ParsedCommand, error) {
	raw := strings.TrimSpace(input)
	if !strings.HasPrefix(raw, "/") {
		return ParsedCommand{}, ErrNotCommand
	}
	parts, err := splitArgs(strings.TrimSpace(strings.TrimPrefix(raw, "/")))
	if err != nil {
		return ParsedCommand{}, err
	}
	if len(parts) == 0 {
		return ParsedCommand{Name: "help", Raw: raw}, nil
	}
	name := strings.ToLower(parts[0])
	spec, ok := Lookup(name)
	if !ok {
		return ParsedCommand{}, ErrUnknownCommand
	}
	return ParsedCommand{Name: spec.Name, Args: parts[1:], Raw: raw}, nil
}

func splitArgs(s string) ([]string, error) {
	var out []string
	var b strings.Builder
	var quote rune
	escaped := false
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for _, r := range s {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if unicode.IsSpace(r) {
			flush()
			continue
		}
		b.WriteRune(r)
	}
	if escaped {
		b.WriteRune('\\')
	}
	if quote != 0 {
		return nil, ErrUnclosedQuote
	}
	flush()
	return out, nil
}
