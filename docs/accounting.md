# Accounting Domain

The `accounting/` package is Stoa's bookkeeping conscience. It owns the
ledger model and the validation rules that any AI-proposed journal entry
must satisfy before it can be posted. The package is pure: no LLM SDK, no
harness, no CLI imports. Bookkeeping orchestration lives in `bookkeeper/`,
which proposes typed `accounting.JournalIntent` values and feeds validation
errors back to the model for self-correction.

## Flow

```text
bookkeeping request
  -> bookkeeper.Agent asks the LLM to propose a JournalIntent
  -> accounting.Validator enforces the accounting invariants
  -> the validated entry is published as a JournalPosted event
  -> a subscribed handler applies the event to the LedgerRepository projection
  -> on validation failure the loop feeds the error back for correction
```

The bookkeeping layer never writes to the projection directly. The
producer (`bookkeeper.Agent`) only validates and publishes a
`JournalPosted` event; the single writer to the `LedgerRepository`
projection is the `Apply` handler subscribed to the event bus. See
`docs/architecture.md` for the event-driven wiring.

## Model

| Concept            | Type                          | Notes                                                                 |
| ------------------ | ----------------------------- | --------------------------------------------------------------------- |
| Company            | `accounting.Company`          | The legal entity that owns the ledger.                                |
| Chart of accounts  | `LedgerRepository.Accounts` (`Account`) | Active flag controls whether new postings may use the account.  |
| Accounting period  | `LedgerRepository.Periods` (`Period`)   | Status `open` or `closed`; closed periods reject postings.      |
| Journal entry      | `JournalEntry`                | Posted, sealed entry. Returned by copy from the repository.           |
| Journal line       | `JournalLine`                 | One debit or credit. `Amount` is in minor units (e.g. cents).         |
| Branch dimension   | `Dimensions.BranchID`         | Reporting tag on a line. Branches are *not* separate ledgers.         |
| Future dimensions  | `Dimensions.Tags`             | Open-ended map for project / department / channel without API churn. |

Amounts use `int64` minor units so the balance check is exact and never
depends on floating-point comparison.

## Invariants enforced by `Validator`

- `currency` is present.
- `period_id` is present, the period exists, and the period is open.
- Each journal entry has at least two lines.
- Each line's amount is positive.
- Each line's side is either `debit` or `credit`.
- Each line references an account that exists *and* is active.
- Branch dimensions, when specified, reference a known branch.
- Total debit equals total credit (only checked once there are at least two lines).

`Validator.Validate` joins every violation with `errors.Join` so a single
correction cycle can address all problems at once.

## Posting and immutability

A posted `JournalEntry` is immutable. `bookkeeper.Agent` derives the entry
ID from the broker sequence (`accounting.FormatEntryID(lastSeq+1)`) and
stamps `PostedAt` via its clock before publishing the `JournalPosted`
event; the `LedgerRepository.Apply` handler then writes the projection.
The entry is never edited afterwards -- corrections are posted as new
reversing entries, never as in-place edits. This is a
double-entry-bookkeeping invariant (SOX / GAAP / IFRS all require the
audit trail to be preserved verbatim), documented in full in the
`accounting` package overview.

Repository reads (`Entries`, `Entry`) return entries by value, and the
in-memory implementation deep-copies the lines slice, so callers cannot
mutate stored state through any returned value.

## Branches are reporting dimensions

Branches share the single ledger. They appear only on
`JournalLine.Dimensions.BranchID` and are validated as known reporting
dimensions. They are deliberately *not* separate `Ledger` instances; that
prevents the system from drifting into branch-level shadow accounting.

## Running the demo

The `stoa book-run` CLI loads a scenario JSON file, runs the
`bookkeeper.Agent` loop, and prints a JSON report. It reads `config.yaml`
from its work directory (`~/.flarex/stoa` by default); an empty file is
valid and selects the all-offline defaults -- memory persistence,
in-process event bus, scripted engine.

```bash
# One-time: an empty config.yaml selects the all-offline defaults.
mkdir -p ~/.flarex/stoa && touch ~/.flarex/stoa/config.yaml

# Offline, deterministic. The scripted engine first proposes an
# unbalanced journal so the demo always walks through the
# validation-feedback loop.
go run ./cmd/stoa book-run testdata/accounting/aws_bill.json \
  --request "Paid AWS bill 100 USD using company credit card"

# Live, against the real OpenAI API. Needs OPENAI_API_KEY in the
# environment and a model -- via --model or the config.yaml llm block;
# the adapter assumes no default. --amount / --currency are ignored in
# this mode; the LLM reads both from the request.
OPENAI_API_KEY=sk-... go run ./cmd/stoa book-run \
  testdata/accounting/aws_bill.json \
  --engine openai \
  --model gpt-5.4-mini \
  --request "Paid AWS bill 100 USD using company credit card on 12 May 2026"
```

The prompt rendered for the LLM is built by `bookkeeper.PromptRenderer`,
which reads the seeded ledger so the model sees the actual active chart
of accounts, open periods, and known branches. Every constraint named in
the system prompt is also enforced by `accounting.Validator`, so the LLM
cannot ship a rule violation past the harness even if the prompt fails
to dissuade it.

## Out of scope

This domain intentionally does not include:

- AR/AP, invoicing, or payments
- Payroll, tax filing, or a tax engine
- Bank reconciliation
- Inventory
- Reporting engine
- Separate branch accounting services
- Multi-currency conversion

These are not banned forever; they are simply not part of this initial
foundation.
