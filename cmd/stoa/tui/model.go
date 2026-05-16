package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/flarexio/stoa/llm"
)

// viewState is the screen the model is currently showing.
type viewState int

const (
	stateSelect viewState = iota // choosing an agent + scenario
	stateChat                    // conversing with a chosen session
)

// lineKind classifies one transcript entry for styling.
type lineKind int

const (
	lineUser lineKind = iota
	lineModel
	lineValidation
	lineExecution
	lineObservation
	lineSystem
	lineError
)

// line is one rendered entry in the chat transcript.
type line struct {
	kind lineKind
	text string
}

// sessionReadyMsg is delivered after an Option.Start finishes composing
// (or failing to compose) a session.
type sessionReadyMsg struct {
	label   string
	session Session
	err     error
}

type model struct {
	ctx     context.Context
	options []Option

	state  viewState
	cursor int // selection index on the start screen

	session Session
	label   string

	viewport viewport.Model
	input    textinput.Model
	spinner  spinner.Model

	lines   []line
	running bool
	cancel  context.CancelFunc
	events  chan llm.CycleEvent
	done    chan turnDoneMsg
	turn    int

	width  int
	height int
	ready  bool
	err    error
}

func newModel(ctx context.Context, options []Option) model {
	ti := textinput.New()
	ti.Placeholder = "Type a request and press Enter"
	ti.Prompt = "> "

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	return model{
		ctx:      ctx,
		options:  options,
		state:    stateSelect,
		input:    ti,
		spinner:  sp,
		viewport: viewport.New(0, 0),
	}
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		m.layout()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case sessionReadyMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.session = msg.session
		m.label = msg.label
		m.state = stateChat
		m.lines = nil
		m.turn = 0
		m.input.Focus()
		m.layout()
		return m, textinput.Blink

	case eventMsg:
		m.appendEvent(llm.CycleEvent(msg))
		return m, waitForTurn(m.events, m.done)

	case turnDoneMsg:
		return m.finishTurn(msg)

	case spinner.TickMsg:
		if !m.running {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	if m.state == stateChat && !m.running {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		if m.running && m.cancel != nil {
			m.cancel() // cancel the in-flight turn, but keep the program open
			return m, nil
		}
		return m, tea.Quit
	}

	switch m.state {
	case stateSelect:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.options)-1 {
				m.cursor++
			}
		case "enter":
			return m, m.startSession(m.options[m.cursor])
		case "q", "esc":
			return m, tea.Quit
		}
		return m, nil

	case stateChat:
		switch msg.String() {
		case "esc":
			if m.running {
				return m, nil
			}
			if m.session != nil {
				_ = m.session.Close()
				m.session = nil
			}
			m.state = stateSelect
			return m, nil
		case "pgup", "pgdown", "ctrl+u", "ctrl+d":
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case "enter":
			if m.running {
				return m, nil
			}
			request := strings.TrimSpace(m.input.Value())
			if request == "" {
				return m, nil
			}
			m.input.Reset()
			return m.startTurn(request)
		}
		if !m.running {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		return m, nil
	}
	return m, nil
}

// startSession composes the chosen option's session off the update loop,
// since Start may load scenarios and open repositories.
func (m model) startSession(opt Option) tea.Cmd {
	ctx := m.ctx
	return func() tea.Msg {
		session, err := opt.Start(ctx)
		return sessionReadyMsg{label: opt.Label, session: session, err: err}
	}
}

// startTurn runs one user request against the session. The agent runs in
// a goroutine; its cycle events stream back through chanSink.
func (m model) startTurn(request string) (tea.Model, tea.Cmd) {
	m.turn++
	m.appendLine(line{kind: lineUser, text: request})

	turnCtx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel

	events := make(chan llm.CycleEvent)
	done := make(chan turnDoneMsg, 1)
	m.events = events
	m.done = done
	m.running = true

	session := m.session
	go func() {
		outcome, err := session.Run(turnCtx, request, chanSink{events: events})
		done <- turnDoneMsg{outcome: outcome, err: err}
		close(events)
	}()

	return m, tea.Batch(waitForTurn(events, done), m.spinner.Tick)
}

func (m model) finishTurn(msg turnDoneMsg) (tea.Model, tea.Cmd) {
	m.running = false
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.events = nil
	m.done = nil

	switch {
	case msg.err != nil && errors.Is(msg.err, context.Canceled):
		m.appendLine(line{kind: lineSystem, text: "turn cancelled"})
	case msg.err != nil:
		m.appendLine(line{kind: lineError, text: msg.err.Error()})
	case msg.outcome.Summary != "":
		m.appendLine(line{kind: lineSystem, text: fmt.Sprintf("done in %d turn(s) — %s", msg.outcome.Turns, msg.outcome.Summary)})
	default:
		m.appendLine(line{kind: lineSystem, text: fmt.Sprintf("done in %d turn(s)", msg.outcome.Turns)})
	}
	return m, nil
}

func (m *model) appendEvent(ev llm.CycleEvent) {
	m.appendLine(line{kind: eventLineKind(ev.Kind), text: strings.TrimSpace(ev.Content)})
}

func (m *model) appendLine(l line) {
	m.lines = append(m.lines, l)
	m.viewport.SetContent(m.renderTranscript())
	m.viewport.GotoBottom()
}

func (m *model) layout() {
	if !m.ready {
		return
	}
	m.input.Width = max(m.width-6, 10)
	// header (1) + blank (1) + status (1) + input (1) + footer (1) + margins.
	h := max(m.height-7, 3)
	m.viewport.Width = m.width
	m.viewport.Height = h
	m.viewport.SetContent(m.renderTranscript())
	m.viewport.GotoBottom()
}

func eventLineKind(k llm.EventKind) lineKind {
	switch k {
	case llm.EventModelOutput:
		return lineModel
	case llm.EventValidationError:
		return lineValidation
	case llm.EventExecutionError:
		return lineExecution
	case llm.EventObservation:
		return lineObservation
	default:
		return lineSystem
	}
}

func lineMeta(k lineKind) (string, lipgloss.Style) {
	switch k {
	case lineUser:
		return "you", userStyle
	case lineModel:
		return "model", modelStyle
	case lineValidation:
		return "rejected", validationStyle
	case lineExecution:
		return "exec error", executionStyle
	case lineObservation:
		return "observation", observationStyle
	case lineError:
		return "error", errorStyle
	default:
		return "·", systemStyle
	}
}

func (m model) renderTranscript() string {
	if len(m.lines) == 0 {
		return hintStyle.Render("No turns yet. Type a request below to start.")
	}
	width := max(m.viewport.Width-2, 20)
	var b strings.Builder
	for i, l := range m.lines {
		if i > 0 {
			b.WriteString("\n\n")
		}
		label, style := lineMeta(l.kind)
		b.WriteString(style.Render(label))
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Width(width).Render(l.text))
	}
	return b.String()
}

func (m model) View() string {
	if !m.ready {
		return "Starting Stoa TUI…"
	}
	if m.err != nil {
		return errorStyle.Render("error: "+m.err.Error()) + "\n\n" +
			footerStyle.Render("ctrl+c quit")
	}
	switch m.state {
	case stateSelect:
		return m.selectView()
	default:
		return m.chatView()
	}
}

func (m model) selectView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Stoa — choose an agent + scenario"))
	b.WriteString("\n\n")
	for i, opt := range m.options {
		cursor := "  "
		label := opt.Label
		if i == m.cursor {
			cursor = cursorStyle.Render("> ")
			label = selectedStyle.Render(label)
		}
		b.WriteString(cursor)
		b.WriteString(label)
		if opt.Hint != "" {
			b.WriteString("  ")
			b.WriteString(hintStyle.Render(opt.Hint))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(footerStyle.Render("↑/↓ move · enter start · q quit"))
	return b.String()
}

func (m model) chatView() string {
	header := headerStyle.Render(m.label)
	if m.turn > 0 {
		header += hintStyle.Render(fmt.Sprintf("  ·  turn %d", m.turn))
	}

	status := " "
	if m.running {
		status = m.spinner.View() + " running… (ctrl+c cancels this turn)"
	}

	footer := "enter send · pgup/pgdn scroll · esc back · ctrl+c quit"
	if m.running {
		footer = "ctrl+c cancel turn"
	}

	return strings.Join([]string{
		header,
		m.viewport.View(),
		systemStyle.Render(status),
		m.input.View(),
		footerStyle.Render(footer),
	}, "\n")
}
