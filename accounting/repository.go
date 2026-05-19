package accounting

import "context"

// LedgerRepository is the projection of one company's books: the bookkeeping
// agent reads it to validate intents, and consumers write to it when applying
// JournalPosted events. It is the single port a persistence adapter satisfies
// (persistence/memory, persistence/postgres); transport adapters do not.
//
// Apply is the only mutation path for journal state -- producers publish a
// JournalPosted and let a subscribed EventHandler invoke Apply. Point reads
// return (value, true, nil) when found and (zero, false, nil) for not-found;
// listings return a snapshot the caller cannot use to mutate stored state.
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

	// Apply records a posted entry from a JournalPosted event, updating
	// LastSequence for evt.Subject atomically with the entry insertion.
	Apply(ctx context.Context, evt JournalPosted) error

	// LastSequence returns the broker sequence of the most recent applied
	// JournalPosted on subject, or 0 when none has been seen.
	LastSequence(ctx context.Context, subject string) (uint64, error)
}
