package axscript

import (
	"fmt"
	"strconv"
	"strings"
)

// tokenSpan is one token together with the byte offset of its first raw
// character in the original command line (an opening quote included). The raw
// tail is needed because Tokenize unquotes values: a trailing free-form
// argument such as `run ... "-p" "a b"` must keep its quoting when it is
// forwarded verbatim to a shell-like consumer.
type tokenSpan struct {
	Value string
	Start int
}

// Tokenize splits a command line into unquoted tokens.
func Tokenize(cmdline string) []string {
	spans := TokenizeWithSpans(cmdline)
	tokens := make([]string, len(spans))
	for i, span := range spans {
		tokens[i] = span.Value
	}
	return tokens
}

// TokenizeWithSpans is Tokenize that also records where each token started in
// the raw input.
func TokenizeWithSpans(cmdline string) []tokenSpan {
	var spans []tokenSpan
	var token strings.Builder
	start := -1
	inQuotes := false
	runes := []rune(cmdline)
	length := len(runes)

	// runeOffsets[i] is the byte offset of rune i in the raw string.
	runeOffsets := make([]int, length+1)
	{
		pos := 0
		for i, r := range runes {
			runeOffsets[i] = pos
			pos += len(string(r))
		}
		runeOffsets[length] = pos
	}

	for i := 0; i < length; {
		c := runes[i]

		if c == ' ' && !inQuotes {
			if token.Len() > 0 {
				spans = append(spans, tokenSpan{Value: token.String(), Start: start})
				token.Reset()
				start = -1
			}
			i++
			continue
		}

		if start < 0 {
			// Record where the token begins; an opening quote counts as the
			// start so a raw tail keeps the quoting style.
			start = runeOffsets[i]
		}

		if c == '"' {
			inQuotes = !inQuotes
			i++
			continue
		}

		if c == '\\' {
			numBS := 0
			for i < length && runes[i] == '\\' {
				numBS++
				i++
			}
			if i < length && runes[i] == '"' {
				for j := 0; j < numBS/2; j++ {
					token.WriteRune('\\')
				}
				if numBS%2 == 0 {
					inQuotes = !inQuotes
				} else {
					token.WriteRune('"')
				}
				i++
			} else {
				for j := 0; j < numBS; j++ {
					token.WriteRune('\\')
				}
			}
			continue
		}

		token.WriteRune(c)
		i++
	}

	if token.Len() > 0 {
		spans = append(spans, tokenSpan{Value: token.String(), Start: start})
	}

	return spans
}

func ParseCommand(cmdline string, resolved *ResolvedCommand) (*ParsedCommand, error) {
	spans := TokenizeWithSpans(cmdline)
	if len(spans) == 0 {
		return nil, fmt.Errorf("empty command line")
	}

	tokens := make([]string, len(spans))
	for i, span := range spans {
		tokens[i] = span.Value
	}

	result := &ParsedCommand{
		Args: make(map[string]interface{}),
	}

	commandName := tokens[0]
	result.CommandName = commandName
	result.Args["command"] = commandName

	var cmdDef *CommandDef
	argTokens := tokens[1:]
	tokenShift := 1

	if resolved.Subcommand != nil {
		if len(tokens) < 2 {
			return nil, fmt.Errorf("subcommand required for '%s'", commandName)
		}
		result.SubcommandName = tokens[1]
		result.Args["subcommand"] = tokens[1]
		cmdDef = resolved.Subcommand
		argTokens = tokens[2:]
		tokenShift = 2
	} else {
		cmdDef = resolved.Command
	}

	parsedArgsMap := make(map[string]string)
	var lastKey string
	lastKeyStart := -1

	for i := 0; i < len(argTokens); i++ {
		arg := argTokens[i]
		isWideArgs := true

		for _, commandArg := range cmdDef.Args {
			if commandArg.Flag {
				if commandArg.Type == ArgTypeBool && commandArg.Mark == arg {
					parsedArgsMap[commandArg.Mark] = "true"
					lastKey = commandArg.Mark
					lastKeyStart = -1
					isWideArgs = false
					break
				} else if commandArg.Mark == arg && i+1 < len(argTokens) {
					i++
					parsedArgsMap[commandArg.Name] = argTokens[i]
					lastKey = commandArg.Name
					lastKeyStart = -1
					if absVal := i + tokenShift; absVal < len(spans) {
						lastKeyStart = spans[absVal].Start
					}
					isWideArgs = false
					break
				}
			} else if _, exists := parsedArgsMap[commandArg.Name]; !exists {
				parsedArgsMap[commandArg.Name] = arg
				lastKey = commandArg.Name
				lastKeyStart = -1
				if absPos := i + tokenShift; absPos < len(spans) {
					lastKeyStart = spans[absPos].Start
				}
				isWideArgs = false
				break
			}
		}

		if isWideArgs && lastKey != "" {
			// The remainder is a free-form argument (e.g. `run` args or a cron
			// schedule). Replace the accumulated value with the RAW tail
			// starting at lastKey's first token: Tokenize already unquoted the
			// tokens, and consumers like the agent's shlex.Split need the
			// quotes back to keep values containing spaces in one piece.
			start := lastKeyStart
			if start < 0 {
				if abs := i + tokenShift; abs < len(spans) {
					start = spans[abs].Start
				}
			}
			if start >= 0 && start < len(cmdline) {
				parsedArgsMap[lastKey] = strings.TrimRight(cmdline[start:], " ")
			}
			break
		}
	}

	for _, commandArg := range cmdDef.Args {
		nameKey := commandArg.Name
		if commandArg.Type == ArgTypeBool {
			nameKey = commandArg.Mark
		}

		if val, exists := parsedArgsMap[nameKey]; exists {
			switch commandArg.Type {
			case ArgTypeString:
				result.Args[commandArg.Name] = val
			case ArgTypeInt:
				intVal, err := strconv.Atoi(strings.TrimSpace(val))
				if err != nil {
					return nil, fmt.Errorf("invalid integer value for argument '%s': %s", commandArg.Name, val)
				}
				result.Args[commandArg.Name] = intVal
			case ArgTypeBool:
				result.Args[commandArg.Mark] = val == "true"
			case ArgTypeFile:
				result.FileArgs = append(result.FileArgs, FileArgInfo{
					ArgName:      commandArg.Name,
					OriginalPath: val,
					Required:     commandArg.Required,
				})
				result.Args[commandArg.Name] = nil
			}
		} else if commandArg.Required {
			if commandArg.DefaultValue == nil && !commandArg.DefaultUsed {
				return nil, fmt.Errorf("missing required argument: %s", commandArg.Name)
			}
			switch commandArg.Type {
			case ArgTypeString:
				if s, ok := commandArg.DefaultValue.(string); ok {
					result.Args[commandArg.Name] = s
				} else {
					return nil, fmt.Errorf("missing required argument: %s", commandArg.Name)
				}
			case ArgTypeInt:
				switch v := commandArg.DefaultValue.(type) {
				case int:
					result.Args[commandArg.Name] = v
				case int64:
					result.Args[commandArg.Name] = int(v)
				case float64:
					result.Args[commandArg.Name] = int(v)
				default:
					return nil, fmt.Errorf("missing required argument: %s", commandArg.Name)
				}
			case ArgTypeBool:
				if b, ok := commandArg.DefaultValue.(bool); ok {
					result.Args[commandArg.Mark] = b
				} else {
					return nil, fmt.Errorf("missing required argument: %s", commandArg.Name)
				}
			}
		}
	}

	msg := cmdDef.Message
	if msg != "" {
		for k, v := range parsedArgsMap {
			param := "<" + k + ">"
			if strings.Contains(msg, param) {
				msg = strings.ReplaceAll(msg, param, v)
			}
		}
		result.Message = msg
		result.Args["message"] = msg
	}

	return result, nil
}
