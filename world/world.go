// Package world is the game-domain package for Stoa's NPC harness.
// It defines typed world state, actors, items, locations, and the NPCIntent
// that flows through the reason→validate→execute loop. This package has no
// dependency on LLM SDKs, the harness, or any provider-specific code.
package world

// ActorRole describes the role an actor plays in the world.
type ActorRole string

const (
	RoleMerchant ActorRole = "merchant"
	RolePlayer   ActorRole = "player"
	RoleGuard    ActorRole = "guard"
	RoleBandit   ActorRole = "bandit"
)

// ActionType names the kind of action an NPC proposes.
type ActionType string

const (
	ActionSpeak  ActionType = "speak"
	ActionOffer  ActionType = "offer"
	ActionRefuse ActionType = "refuse"
	ActionGive   ActionType = "give"
	ActionTrade  ActionType = "trade"
	ActionMove   ActionType = "move"
	ActionIdle   ActionType = "idle"
)

// interactionActions require the actor and target to share a location.
var interactionActions = map[ActionType]bool{
	ActionSpeak: true, ActionOffer: true, ActionRefuse: true,
	ActionGive: true, ActionTrade: true,
}

// dialogueActions require non-empty Say text.
var dialogueActions = map[ActionType]bool{
	ActionSpeak: true, ActionOffer: true, ActionRefuse: true,
}

// itemActions require the actor to own the item.
var itemActions = map[ActionType]bool{
	ActionGive: true, ActionTrade: true,
}

// roleAllowedActions maps each role to the set of actions it may perform.
var roleAllowedActions = map[ActorRole]map[ActionType]bool{
	RoleMerchant: {
		ActionSpeak: true, ActionOffer: true, ActionRefuse: true,
		ActionGive: true, ActionTrade: true, ActionIdle: true,
	},
	RolePlayer: {
		ActionSpeak: true, ActionMove: true, ActionTrade: true, ActionIdle: true,
	},
	RoleGuard: {
		ActionSpeak: true, ActionRefuse: true, ActionMove: true, ActionIdle: true,
	},
	RoleBandit: {
		ActionSpeak: true, ActionMove: true, ActionIdle: true,
	},
}

// Personality holds personality traits for an actor.
type Personality struct {
	Cautious bool `json:"cautious"`
	Friendly bool `json:"friendly"`
}

// Relationship captures the social standing between two actors.
type Relationship struct {
	Reputation int `json:"reputation"` // -100 to 100
}

// Location is a place in the game world.
type Location struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Connections []string `json:"connections"` // IDs of reachable locations
}

// Actor is a character (NPC or player) in the game world.
type Actor struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Role        ActorRole   `json:"role"`
	LocationID  string      `json:"location_id"`
	Inventory   []string    `json:"inventory"` // Item IDs
	Personality Personality `json:"personality"`
}

// Item is an object in the game world.
type Item struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Value int    `json:"value"`
}

// WorldState is a snapshot of the game world at one point in time.
type WorldState struct {
	Locations map[string]Location      `json:"locations"`
	Actors    map[string]Actor         `json:"actors"`
	Items     map[string]Item          `json:"items"`
	Relations map[string]Relationship  `json:"relations"` // key: "fromID:toID"
}

// RelationKey builds the map key for WorldState.Relations.
func RelationKey(from, to string) string {
	return from + ":" + to
}

// Action is a proposed game action within an NPCIntent.
type Action struct {
	Type       ActionType `json:"type"`
	TargetID   string     `json:"target_id,omitempty"`
	ItemID     string     `json:"item_id,omitempty"`
	LocationID string     `json:"location_id,omitempty"`
}

// NPCIntent is the typed output of an NPC reasoning step. It carries the
// dialogue, emotional state, and proposed action for one turn.
type NPCIntent struct {
	Say     string `json:"say"`
	Emotion string `json:"emotion"`
	Action  Action `json:"action"`
}
