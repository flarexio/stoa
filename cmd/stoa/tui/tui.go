// Package tui is the Bubble Tea conversational front-end for Stoa. It is
// pure presentation: it renders the reason -> validate -> execute cycle
// from harness/loop cycle events and never composes agents itself.
//
// cmd/stoa builds the agents and injects them as Options, so this package
// imports neither cmd/stoa nor the bookkeeper / npc / accounting / world
// packages. The Bubble Tea dependency is confined here.
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/flarexio/stoa/harness/loop"
)

// Session is one composed agent the TUI runs user turns against. A
// cmd/stoa Option.Start produces it; the TUI never builds one itself.
type Session interface {
	// Run executes a single user turn, streaming cycle events to sink as
	// they happen. It returns once the turn is complete.
	Run(ctx context.Context, request string, sink loop.EventSink) (Outcome, error)
	// Close releases the session's resources.
	Close() error
}

// Outcome is the non-event summary of a completed turn.
type Outcome struct {
	Turns   int
	Summary string // one-line human summary of the final state; may be empty
}

// Option is one selectable agent + scenario pairing shown on the start
// screen. Start composes the underlying agent lazily, when the user picks
// the option, so opening the TUI does not seed every scenario up front.
type Option struct {
	Label string
	Hint  string // short secondary description, e.g. the scenario file
	Start func(ctx context.Context) (Session, error)
}

// Run starts the Bubble Tea program with the given selectable options and
// blocks until the user quits. It returns an error if no options are
// supplied or if the program itself fails.
func Run(ctx context.Context, options []Option) error {
	if len(options) == 0 {
		return errNoOptions
	}
	p := tea.NewProgram(newModel(ctx, options), tea.WithAltScreen(), tea.WithContext(ctx))
	_, err := p.Run()
	return err
}
