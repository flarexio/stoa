package world

import (
	"context"
	"fmt"
	"slices"
)

// Validator enforces hard game rules for an NPCIntent before execution.
// World's maps are not deep-copied; callers must not mutate them while a
// Validator is in use.
type Validator struct {
	World   WorldState
	ActorID string
}

func (v Validator) Validate(_ context.Context, intent NPCIntent) error {
	actor, ok := v.World.Actors[v.ActorID]
	if !ok {
		return fmt.Errorf("world: actor %q does not exist", v.ActorID)
	}

	action := intent.Action

	allowed, hasRole := roleAllowedActions[actor.Role]
	if !hasRole || !allowed[action.Type] {
		return fmt.Errorf("world: action %q is not allowed for role %q", action.Type, actor.Role)
	}

	if dialogueActions[action.Type] && intent.Say == "" {
		return fmt.Errorf("world: action %q requires non-empty dialogue", action.Type)
	}

	if interactionActions[action.Type] {
		if action.TargetID == "" {
			return fmt.Errorf("world: action %q requires a target", action.Type)
		}
		target, ok := v.World.Actors[action.TargetID]
		if !ok {
			return fmt.Errorf("world: target actor %q does not exist", action.TargetID)
		}
		if actor.LocationID != target.LocationID {
			return fmt.Errorf("world: actor %q and target %q are not in the same location", v.ActorID, action.TargetID)
		}
	}

	if itemActions[action.Type] {
		if action.ItemID == "" {
			return fmt.Errorf("world: action %q requires an item", action.Type)
		}
		if _, ok := v.World.Items[action.ItemID]; !ok {
			return fmt.Errorf("world: item %q does not exist", action.ItemID)
		}
		if !actorOwns(actor, action.ItemID) {
			return fmt.Errorf("world: actor %q does not own item %q", v.ActorID, action.ItemID)
		}
	}

	if action.Type == ActionMove {
		if action.LocationID == "" {
			return fmt.Errorf("world: action %q requires a destination location", action.Type)
		}
		if _, ok := v.World.Locations[action.LocationID]; !ok {
			return fmt.Errorf("world: location %q does not exist", action.LocationID)
		}
		current, ok := v.World.Locations[actor.LocationID]
		if !ok {
			return fmt.Errorf("world: actor's current location %q does not exist", actor.LocationID)
		}
		if !isConnected(current, action.LocationID) {
			return fmt.Errorf("world: location %q is not reachable from %q", action.LocationID, actor.LocationID)
		}
	}

	return nil
}

func actorOwns(actor Actor, itemID string) bool {
	return slices.Contains(actor.Inventory, itemID)
}

func isConnected(loc Location, targetID string) bool {
	return slices.Contains(loc.Connections, targetID)
}
