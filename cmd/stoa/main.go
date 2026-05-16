// Command stoa is the demo CLI for Stoa's vertical slices. Each subcommand
// wires a scenario JSON file through the same reason → validate → execute
// loop the underlying package uses in tests, with a deterministic scripted
// reasoning engine so no live LLM provider is needed.
package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/urfave/cli/v3"
)

func main() {
	app := newApp(os.Stdout, os.Stderr)
	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newApp(stdout, stderr io.Writer) *cli.Command {
	return &cli.Command{
		Name:      "stoa",
		Usage:     "demo CLI for the Stoa harness",
		Writer:    stdout,
		ErrWriter: stderr,
		Commands: []*cli.Command{
			newNPCRunCommand(stdout),
			newBookRunCommand(stdout),
			newTUICommand(),
		},
	}
}
