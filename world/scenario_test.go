package world_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/flarexio/stoa/world"
)

func TestLoadScenarioFile_Tavern(t *testing.T) {
	path := filepath.Join("..", "testdata", "scenarios", "tavern.json")
	scenario, err := world.LoadScenarioFile(path)
	if err != nil {
		t.Fatalf("load tavern scenario: %v", err)
	}

	if scenario.Name == "" {
		t.Errorf("expected scenario name, got empty string")
	}
	if scenario.Summary == "" {
		t.Errorf("expected scenario summary, got empty string")
	}

	mira, ok := scenario.State.Actors["mira"]
	if !ok {
		t.Fatalf("expected actor mira, got actors %v", keys(scenario.State.Actors))
	}
	if mira.Role != world.RoleMerchant {
		t.Errorf("expected mira role merchant, got %q", mira.Role)
	}
	if mira.LocationID != "tavern" {
		t.Errorf("expected mira in tavern, got %q", mira.LocationID)
	}
	if got := strings.Join(mira.Inventory, ","); got != "healing_potion" {
		t.Errorf("expected mira inventory healing_potion, got %q", got)
	}

	if _, ok := scenario.State.Locations["tavern"]; !ok {
		t.Errorf("expected tavern location, got locations %v", keys(scenario.State.Locations))
	}
	if _, ok := scenario.State.Items["healing_potion"]; !ok {
		t.Errorf("expected healing_potion item, got items %v", keys(scenario.State.Items))
	}
	if rel, ok := scenario.State.Relations[world.RelationKey("player", "mira")]; !ok {
		t.Errorf("expected player:mira relation")
	} else if rel.Reputation != -30 {
		t.Errorf("expected reputation -30, got %d", rel.Reputation)
	}
}

func TestDecodeScenario_RejectsUnknownFields(t *testing.T) {
	const doc = `{"locations": {}, "actors": {}, "items": {}, "relations": {}, "extra": 1}`
	_, err := world.DecodeScenario(strings.NewReader(doc))
	if err == nil {
		t.Fatal("expected error for unknown field, got nil")
	}
}

func TestLoadScenarioFile_MissingPath(t *testing.T) {
	_, err := world.LoadScenarioFile(filepath.Join("..", "testdata", "scenarios", "does_not_exist.json"))
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
