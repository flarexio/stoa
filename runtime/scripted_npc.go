package runtime

import (
	"context"
	"fmt"
	"sort"

	"github.com/flarexio/stoa/llm"
	"github.com/flarexio/stoa/world"
)

// ScriptedNPCEngine is a deterministic, offline llm.ReasoningEngine used
// by the demo CLI and future TUI. It first proposes an intent that the
// world.Validator will reject (giving an item the actor does not own),
// then — once validation feedback appears — proposes a valid intent.
type ScriptedNPCEngine struct {
	world   world.WorldState
	actorID string
}

// NewScriptedNPCEngine creates a deterministic reasoning engine for the
// given world state and actor.
func NewScriptedNPCEngine(w world.WorldState, actorID string) *ScriptedNPCEngine {
	return &ScriptedNPCEngine{world: w, actorID: actorID}
}

func (e *ScriptedNPCEngine) Predict(_ context.Context, input llm.ReasoningInput) (llm.ReasoningResult[world.NPCIntent], error) {
	if HasValidationFeedback(input.Events) {
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

func (e *ScriptedNPCEngine) firstAttempt() (world.NPCIntent, string) {
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

func (e *ScriptedNPCEngine) recover() (world.NPCIntent, string) {
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

func (e *ScriptedNPCEngine) firstOtherActorInLocation() string {
	actor, ok := e.world.Actors[e.actorID]
	if !ok {
		return ""
	}
	for _, id := range SortedActorIDs(e.world.Actors) {
		if id == e.actorID {
			continue
		}
		if e.world.Actors[id].LocationID == actor.LocationID {
			return id
		}
	}
	return ""
}

func roleCanSpeak(role world.ActorRole) bool {
	switch role {
	case world.RoleMerchant, world.RolePlayer, world.RoleGuard, world.RoleBandit:
		return true
	}
	return false
}

// HasValidationFeedback returns true if any cycle event is a validation
// or execution error, signalling that the model should self-correct.
func HasValidationFeedback(events []llm.CycleEvent) bool {
	for _, ev := range events {
		if ev.Kind == llm.EventValidationError || ev.Kind == llm.EventExecutionError {
			return true
		}
	}
	return false
}

// SortedActorIDs returns actor IDs in sorted order for deterministic
// iteration.
func SortedActorIDs(actors map[string]world.Actor) []string {
	ids := make([]string, 0, len(actors))
	for id := range actors {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
