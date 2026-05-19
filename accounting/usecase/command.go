package usecase

import "github.com/flarexio/stoa/accounting"

// CommandKind tags which bookkeeping use case a Command selects. The
// reasoning model emits it as the "kind" field and the Registry routes on
// it, so the set of kinds is the agent's whole vocabulary.
type CommandKind string

const (
	CommandPostJournal    CommandKind = "post_journal"
	CommandReverseJournal CommandKind = "reverse_journal"
)

// Command is the discriminated union the bookkeeping agent's model emits.
// Kind names the use case to run; the payload field matching Kind carries
// its typed arguments. Exactly one payload is read -- the one Kind selects
// -- and any others are ignored.
//
// Modelling the agent's whole vocabulary as one type lets a single harness
// loop, generic over Command, route to many use cases: the loop validates
// and executes a Command without ever learning the union has variants, and
// the Registry does the dumb dispatch on Kind.
type Command struct {
	Kind    CommandKind               `json:"kind"`
	Post    *accounting.JournalIntent `json:"post_journal,omitempty"`
	Reverse *ReverseIntent            `json:"reverse_journal,omitempty"`
}

// ReverseIntent is the payload of a reverse_journal Command: the ID of the
// posted entry to reverse, plus an optional human reason recorded in the
// reversing entry's description for the audit trail.
type ReverseIntent struct {
	EntryID string `json:"entry_id"`
	Reason  string `json:"reason,omitempty"`
}

// CommandDescriptor is the prompt-facing description of one command
// variant: enough for the agent to tell the model the command exists and
// how to shape its arguments. Commands returns the full set, so the agent
// builds the model's command menu from the same vocabulary the Registry
// routes -- the prompt and the dispatch cannot drift.
type CommandDescriptor struct {
	Kind      CommandKind // value of the "kind" field that selects this command
	Summary   string      // one line: what the command does and when to use it
	ArgsShape string      // JSON skeleton of the payload object
}

const (
	postJournalArgsShape = `{"date":"2026-05-12T00:00:00Z","period_id":"<period_id>","currency":"USD","description":"...","lines":[{"account_code":"<code>","side":"debit","amount":10000,"memo":"...","dimensions":{"branch_id":"<branch_id>"}},{"account_code":"<code>","side":"credit","amount":10000,"memo":"...","dimensions":{}}]}`

	reverseJournalArgsShape = `{"entry_id":"<JE-id of the posted entry to reverse>","reason":"..."}`
)

// Commands returns the descriptor for every command kind, ordered by Kind.
// It is the single source of the agent's vocabulary: the prompt renders
// this list and NewBookkeepingRegistry routes exactly these kinds, a
// correspondence a registry test enforces.
func Commands() []CommandDescriptor {
	return []CommandDescriptor{
		{
			Kind:      CommandPostJournal,
			Summary:   "Post a new balanced double-entry journal entry.",
			ArgsShape: postJournalArgsShape,
		},
		{
			Kind:      CommandReverseJournal,
			Summary:   "Reverse an existing posted entry, named by its JE-id, with a mirror-image entry.",
			ArgsShape: reverseJournalArgsShape,
		},
	}
}
