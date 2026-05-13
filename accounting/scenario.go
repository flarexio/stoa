package accounting

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Scenario is the on-disk shape of an accounting fixture. It carries the
// company, chart of accounts, branches, and periods that seed a
// LedgerRepository before the bookkeeper agent starts posting entries.
//
// Scenario intentionally does not carry journal entries: those arrive
// through the event stream as JournalPosted, never as static fixture
// data, so the projection is always built from the same code path in
// tests and in production.
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

// Seed loads the scenario's chart of accounts, branches, and periods into
// repo through its Put* methods. Callers typically pass an empty
// repository; Seed does not check for or merge with pre-existing state.
func (s Scenario) Seed(ctx context.Context, repo LedgerRepository) error {
	for _, a := range s.Accounts {
		if err := repo.PutAccount(ctx, a); err != nil {
			return fmt.Errorf("accounting: seed account %q: %w", a.Code, err)
		}
	}
	for _, b := range s.Branches {
		if err := repo.PutBranch(ctx, b); err != nil {
			return fmt.Errorf("accounting: seed branch %q: %w", b.ID, err)
		}
	}
	for _, p := range s.Periods {
		if err := repo.PutPeriod(ctx, p); err != nil {
			return fmt.Errorf("accounting: seed period %q: %w", p.ID, err)
		}
	}
	return nil
}
