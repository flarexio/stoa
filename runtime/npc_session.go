package runtime

import (
	"context"
	"fmt"

	"github.com/flarexio/stoa/harness/loop"
	"github.com/flarexio/stoa/npc"
	"github.com/flarexio/stoa/world"
)

// NPCSession manages the lifecycle of an NPC agent session. It loads the
// scenario and builds the agent, exposing a Run method that a
// conversational front-end can call per user task.
type NPCSession struct {
	Scenario world.Scenario
	Agent    npc.Agent
	ActorID  string
}

// NPCSessionOptions carries the parameters needed to initialise an NPC
// session.
type NPCSessionOptions struct {
	ScenarioPath string
	ActorID      string
	MaxTurns     int
}

// NewNPCSession loads a world scenario and constructs the NPC agent for
// the given actor.
func NewNPCSession(_ context.Context, opts NPCSessionOptions) (*NPCSession, error) {
	scenario, err := world.LoadScenarioFile(opts.ScenarioPath)
	if err != nil {
		return nil, err
	}
	if _, ok := scenario.State.Actors[opts.ActorID]; !ok {
		return nil, fmt.Errorf("runtime: actor %q not present in scenario", opts.ActorID)
	}

	maxTurns := opts.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 3
	}

	engine := NewScriptedNPCEngine(scenario.State, opts.ActorID)
	agent := npc.Agent{Engine: engine, MaxTurns: maxTurns}

	return &NPCSession{
		Scenario: scenario,
		Agent:    agent,
		ActorID:  opts.ActorID,
	}, nil
}

// Run executes a single NPC task. If the task is empty, the scenario
// summary is used as the default. If sink is non-nil, per-turn events are
// streamed through it as they happen.
func (s *NPCSession) Run(ctx context.Context, task string, sink loop.EventSink) (npc.Result, error) {
	taskText := task
	if taskText == "" {
		taskText = s.Scenario.Summary
	}
	if taskText == "" {
		taskText = fmt.Sprintf("Decide what %s does next.", s.ActorID)
	}

	agent := s.Agent
	agent.Sink = sink
	return agent.Act(ctx, s.ActorID, s.Scenario.State, taskText)
}
