package accounting

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Scenario is the on-disk shape of an accounting fixture. It carries the
// company, chart of accounts, branches, and periods that seed a Ledger,
// plus optional metadata used by demos and tests.
type Scenario struct {
	Name        string    `json:"name,omitempty"`
	Description string    `json:"description,omitempty"`
	Company     Company   `json:"company"`
	Accounts    []Account `json:"accounts"`
	Branches    []Branch  `json:"branches,omitempty"`
	Periods     []Period  `json:"periods"`
}

// LoadScenarioFile reads and decodes a scenario JSON file from disk.
func LoadScenarioFile(path string) (Scenario, error) {
	f, err := os.Open(path)
	if err != nil {
		return Scenario{}, fmt.Errorf("accounting: open scenario %q: %w", path, err)
	}
	defer f.Close()
	return DecodeScenario(f)
}

// DecodeScenario reads a scenario JSON document from r.
func DecodeScenario(r io.Reader) (Scenario, error) {
	var s Scenario
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return Scenario{}, fmt.Errorf("accounting: decode scenario: %w", err)
	}
	return s, nil
}

// BuildLedger constructs a fresh Ledger seeded from the scenario.
func (s Scenario) BuildLedger() *Ledger {
	l := NewLedger(s.Company)
	for _, a := range s.Accounts {
		l.AddAccount(a)
	}
	for _, b := range s.Branches {
		l.AddBranch(b)
	}
	for _, p := range s.Periods {
		l.AddPeriod(p)
	}
	return l
}
