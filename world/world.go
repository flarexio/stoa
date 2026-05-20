// Package world is the game-domain package for Stoa's NPC harness: typed
// world state, actors, items, locations, and the NPCIntent that flows through
// the reason -> validate -> execute loop. No LLM, harness, or provider code.
package world

// ActorRole names what an Actor can do in the world.
type ActorRole string

const (
	RoleMerchant ActorRole = "merchant"
	RolePlayer   ActorRole = "player"
	RoleGuard    ActorRole = "guard"
	RoleBandit   ActorRole = "bandit"
)

// ActionType is one verb an actor can perform on a turn.
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

// roleAllowedActions is the set of actions each role may perform.
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

// Relationship captures the social standing between two actors.
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
	Inventory   []string    `json:"inventory"` // item IDs
	Personality Personality `json:"personality"`
}

type Item struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Value int    `json:"value"`
}

// WorldState is a snapshot of the game world at one point in time.
type WorldState struct {
	Locations map[string]Location     `json:"locations"`
	Actors    map[string]Actor        `json:"actors"`
	Items     map[string]Item         `json:"items"`
	Relations map[string]Relationship `json:"relations"` // key: "fromID:toID"
}

// RelationKey builds the map key for WorldState.Relations.
func RelationKey(from, to string) string {
	return from + ":" + to
}

// Action is the proposed game action carried in an NPCIntent.
type Action struct {
	Type       ActionType `json:"type"`
	TargetID   string     `json:"target_id,omitempty"`
	ItemID     string     `json:"item_id,omitempty"`
	LocationID string     `json:"location_id,omitempty"`
}

// NPCIntent is the typed output of one NPC reasoning step.
type NPCIntent struct {
	Say     string `json:"say"`
	Emotion string `json:"emotion"`
	Action  Action `json:"action"`
}
