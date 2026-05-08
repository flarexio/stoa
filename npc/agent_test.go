package npc_test

import (
	"context"
	"errors"
	"testing"

	"github.com/flarexio/stoa/harness/loop"
	"github.com/flarexio/stoa/llm"
	"github.com/flarexio/stoa/npc"
	"github.com/flarexio/stoa/world"
)

type fakeEngineFunc func(ctx context.Context, input llm.ReasoningInput) (llm.ReasoningResult[world.NPCIntent], error)

func (f fakeEngineFunc) Predict(ctx context.Context, input llm.ReasoningInput) (llm.ReasoningResult[world.NPCIntent], error) {
	return f(ctx, input)
}

// tavernScenario is a small deterministic fixture for NPC tests.
// Mira is a cautious merchant who owns a healing potion; the player is in
// the same tavern but has low reputation with Mira. North road has bandits.
func tavernScenario() (world.WorldState, string) {
	return world.WorldState{
		Locations: map[string]world.Location{
			"tavern":     {ID: "tavern", Name: "The Rusty Flagon", Connections: []string{"north_road"}},
			"north_road": {ID: "north_road", Name: "North Road (bandits)", Connections: []string{"tavern"}},
		},
		Actors: map[string]world.Actor{
			"mira": {
				ID:          "mira",
				Name:        "Mira",
				Role:        world.RoleMerchant,
				LocationID:  "tavern",
				Inventory:   []string{"healing_potion"},
				Personality: world.Personality{Cautious: true},
			},
			"player": {
				ID:         "player",
				Name:       "Adventurer",
				Role:       world.RolePlayer,
				LocationID: "tavern",
			},
		},
		Items: map[string]world.Item{
			"healing_potion": {ID: "healing_potion", Name: "Healing Potion", Value: 50},
		},
		Relations: map[string]world.Relationship{
			world.RelationKey("player", "mira"): {Reputation: -30},
		},
	}, "mira"
}

func TestAgent_ValidActionExecution(t *testing.T) {
	w, actorID := tavernScenario()
	engine := fakeEngineFunc(func(_ context.Context, _ llm.ReasoningInput) (llm.ReasoningResult[world.NPCIntent], error) {
		return llm.ReasoningResult[world.NPCIntent]{
			Rationale: "player has low reputation; stay cautious",
			Intent: world.NPCIntent{
				Say:     "I don't deal with strangers without good faith.",
				Emotion: "wary",
				Action:  world.Action{Type: world.ActionSpeak, TargetID: "player"},
			},
		}, nil
	})

	agent := npc.Agent{Engine: engine, MaxTurns: 3}
	res, err := agent.Act(context.Background(), actorID, w, "Player approaches Mira's stall.")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if res.Turns != 1 {
		t.Fatalf("expected 1 turn, got %d", res.Turns)
	}
	if res.Intent.Action.Type != world.ActionSpeak {
		t.Fatalf("expected speak action, got %s", res.Intent.Action.Type)
	}
}

func TestAgent_CorrectsAfterValidationFeedback(t *testing.T) {
	w, actorID := tavernScenario()

	calls := 0
	engine := fakeEngineFunc(func(_ context.Context, input llm.ReasoningInput) (llm.ReasoningResult[world.NPCIntent], error) {
		calls++
		switch calls {
		case 1:
			// First attempt: Mira tries to give an item she doesn't own.
			return llm.ReasoningResult[world.NPCIntent]{
				Rationale: "be generous",
				Intent: world.NPCIntent{
					Say:     "Here, take this sword.",
					Emotion: "generous",
					Action:  world.Action{Type: world.ActionGive, TargetID: "player", ItemID: "magic_sword"},
				},
			}, nil
		default:
			sawValidationErr := false
			for _, e := range input.Events {
				if e.Kind == llm.EventValidationError {
					sawValidationErr = true
				}
			}
			if !sawValidationErr {
				t.Errorf("expected validation_error event on retry, got events %+v", input.Events)
			}
			// Second attempt: Mira speaks instead.
			return llm.ReasoningResult[world.NPCIntent]{
				Rationale: "corrected: I can only give what I own",
				Intent: world.NPCIntent{
					Say:     "I can only offer what I carry.",
					Emotion: "cautious",
					Action:  world.Action{Type: world.ActionSpeak, TargetID: "player"},
				},
			}, nil
		}
	})

	agent := npc.Agent{Engine: engine, MaxTurns: 3}
	res, err := agent.Act(context.Background(), actorID, w, "Player asks Mira for a magic sword.")
	if err != nil {
		t.Fatalf("expected success after correction, got %v", err)
	}
	if res.Turns != 2 {
		t.Fatalf("expected 2 turns, got %d", res.Turns)
	}
	if calls != 2 {
		t.Fatalf("expected engine called twice, got %d", calls)
	}
}

func TestAgent_GivesUpAfterMaxTurns(t *testing.T) {
	w, actorID := tavernScenario()
	engine := fakeEngineFunc(func(_ context.Context, _ llm.ReasoningInput) (llm.ReasoningResult[world.NPCIntent], error) {
		// Merchant role does not include ActionMove — always invalid.
		return llm.ReasoningResult[world.NPCIntent]{
			Rationale: "stubborn",
			Intent:    world.NPCIntent{Action: world.Action{Type: world.ActionMove, LocationID: "north_road"}},
		}, nil
	})

	agent := npc.Agent{Engine: engine, MaxTurns: 2}
	_, err := agent.Act(context.Background(), actorID, w, "What does Mira do?")
	if !errors.Is(err, loop.ErrMaxTurnsExceeded) {
		t.Fatalf("expected ErrMaxTurnsExceeded, got %v", err)
	}
}
