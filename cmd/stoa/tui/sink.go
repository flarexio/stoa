package tui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/flarexio/stoa/llm"
)

var errNoOptions = errors.New("tui: no agent/scenario options to choose from")

// eventMsg carries one cycle event from a running turn into the Bubble
// Tea update loop.
type eventMsg llm.CycleEvent

// turnDoneMsg signals that the running turn's goroutine has returned.
type turnDoneMsg struct {
	outcome Outcome
	err     error
}

// chanSink is the loop.EventSink the TUI hands to Session.Run. It forwards
// each cycle event onto a channel the Bubble Tea command drains. A blocked
// send is released by ctx cancellation, so quitting or cancelling a turn
// never leaks the agent goroutine.
type chanSink struct {
	events chan<- llm.CycleEvent
}

func (s chanSink) Emit(ctx context.Context, event llm.CycleEvent) error {
	select {
	case s.events <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// waitForTurn blocks on the next cycle event. When the events channel is
// closed the turn goroutine has finished, and its result is already
// waiting on done.
func waitForTurn(events <-chan llm.CycleEvent, done <-chan turnDoneMsg) tea.Cmd {
	return func() tea.Msg {
		if ev, ok := <-events; ok {
			return eventMsg(ev)
		}
		return <-done
	}
}
