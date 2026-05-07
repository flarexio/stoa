# Review Guidelines: `icd/`

This package is the ICD-10 clinical coding **domain**. It owns entities,
validators, and port interfaces (with stdlib-only default implementations).
Apply these guidelines on top of the root `REVIEW.md`.

## Hard rules

- **No imports from `coder/`, `harness/`, `llm/`, or any provider SDK.**
  This package must be importable by an offline batch job or handoff
  receiver without dragging LLM code along. Run `go list -deps ./icd/...`
  in your head before approving a new import.
- **Stdlib-only defaults stay here; external infrastructure does not.**
  `InMemoryDictionary`, `InMemoryRecorder`, and similar in-process types
  ship next to the interfaces they satisfy (compare `io.Discard`).
  Postgres/Redis/HTTP adapters belong in sibling subpackages
  (`icd/postgres/`, `icd/redis/`), never in this directory.
- **Validators must enforce rules with pure Go**, not by relying on prompt
  text upstream. Examples already enforced:
  - `Code` non-empty and present in the supplied `Dictionary`.
  - `EvidenceSpan` is a verbatim substring of the source note (after
    whitespace normalization).
  - `Confidence` in `[0, 1]`.
  - No duplicate codes within an intent.
  - `System`, when set, equals `SystemICD10`.
  Any new domain rule must be added here, not in `coder/prompt.go`.
- **`Validator.Validate` returns an aggregated error** via `errors.Join`.
  Do not short-circuit on the first error; the LLM benefits from seeing all
  problems in one feedback turn.

## What to flag in a PR

- A new field on `CodeSuggestion` or `Intent` that is not validated.
- A new validation rule expressed only in `coder/prompt.go` instead of here.
- A `Recorder` or `Dictionary` implementation that does I/O placed in this
  package instead of a sibling subpackage.
- Loosening of evidence-span checking (e.g., switching from substring to
  fuzzy match) without an explicit rationale and tests for both pass and
  fail cases.
- Tests that only assert happy paths. Domain tests must cover invalid
  intents, ambiguous notes, and dictionary misses.

## Tests

- `icd_test.go` is the canonical place for validator behavior. New rules
  need both a passing case and at least one failing case with an asserted
  error message fragment.
- Avoid mocks for `Dictionary`/`Recorder` — use the in-memory defaults.
