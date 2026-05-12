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
  -> accounting.Ledger.Post records the entry only after validation succeeds
  -> on validation failure the loop feeds the error back for correction
```

The bookkeeping layer never writes to the ledger directly. The only way a
journal entry enters the ledger is through `Ledger.Post`, which re-runs
`Validator` before appending.

## Model

| Concept            | Type                          | Notes                                                                 |
| ------------------ | ----------------------------- | --------------------------------------------------------------------- |
| Company            | `accounting.Company`          | The legal entity that owns the ledger.                                |
| Chart of accounts  | `Ledger.Accounts` (`Account`) | Active flag controls whether new postings may use the account.        |
| Accounting period  | `Ledger.Periods` (`Period`)   | Status `open` or `closed`; closed periods reject postings.            |
| Journal entry      | `JournalEntry`                | Posted, sealed entry. Stored privately and returned by copy.          |
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

`Ledger.Post` is the single mutation path. It validates the intent, assigns
a sequential ID (overridable for tests), stamps `PostedAt` via the ledger's
`Clock`, and appends a deep copy of the lines to its private slice. Both
`Ledger.Post` and `Ledger.Entries` / `Ledger.Entry` return cloned copies,
so callers cannot mutate stored entries through any returned value. That is
how the "posted journal entries are immutable" invariant is enforced
mechanically rather than by convention.

## Branches are reporting dimensions

Branches share the single ledger. They appear only on
`JournalLine.Dimensions.BranchID` and are validated as known reporting
dimensions. They are deliberately *not* separate `Ledger` instances; that
prevents the system from drifting into branch-level shadow accounting.

## Running the demo

The `stoa book-run` CLI loads a scenario JSON file, runs the
`bookkeeper.Agent` loop, and prints a JSON report:

```bash
# Offline, deterministic. Scripted engine first proposes an unbalanced
# journal so the demo always walks through the validation-feedback loop.
go run ./cmd/stoa book-run testdata/accounting/aws_bill.json \
  --request "Paid AWS bill 100 USD using company credit card"

# Live, against the real OpenAI API. Needs OPENAI_API_KEY in the
# environment. --amount / --currency are ignored in this mode; the LLM
# reads both from the request.
OPENAI_API_KEY=sk-... go run ./cmd/stoa book-run \
  testdata/accounting/aws_bill.json \
  --engine openai \
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
