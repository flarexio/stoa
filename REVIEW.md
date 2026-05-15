# Code Review Guidelines (Root)

Global guidelines for Claude Code when reviewing changes in this repository.
Subdirectory `REVIEW.md` files (e.g. `persistence/postgres/REVIEW.md`) add
scoped rules on top of these.

## Read first

1. `AGENTS.md` — project-wide standards and critical rules.
2. `docs/architecture.md` — Clean Architecture layout, the Stoa cycle, and
   the feature-slice package layout.

These two documents are the source of truth. The list below is just the
checklist of things easy to miss in a PR.

## What to flag

- **Wrong-direction imports.** A domain package importing `llm/`, its own
  agent package, or any provider SDK. `harness/` importing a provider SDK.
  `llm/` importing a feature-specific package.
- **Rules in prompts that should be validators.** If a constraint can be
  checked in pure Go, it must not live only in a system prompt string.
- **Untyped handoffs.** Free-form strings passed between agents where a
  typed struct would do.
- **Blind retries.** Loops that retry without feeding the validation or
  execution error back as a typed `llm.CycleEvent` for the next cycle.
- **Provider leakage into use cases.** OpenAI (or any other SDK) types
  appearing outside `llm/<provider>/`.
- **Provider adapters doing more than translating.** Code under
  `llm/<provider>/` must route model output through `llm.Decoder` (no
  inline JSON parsing), keep its `var _ llm.ReasoningEngine[...]`
  compile-time assertion, import no feature package, and never log the
  API key. New provider capabilities (streaming, tool calling) need an
  `llm` contract change before the provider implementation.
- **Speculative abstraction.** New shared packages or interfaces with no
  second caller yet.
- **Public contract changes without callers updated.** Touching
  `llm.ReasoningEngine`, `llm.PromptRenderer`, `llm.Decoder`, or
  `harness/loop.Runner` requires the same PR to update every agent
  package that consumes it (`bookkeeper/`, `npc/`), `llm/openai/`, and
  tests.
- **Domain rule changes without tests.** New or relaxed validation rules
  that ship without both a passing and a failing test case.

## Tone

Concise findings beat exhaustive ones. If the change looks fine, say so
briefly and move on.
