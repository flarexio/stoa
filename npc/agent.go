// Package npc is the NPC use-case package for Stoa's game agent harness.
// It wires world.NPCIntent through harness/loop.Runner: the LLM proposes an
// NPCIntent, world.Validator enforces hard game rules, and the executor
// observes world state only after validation passes. Provider-specific code
// stays outside this package; wire it through llm.ReasoningEngine.
package npc

import (
	"context"
	"fmt"

	"github.com/flarexio/stoa/harness/loop"
	"github.com/flarexio/stoa/llm"
	"github.com/flarexio/stoa/world"
)

// Agent orchestrates one NPC decision turn: reason → validate → execute.
type Agent struct {
	Engine   llm.ReasoningEngine[world.NPCIntent]
	MaxTurns int
}

// Result holds the outcome of one NPC reasoning turn.
type Result struct {
	Intent      world.NPCIntent
	Observation llm.Observation
	Turns       int
	Events      []llm.CycleEvent
}

// Act runs one NPC reasoning cycle for actorID in world w.
// task describes the in-world situation the NPC should respond to.
func (a Agent) Act(ctx context.Context, actorID string, w world.WorldState, task string) (Result, error) {
	if a.Engine == nil {
		return Result{}, fmt.Errorf("npc: agent has no reasoning engine")
	}

	validator := world.Validator{World: w, ActorID: actorID}

	executor := loop.ExecutorFunc[world.NPCIntent](func(_ context.Context, intent world.NPCIntent) (llm.Observation, error) {
		actor := w.Actors[actorID]
		return llm.Observation{
			Summary: fmt.Sprintf("%s says: %q. Action: %s.", actor.Name, intent.Say, intent.Action.Type),
			Fields: map[string]string{
				"actor":  actorID,
				"action": string(intent.Action.Type),
			},
		}, nil
	})

	runner := loop.Runner[world.NPCIntent]{
		Engine:    a.Engine,
		Validator: validator,
		Executor:  executor,
		MaxTurns:  a.MaxTurns,
	}

	out, err := runner.Run(ctx, llm.ReasoningInput{
		Task:         task,
		Instructions: npcInstructions,
	})
	return Result{
		Intent:      out.Reasoning.Intent,
		Observation: out.Observation,
		Turns:       out.Turns,
		Events:      out.Events,
	}, err
}

const npcInstructions = `You are a game NPC. Propose a typed intent with dialogue and one action.
If validation feedback is present in the message history, fix only the problems it names and resubmit.
Output JSON only. No prose outside the JSON object.`
