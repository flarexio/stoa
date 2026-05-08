package npc

import (
	"fmt"
	"strings"

	"github.com/flarexio/stoa/llm"
	"github.com/flarexio/stoa/world"
)

// PromptRenderer builds provider-neutral messages for an NPC reasoning turn.
// Wire it into the llm/openai adapter via openai.Config.Renderer so the NPC
// use case never imports provider-specific packages.
type PromptRenderer struct {
	World   world.WorldState
	ActorID string
}

func (r PromptRenderer) Render(input llm.ReasoningInput) ([]llm.Message, error) {
	messages := []llm.Message{
		{Role: llm.MessageRoleSystem, Content: r.buildSystemPrompt()},
		{Role: llm.MessageRoleUser, Content: r.buildUserPrompt(input)},
	}

	for _, event := range input.Events {
		content := fmt.Sprintf("[%s:%s]\n%s", event.Role, event.Kind, strings.TrimSpace(event.Content))
		role := llm.MessageRoleUser
		if event.Role == llm.EventRoleAssistant {
			role = llm.MessageRoleAssistant
		}
		messages = append(messages, llm.Message{Role: role, Content: content})
	}

	return messages, nil
}

func (r PromptRenderer) buildSystemPrompt() string {
	actor, ok := r.World.Actors[r.ActorID]
	if !ok {
		return npcSystemPrompt
	}
	locName := actor.LocationID
	if loc, ok := r.World.Locations[actor.LocationID]; ok {
		locName = loc.Name
	}
	return fmt.Sprintf("%s\n\nYou are %s, a %s currently in %s.", npcSystemPrompt, actor.Name, actor.Role, locName)
}

func (r PromptRenderer) buildUserPrompt(input llm.ReasoningInput) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(input.Task))
	if strings.TrimSpace(input.Instructions) != "" {
		b.WriteString("\n\nInstructions:\n")
		b.WriteString(strings.TrimSpace(input.Instructions))
	}
	b.WriteString("\n\n")
	b.WriteString(r.describeWorld())
	b.WriteString("\nReturn JSON with this exact shape:\n")
	b.WriteString(`{"evidence":[{"source":"world","fact":"..."}],"rationale":"...","intent":{"say":"...","emotion":"...","action":{"type":"speak","target_id":"player_id"}}}`)
	return b.String()
}

func (r PromptRenderer) describeWorld() string {
	actor, ok := r.World.Actors[r.ActorID]
	if !ok {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Your inventory: %s\n", r.describeInventory(actor))
	b.WriteString("Actors in your location:\n")
	for id, a := range r.World.Actors {
		if id != r.ActorID && a.LocationID == actor.LocationID {
			rel := r.World.Relations[world.RelationKey(id, r.ActorID)]
			fmt.Fprintf(&b, "  - %s (%s), reputation with you: %d\n", a.Name, a.Role, rel.Reputation)
		}
	}
	return b.String()
}

func (r PromptRenderer) describeInventory(actor world.Actor) string {
	if len(actor.Inventory) == 0 {
		return "empty"
	}
	names := make([]string, 0, len(actor.Inventory))
	for _, id := range actor.Inventory {
		if item, ok := r.World.Items[id]; ok {
			names = append(names, item.Name)
		}
	}
	return strings.Join(names, ", ")
}

const npcSystemPrompt = `You are a game NPC in a validated agent harness.
Rules you must follow:
- Propose typed dialogue and one action based on the world state.
- Use only actions allowed for your role.
- Interact only with actors in your current location.
- Only give or trade items you own.
- If moving, only target reachable locations.
- If validation feedback is present, fix only the named problems and resubmit.
- Output JSON only. No prose outside the JSON object.`
