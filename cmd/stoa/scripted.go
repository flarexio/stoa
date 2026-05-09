package main

import (
	"context"
	"fmt"
	"sort"

	"github.com/flarexio/stoa/llm"
	"github.com/flarexio/stoa/world"
)

// scriptedEngine is a deterministic, offline llm.ReasoningEngine used by the
// demo CLI. It first proposes an intent that the world.Validator will reject
// (giving an item the actor does not own), then — once validation feedback
// appears in the cycle events — proposes a valid intent derived from the
// scenario. This proves the reason → validate → execute → feedback loop end
// to end without needing an LLM provider.
type scriptedEngine struct {
	world   world.WorldState
	actorID string
}

func newScriptedEngine(w world.WorldState, actorID string) *scriptedEngine {
	return &scriptedEngine{world: w, actorID: actorID}
}

func (e *scriptedEngine) Predict(_ context.Context, input llm.ReasoningInput) (llm.ReasoningResult[world.NPCIntent], error) {
	if hasValidationFeedback(input.Events) {
		intent, rationale := e.recover()
		return llm.ReasoningResult[world.NPCIntent]{
			Evidence: []llm.EvidenceRef{
				{Source: "validator", Fact: "previous intent was rejected; corrected to a valid action"},
			},
			Rationale: rationale,
			Intent:    intent,
		}, nil
	}

	intent, rationale := e.firstAttempt()
	return llm.ReasoningResult[world.NPCIntent]{
		Evidence: []llm.EvidenceRef{
			{Source: "scenario", Fact: fmt.Sprintf("acting as %s", e.actorID)},
		},
		Rationale: rationale,
		Intent:    intent,
	}, nil
}

// firstAttempt proposes an intent the validator will reject, so the demo can
// exercise the feedback loop. Giving a non-existent item fails for any role
// the engine is asked to drive.
func (e *scriptedEngine) firstAttempt() (world.NPCIntent, string) {
	target := e.firstOtherActorInLocation()
	return world.NPCIntent{
			Say:     "Here, take this legendary blade I found.",
			Emotion: "boastful",
			Action: world.Action{
				Type:     world.ActionGive,
				TargetID: target,
				ItemID:   "phantom_blade",
			},
		},
		"first attempt: try to give a magical item without checking inventory"
}

// recover proposes a valid intent based on the actor's role and surroundings.
func (e *scriptedEngine) recover() (world.NPCIntent, string) {
	actor, ok := e.world.Actors[e.actorID]
	if !ok {
		return world.NPCIntent{Action: world.Action{Type: world.ActionIdle}},
			"recover: actor missing from world; idle"
	}

	if target := e.firstOtherActorInLocation(); target != "" && roleCanSpeak(actor.Role) {
		return world.NPCIntent{
				Say:     "Apologies — I spoke without checking. Let me reconsider.",
				Emotion: "measured",
				Action: world.Action{
					Type:     world.ActionSpeak,
					TargetID: target,
				},
			},
			"recover: validation rejected the gift; speak instead of giving"
	}

	return world.NPCIntent{
			Emotion: "thoughtful",
			Action:  world.Action{Type: world.ActionIdle},
		},
		"recover: no valid speak target available; idle"
}

func (e *scriptedEngine) firstOtherActorInLocation() string {
	actor, ok := e.world.Actors[e.actorID]
	if !ok {
		return ""
	}
	// Iterate in a stable order so output is deterministic across runs.
	for _, id := range sortedActorIDs(e.world.Actors) {
		if id == e.actorID {
			continue
		}
		if e.world.Actors[id].LocationID == actor.LocationID {
			return id
		}
	}
	return ""
}

func hasValidationFeedback(events []llm.CycleEvent) bool {
	for _, ev := range events {
		if ev.Kind == llm.EventValidationError || ev.Kind == llm.EventExecutionError {
			return true
		}
	}
	return false
}

func roleCanSpeak(role world.ActorRole) bool {
	switch role {
	case world.RoleMerchant, world.RolePlayer, world.RoleGuard, world.RoleBandit:
		return true
	}
	return false
}

func sortedActorIDs(actors map[string]world.Actor) []string {
	ids := make([]string, 0, len(actors))
	for id := range actors {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
