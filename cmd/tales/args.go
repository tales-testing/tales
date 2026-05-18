package main

import (
	"strings"

	urfavecli "github.com/urfave/cli/v2"
)

// reorderArgs moves flags (and their values) ahead of the positional
// arguments of a subcommand. urfave/cli v2 parses flags with Go's
// stdlib flag.FlagSet, which stops at the first non-flag token, so
// `tales test ./path --tag X` silently drops --tag. By rewriting the
// argv slice we restore the intuitive behavior of placing flags
// anywhere on the command line.
//
// boolFlagsByCommand maps each subcommand name to the set of flags it
// declares as bool (and thus do not consume the next token as a value).
// Anything else is treated as taking a value. A literal "--" terminates
// flag parsing: every token that follows is kept verbatim as a
// positional argument.
func reorderArgs(args []string, boolFlagsByCommand map[string]map[string]struct{}) []string {
	if len(args) < 2 {
		return args
	}

	binary := args[0]
	rest := args[1:]

	// Find the subcommand: first token that does not start with "-".
	// Tokens that come before it (global flags / their values) are kept
	// in front in their original order.
	preCommand := []string{}

	i := 0
	for i < len(rest) && strings.HasPrefix(rest[i], "-") {
		preCommand = append(preCommand, rest[i])
		i++
	}

	if i >= len(rest) {
		return args
	}

	command := rest[i]
	i++

	boolFlags, isKnown := boolFlagsByCommand[command]
	if !isKnown {
		return args
	}

	flagsAndValues := []string{}
	positional := []string{}

	for i < len(rest) {
		token := rest[i]

		if token == "--" {
			positional = append(positional, rest[i:]...)

			break
		}

		if !strings.HasPrefix(token, "-") {
			positional = append(positional, token)
			i++

			continue
		}

		flagsAndValues = append(flagsAndValues, token)
		name := flagName(token)
		// "--flag=value" is self-contained.
		if strings.Contains(token, "=") {
			i++

			continue
		}
		// Bool flags do not take a value.
		if _, isBool := boolFlags[name]; isBool {
			i++

			continue
		}
		// Otherwise the next token (if any) is the flag's value.
		if i+1 < len(rest) {
			flagsAndValues = append(flagsAndValues, rest[i+1])
			i += 2

			continue
		}

		i++
	}

	out := make([]string, 0, len(args))
	out = append(out, binary)
	out = append(out, preCommand...)
	out = append(out, command)
	out = append(out, flagsAndValues...)
	out = append(out, positional...)

	return out
}

// flagName extracts the canonical flag name from a CLI token such as
// "--tag", "-t" or "--tag=value", returning "tag" / "t" respectively.
func flagName(token string) string {
	name := strings.TrimLeft(token, "-")
	if idx := strings.Index(name, "="); idx >= 0 {
		return name[:idx]
	}

	return name
}

// collectBoolFlags walks each registered subcommand and returns a map
// of <command name> → set of bool flag names (including aliases). It
// powers reorderArgs so we never accidentally swallow a positional
// argument that follows a bool flag.
func collectBoolFlags(app *urfavecli.App) map[string]map[string]struct{} {
	out := map[string]map[string]struct{}{}

	for _, cmd := range app.Commands {
		bools := map[string]struct{}{}

		for _, f := range cmd.Flags {
			bf, ok := f.(*urfavecli.BoolFlag)
			if !ok {
				continue
			}

			for _, name := range bf.Names() {
				bools[name] = struct{}{}
			}
		}

		out[cmd.Name] = bools

		for _, alias := range cmd.Aliases {
			out[alias] = bools
		}
	}

	return out
}
