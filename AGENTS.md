# AI Agents Development Guide (AGENTS.md)

This file is the single source of truth for all AI agents (Gemini CLI, Claude Code, OpenAI, etc.) working on this repository.

## Project Overview
Stoa is a Go workshop for building production-grade AI agents. It is not a framework -- it is an architecture and set of patterns for crafting agents that act and verify, not just reason.
The name comes from the Greek στοά (covered colonnade), connecting Stoic philosophy (control what you can) with Wang Yangming's 知行合一 (unity of knowing and doing).

### Feature domains
- **NPC harness** (`world/`, `world/agent/`): LLM-driven game NPC that proposes typed intents validated by hard game rules. Demo: `go run ./cmd/stoa npc-run testdata/scenarios/tavern.json --actor mira`.
- **Bookkeeping agent** (`accounting/`, `accounting/bookkeeping/`, `accounting/agent/`): turns a natural-language request into a typed `bookkeeping.Intent` and routes it through a use-case registry -- `post_journal` posts a validated double-entry journal entry, `reverse_journal` reverses an existing one -- publishing each as a `JournalPosted` event projected into a ledger repository. Domain rules and invariants in `docs/accounting.md`. Demo: `go run ./cmd/stoa book-run testdata/accounting/aws_bill.json --request "..."`.

## Architecture (Clean Architecture)
Dependencies flow inward. Code is organized **by feature**.
1. **Infrastructure**: LLM SDKs, databases, external services.
2. **Interface Adapters**: LLM adapters, prompt templates, output parsers.
3. **Use Cases**: Agent task flows, orchestration (defines interfaces).
4. **Domain**: Pure business entities, rules, validators (no external dependencies).

## Critical Rules
- **LLM is infrastructure, not domain.** Business logic never imports an SDK.
- **Prompts hold judgment; code holds contracts.** If a rule can be a validator, it must not be only a prompt instruction.
- **Agents communicate through typed handoff objects**, never free-form text.
- **Errors feed context back to the LLM** for self-correction rather than blind retries.
- **Provider adapters only translate.** Prompt rendering and output decoding must be replaceable strategies; domain validation never lives in an LLM adapter.
- **Domain, use case, and agent are separate packages.** The domain package holds entities, validators, and port interfaces (plus stdlib-only default adapters). The `bookkeeping` subpackage holds the application operations -- each a validate + execute step callable without an LLM -- the transport ports they publish through, and, when one agent drives several, the discriminated-union intent set and registry that route to them. The agent subpackage holds the LLM-driven loop and feature-specific prompt rendering. Imports point inward only: the domain imports neither subpackage, the use case imports neither the agent nor any LLM code, and only the agent package depends on `llm`.

## The Stoa Pattern (Intent-Validator-Execution)
To ensure "Knowing and Doing are One", every agent must follow this cycle:

1.  **Reasoning with Evidence**: The agent must output its reasoning based on provided facts before stating an intent.
2.  **Structured Intent**: The agent outputs a strictly typed `Intent` (not an action). When it needs more facts first, it may return tool calls instead, which the harness runs and feeds back before the next cycle.
3.  **Domain Validation**: The `Intent` is validated against pure Go business rules (The Conscience).
4.  **Verified Execution**: Only validated intents are executed by Go code.
5.  **Environment Feedback**: If validation or execution fails, the precise error is fed back as context for the next reasoning cycle.

## Design Decisions
- **No heavy frameworks** (LangChain, LangGraph). Keep the agent loop short and understood.
- **Go-first**: Type system as contract, implicit interfaces, and high performance.
- **Harness engineering**: Validation, retry with context, and circuit breakers are mandatory.

## Comment and Documentation Hygiene
- Prefer comments that explain domain rules, invariants, boundaries, or non-obvious behavior.
- Do not keep comments only to satisfy style convention; remove or rewrite comments that merely restate names or obvious code.
- Keep README, AGENTS, CLAUDE, and docs aligned with actual package names and runnable commands.

## Release Workflow
- If an AI agent performs a release, preserve attribution with a standard `Co-Authored-By:` trailer.
- Some tools add this automatically; for tools that do not, the agent must add it explicitly instead of omitting it.

## Current LLM Contract
- `llm.ReasoningEngine[TIntent]` returns `llm.ReasoningResult[TIntent]` with evidence, rationale, and either a typed intent or tool calls.
- A turn may return `llm.ToolCall` values instead of a final intent; `harness/loop` runs the matching tool handler and feeds the result back as a typed `tool_result` event before the next turn.
- `llm.PromptRenderer` converts typed reasoning input into provider-neutral messages.
- `llm.Decoder[TIntent]` converts raw model output into typed reasoning results. JSON is only the default decoder, not an architecture requirement.
- OpenAI code under `llm/openai` must stay provider-specific: SDK calls, message translation, response-format selection, and provider error wrapping only.
