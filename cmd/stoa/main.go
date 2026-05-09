// Command stoa is the demo CLI for Stoa's NPC harness. It is intentionally
// small: a single subcommand wires a scenario JSON file through the same
// reason → validate → execute loop the npc package uses in tests, with a
// deterministic scripted reasoning engine so no live LLM provider is needed.
package main

import (
	"context"
	"fmt"
	"os"
)

const usage = `stoa - demo CLI for the Stoa NPC harness

Usage:
  stoa <command> [arguments]

Commands:
  npc-run    Run an NPC reasoning loop against a scenario JSON file.

Run "stoa <command> -h" for help on a specific command.`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}

	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "npc-run":
		if err := runNPC(context.Background(), args, os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "-h", "--help", "help":
		fmt.Fprintln(os.Stdout, usage)
	default:
		fmt.Fprintf(os.Stderr, "stoa: unknown command %q\n\n%s\n", cmd, usage)
		os.Exit(2)
	}
}
