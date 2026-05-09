package world

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Scenario wraps a WorldState with optional metadata used by demos and CLI
// loaders. The metadata fields are advisory; only WorldState participates in
// validation.
type Scenario struct {
	Name    string     `json:"name,omitempty"`
	Summary string     `json:"summary,omitempty"`
	State   WorldState `json:"-"`
}

// scenarioFile is the on-disk shape: the world fields are inline alongside the
// optional metadata, so existing fixtures remain valid as raw WorldState JSON.
type scenarioFile struct {
	Name      string                  `json:"name,omitempty"`
	Summary   string                  `json:"summary,omitempty"`
	Locations map[string]Location     `json:"locations"`
	Actors    map[string]Actor        `json:"actors"`
	Items     map[string]Item         `json:"items"`
	Relations map[string]Relationship `json:"relations"`
}

// LoadScenarioFile reads and decodes a scenario JSON file from disk.
func LoadScenarioFile(path string) (Scenario, error) {
	f, err := os.Open(path)
	if err != nil {
		return Scenario{}, fmt.Errorf("world: open scenario %q: %w", path, err)
	}
	defer f.Close()
	return DecodeScenario(f)
}

// DecodeScenario reads a scenario JSON document from r.
func DecodeScenario(r io.Reader) (Scenario, error) {
	var raw scenarioFile
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return Scenario{}, fmt.Errorf("world: decode scenario: %w", err)
	}
	return Scenario{
		Name:    raw.Name,
		Summary: raw.Summary,
		State: WorldState{
			Locations: raw.Locations,
			Actors:    raw.Actors,
			Items:     raw.Items,
			Relations: raw.Relations,
		},
	}, nil
}
