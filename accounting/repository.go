package accounting

import "context"

// LedgerRepository is the projection of a single company's books that the
// bookkeeping agent reads to validate proposed intents and that consumers
// write to when applying JournalPosted events. It is the single port any
// persistence adapter has to satisfy; transport adapters do not implement
// it -- they carry events between producer and consumer, where Apply is
// the only write path.
//
// Implementations live in sibling top-level packages (persistence/memory,
// persistence/postgres, ...); they may be backed by anything from a
// process-local map to a SQL database, as long as this contract holds:
//
//   - Apply is the only mutation path for journal state. Producers never
//     call it directly; they publish a JournalPosted and let a subscribed
//     EventHandler invoke Apply on their behalf.
//   - PutAccount, PutPeriod, PutBranch are seed-time operations used to
//     load the chart of accounts, accounting periods, and reporting
//     branches from a Scenario or similar bootstrap. They are not in the
//     domain event stream; an event-sourced model for chart changes is
//     deliberately deferred.
//   - LastSequence reflects the highest Sequence seen on the given
//     Subject in events that have been applied. Producers use it to
//     populate ExpectedSequence on the next Publish.
//   - Point reads return (value, true, nil) when found, (zero, false, nil)
//     for not-found, and (zero, false, err) only for an infrastructure
//     error.
//   - Listings return a snapshot slice; the stored projection must not be
//     exposed by reference so callers cannot mutate the repository's
//     state through a returned value.
type LedgerRepository interface {
	// Point reads
	Account(ctx context.Context, code string) (Account, bool, error)
	Period(ctx context.Context, id string) (Period, bool, error)
	Branch(ctx context.Context, id string) (Branch, bool, error)
	Entry(ctx context.Context, id string) (JournalEntry, bool, error)

	// Listings
	Accounts(ctx context.Context) ([]Account, error)
	Periods(ctx context.Context) ([]Period, error)
	Branches(ctx context.Context) ([]Branch, error)
	Entries(ctx context.Context) ([]JournalEntry, error)

	// Seed (called by Scenario, not by the agent loop)
	PutAccount(ctx context.Context, a Account) error
	PutPeriod(ctx context.Context, p Period) error
	PutBranch(ctx context.Context, b Branch) error

	// Apply records a posted journal entry from a JournalPosted event.
	// Consumers (EventHandler implementations) call this; producers do
	// not. The repository is expected to update its LastSequence record
	// for evt.Subject atomically with the entry insertion.
	Apply(ctx context.Context, evt JournalPosted) error

	// LastSequence returns the broker sequence of the most recent
	// JournalPosted that has been applied on subject, or 0 when no event
	// has been seen yet on that subject.
	LastSequence(ctx context.Context, subject string) (uint64, error)
}
