// Package world is the game-domain package for Stoa's NPC harness.
// It defines typed world state, actors, items, locations, and the NPCIntent
// that flows through the reason→validate→execute loop. This package has no
// dependency on LLM SDKs, the harness, or any provider-specific code.
package world

type ActorRole string

const (
	RoleMerchant ActorRole = "merchant"
	RolePlayer   ActorRole = "player"
	RoleGuard    ActorRole = "guard"
	RoleBandit   ActorRole = "bandit"
)

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

var interactionActions = map[ActionType]bool{
	ActionSpeak: true, ActionOffer: true, ActionRefuse: true,
	ActionGive: true, ActionTrade: true,
}

var dialogueActions = map[ActionType]bool{
	ActionSpeak: true, ActionOffer: true, ActionRefuse: true,
}

var itemActions = map[ActionType]bool{
	ActionGive: true, ActionTrade: true,
}

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

type Personality struct {
	Cautious bool `json:"cautious"`
	Friendly bool `json:"friendly"`
}

type Relationship struct {
	Reputation int `json:"reputation"` // -100 to 100
}

type Location struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Connections []string `json:"connections"` // IDs of reachable locations
}

type Actor struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Role        ActorRole   `json:"role"`
	LocationID  string      `json:"location_id"`
	Inventory   []string    `json:"inventory"` // Item IDs
	Personality Personality `json:"personality"`
}

type Item struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Value int    `json:"value"`
}

type WorldState struct {
	Locations map[string]Location     `json:"locations"`
	Actors    map[string]Actor        `json:"actors"`
	Items     map[string]Item         `json:"items"`
	Relations map[string]Relationship `json:"relations"` // key: "fromID:toID"
}

func RelationKey(from, to string) string {
	return from + ":" + to
}

type Action struct {
	Type       ActionType `json:"type"`
	TargetID   string     `json:"target_id,omitempty"`
	ItemID     string     `json:"item_id,omitempty"`
	LocationID string     `json:"location_id,omitempty"`
}

// NPCIntent is the contract the model must satisfy for one NPC turn.
type NPCIntent struct {
	Say     string `json:"say"`
	Emotion string `json:"emotion"`
	Action  Action `json:"action"`
}
