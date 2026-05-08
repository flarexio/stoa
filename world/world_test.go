package world_test

import (
	"context"
	"testing"

	"github.com/flarexio/stoa/world"
)

// tavernScenario returns a small deterministic world: Mira is a cautious
// merchant in the tavern who owns a healing potion; the player is also present
// but has low reputation with Mira.
func tavernScenario() (world.WorldState, string) {
	w := world.WorldState{
		Locations: map[string]world.Location{
			"tavern":     {ID: "tavern", Name: "The Rusty Flagon", Connections: []string{"north_road"}},
			"north_road": {ID: "north_road", Name: "North Road", Connections: []string{"tavern"}},
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
	}
	return w, "mira"
}

func TestValidator_ValidSpeak(t *testing.T) {
	w, actorID := tavernScenario()
	v := world.Validator{World: w, ActorID: actorID}
	intent := world.NPCIntent{
		Say:     "I don't deal with strangers.",
		Emotion: "wary",
		Action:  world.Action{Type: world.ActionSpeak, TargetID: "player"},
	}
	if err := v.Validate(context.Background(), intent); err != nil {
		t.Fatalf("expected valid speak to pass, got %v", err)
	}
}

func TestValidator_ValidOffer(t *testing.T) {
	w, actorID := tavernScenario()
	v := world.Validator{World: w, ActorID: actorID}
	intent := world.NPCIntent{
		Say:     "I have a healing potion for sale.",
		Emotion: "cautious",
		Action:  world.Action{Type: world.ActionOffer, TargetID: "player", ItemID: "healing_potion"},
	}
	if err := v.Validate(context.Background(), intent); err != nil {
		t.Fatalf("expected valid offer to pass, got %v", err)
	}
}

func TestValidator_ValidIdle(t *testing.T) {
	w, actorID := tavernScenario()
	v := world.Validator{World: w, ActorID: actorID}
	intent := world.NPCIntent{Emotion: "neutral", Action: world.Action{Type: world.ActionIdle}}
	if err := v.Validate(context.Background(), intent); err != nil {
		t.Fatalf("expected idle to pass with no extra requirements, got %v", err)
	}
}

func TestValidator_RejectsActorNotExist(t *testing.T) {
	w, _ := tavernScenario()
	v := world.Validator{World: w, ActorID: "ghost"}
	intent := world.NPCIntent{Action: world.Action{Type: world.ActionIdle}}
	if err := v.Validate(context.Background(), intent); err == nil {
		t.Fatal("expected error for non-existent actor")
	}
}

func TestValidator_RejectsActionNotAllowedForRole(t *testing.T) {
	w, actorID := tavernScenario()
	v := world.Validator{World: w, ActorID: actorID}
	// Merchant role does not include ActionMove.
	intent := world.NPCIntent{Action: world.Action{Type: world.ActionMove, LocationID: "north_road"}}
	if err := v.Validate(context.Background(), intent); err == nil {
		t.Fatal("expected error for action not allowed for merchant role")
	}
}

func TestValidator_RejectsEmptyDialogueForSpeak(t *testing.T) {
	w, actorID := tavernScenario()
	v := world.Validator{World: w, ActorID: actorID}
	intent := world.NPCIntent{Say: "", Action: world.Action{Type: world.ActionSpeak, TargetID: "player"}}
	if err := v.Validate(context.Background(), intent); err == nil {
		t.Fatal("expected error for empty dialogue on speak action")
	}
}

func TestValidator_RejectsTargetNotExist(t *testing.T) {
	w, actorID := tavernScenario()
	v := world.Validator{World: w, ActorID: actorID}
	intent := world.NPCIntent{Say: "Hello?", Action: world.Action{Type: world.ActionSpeak, TargetID: "nobody"}}
	if err := v.Validate(context.Background(), intent); err == nil {
		t.Fatal("expected error for non-existent target")
	}
}

func TestValidator_RejectsTargetInDifferentLocation(t *testing.T) {
	w, actorID := tavernScenario()
	// Move player out of the tavern.
	player := w.Actors["player"]
	player.LocationID = "north_road"
	w.Actors["player"] = player
	v := world.Validator{World: w, ActorID: actorID}
	intent := world.NPCIntent{Say: "Wait!", Action: world.Action{Type: world.ActionSpeak, TargetID: "player"}}
	if err := v.Validate(context.Background(), intent); err == nil {
		t.Fatal("expected error for target in different location")
	}
}

func TestValidator_RejectsGiveItemNotOwned(t *testing.T) {
	w, actorID := tavernScenario()
	// Item doesn't exist in the world at all.
	v := world.Validator{World: w, ActorID: actorID}
	intent := world.NPCIntent{
		Say:    "Here, take this.",
		Action: world.Action{Type: world.ActionGive, TargetID: "player", ItemID: "dragons_egg"},
	}
	if err := v.Validate(context.Background(), intent); err == nil {
		t.Fatal("expected error for non-existent item")
	}
}

func TestValidator_RejectsGiveItemNotInInventory(t *testing.T) {
	w, actorID := tavernScenario()
	// Item exists in world but is not in Mira's inventory.
	w.Items["dragons_egg"] = world.Item{ID: "dragons_egg", Name: "Dragon's Egg", Value: 1000}
	v := world.Validator{World: w, ActorID: actorID}
	intent := world.NPCIntent{
		Say:    "Here.",
		Action: world.Action{Type: world.ActionGive, TargetID: "player", ItemID: "dragons_egg"},
	}
	if err := v.Validate(context.Background(), intent); err == nil {
		t.Fatal("expected error for item not in actor's inventory")
	}
}

func TestValidator_RejectsMoveToUnreachableLocation(t *testing.T) {
	w, _ := tavernScenario()
	// Dungeon exists but is not connected from the tavern.
	w.Locations["dungeon"] = world.Location{ID: "dungeon", Name: "Dungeon"}
	v := world.Validator{World: w, ActorID: "player"}
	intent := world.NPCIntent{Action: world.Action{Type: world.ActionMove, LocationID: "dungeon"}}
	if err := v.Validate(context.Background(), intent); err == nil {
		t.Fatal("expected error for unreachable location")
	}
}

func TestValidator_RejectsMoveToNonExistentLocation(t *testing.T) {
	w, _ := tavernScenario()
	v := world.Validator{World: w, ActorID: "player"}
	intent := world.NPCIntent{Action: world.Action{Type: world.ActionMove, LocationID: "nowhere"}}
	if err := v.Validate(context.Background(), intent); err == nil {
		t.Fatal("expected error for non-existent location")
	}
}
