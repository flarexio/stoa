# AGENTS.md

Source of truth for AI agents (Gemini CLI, Claude Code, etc.) working in this repo. Subagent guides may extend it; they may not override it.

## Project Overview
Stoa is a Go workshop for production-grade AI agents — typed reasoning, validated execution, and a Clean Architecture layout. It's not a framework. The name comes from στοά (Greek for covered colonnade), connecting Stoic philosophy with Wang Yangming's 知行合一 — unity of knowing and doing.

### Feature domains
- **NPC harness** (`world/`, `world/agent/`): LLM-driven game NPC that proposes typed intents validated by hard game rules.
- **Bookkeeping agent** (`accounting/`, `accounting/bookkeeping/`, `accounting/agent/`): turns a natural-language request into a `bookkeeping.Intent` (`post_journal` | `reverse_journal`), routes it through a use-case registry, validates against the accounting domain, and publishes a `JournalPosted` event projected into a ledger repository. Domain rules in `docs/accounting.md`.

### CLI surface (`cmd/stoa/`)
- `stoa seed <seed.yaml | dir/>` — apply a declarative YAML ledger seed (chart of accounts, branches, periods) to the configured repository. Upsert; safe to re-run.
- `stoa book-run <scenario.json> --request "..."` — one bookkeeping cycle; prints a JSON report to stdout.
- `stoa npc-run <scenario.json> --actor <id>` — one NPC reasoning cycle.
- `stoa tui <scenario.json> ...` — conversational Bubble Tea front-end over the same reason → validate → execute loop.
- All commands read `config.yaml` from `--work-dir`, defaulting to `~/.flarex/stoa`. A missing config file is an error, never an implicit in-process fallback.

## Architecture
Clean Architecture, organized by feature slice. Dependencies point inward: Infrastructure → Interface Adapters → Use Cases → Domain. See [docs/architecture.md](docs/architecture.md) for the full layout, layer responsibilities, and the Stoa cycle.

## Critical Rules
- **LLM is infrastructure, not domain.** Business logic never imports an SDK.
- **Prompts hold judgment; code holds contracts.** If a rule can be a validator, it must not be only a prompt instruction.
- **Agents communicate through typed handoff objects**, never free-form text.
- **Errors feed context back to the LLM** for self-correction rather than blind retries.
- **Provider adapters only translate.** Prompt rendering and output decoding must be replaceable strategies; domain validation never lives in an LLM adapter.
- **Domain, use case, and agent are separate packages.** The domain package holds entities, validators, and port interfaces (plus stdlib-only default adapters). The use-case subpackage (e.g. `bookkeeping/`) holds application operations — each a validate + execute step callable without an LLM — the transport ports they publish through, and, when one agent drives several, the discriminated-union intent set and registry that route to them. The agent subpackage holds the LLM-driven loop and feature-specific prompt rendering. Imports point inward only: the domain imports neither subpackage, the use case imports neither the agent nor any LLM code, and only the agent package depends on `llm`.

## The Stoa Pattern (Intent-Validator-Execution)
To ensure "Knowing and Doing are One", every agent follows this cycle:

1. **Reasoning with Evidence**: the agent outputs its reasoning based on provided facts before stating an intent.
2. **Structured Intent**: the agent outputs a strictly typed `Intent` (not an action). When it needs more facts first, it may return tool calls instead, which the harness runs and feeds back before the next cycle.
3. **Domain Validation**: the `Intent` is validated against pure Go business rules (The Conscience).
4. **Verified Execution**: only validated intents are executed by Go code.
5. **Environment Feedback**: if validation or execution fails, the precise error is fed back as context for the next reasoning cycle.

## Design Decisions
- **No heavy frameworks** (LangChain, LangGraph). Keep the agent loop short and inspectable.
- **Validation and feedback are mandatory.** Domain errors flow back into the next reasoning cycle as typed events; never retry blindly.
- **Go-first.** Implicit interfaces; generics parameterize the harness loop and LLM contract over the feature's `Intent` type.

## Code Style
- **Few comments; godoc only.** Every exported symbol gets one concise godoc line — enough for an LLM or `go doc` reader to know what it is without seeing the body. Omit the doc entirely when the name and signature already say it.
- **No essays in source.** Multi-paragraph rationale belongs in `docs/`, in a PR description, or in a commit message — not above a function. Inline comments are for non-obvious "why" only, never to restate what the next line does.
- **Test names carry the description.** Don't write `// TestX does Y` above `func TestX`; the name is the doc. Keep only comments that explain non-obvious test mechanics (fixture invariants the assertions rely on, scaffolding rationale).
- **No section dividers** (`// --- point reads ---`). Code organization shows itself.
- **Treat comment churn as code churn.** Comments that drift out of sync are worse than no comment. If you change a function's contract, update or delete its doc in the same change.

## Release Workflow
If an AI agent performs a release, preserve the agent attribution in the commit metadata as a `Co-Author`. Some tools add this automatically; for tools that do not, the agent must add it explicitly instead of omitting it.

## Current LLM Contract
- `llm.ReasoningEngine[TIntent]` returns `llm.ReasoningResult[TIntent]` with evidence, rationale, and either a typed intent or tool calls.
- A turn may return `llm.ToolCall` values instead of a final intent; `harness/loop` runs the matching tool handler and feeds the result back as a typed `tool_result` event before the next turn.
- `llm.PromptRenderer` converts typed reasoning input into provider-neutral messages.
- `llm.Decoder[TIntent]` converts raw model output into typed reasoning results. JSON is only the default decoder, not an architecture requirement.
- OpenAI code under `llm/openai/` must stay provider-specific: SDK calls, message translation, response-format selection, and provider error wrapping only.
