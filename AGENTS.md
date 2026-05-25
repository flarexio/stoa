# AGENTS.md

Source of truth for AI agents (Gemini CLI, Claude Code, etc.) working in this repo. Subagent guides may extend it; they may not override it.

## Project Overview
Stoa is a Go workshop for production-grade AI agents — typed reasoning, validated execution, and a Clean Architecture layout. It's not a framework. The name comes from στοά (Greek for covered colonnade), connecting Stoic philosophy with Wang Yangming's 知行合一 — unity of knowing and doing.

### Feature domains
- **NPC harness** (`world/`, `world/agent/`): LLM-driven game NPC that proposes typed intents validated by hard game rules.

### CLI surface (`cmd/stoa/`)
- `stoa npc-run <scenario.json> --actor <id>` — one NPC reasoning cycle.

## Architecture
Clean Architecture, organized by feature slice. Dependencies point inward: Infrastructure → Interface Adapters → Use Cases → Domain. See [docs/architecture.md](docs/architecture.md) for the full layout, layer responsibilities, and the Stoa cycle.

## Critical Rules
- **LLM is infrastructure, not domain.** Business logic never imports an SDK.
- **Prompts hold judgment; code holds contracts.** If a rule can be a validator, it must not be only a prompt instruction.
- **Agents communicate through typed handoff objects**, never free-form text.
- **Errors feed context back to the LLM** for self-correction rather than blind retries.
- **Provider adapters only translate.** Prompt rendering and output decoding must be replaceable strategies; domain validation never lives in an LLM adapter.
- **Domain and agent are separate packages.** The domain package holds entities, validators, and port interfaces. The agent subpackage holds the LLM-driven loop and feature-specific prompt rendering. Imports point inward only: the domain imports neither the agent nor any LLM code, and only the agent package depends on `llm`.

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
- `llm.ReasoningEngine[TIntent]` returns `llm.ReasoningOutput[TIntent]`, a discriminated value carrying evidence, rationale, and either a typed intent (`Kind == ReasoningIntent`) or a list of tool calls (`Kind == ReasoningToolCalls`) — never both.
- Tools are registered on `harness/loop.Runner.Tools` as `loop.Tool{Spec, Handler}`. The loop forwards the specs as `ReasoningInput.Tools`; adapters translate them into provider-native function definitions, then route the model's `tool_calls` back through the handler whose `Spec.Name` matches.
- `llm.PromptRenderer` converts typed reasoning input into provider-neutral messages and never prescribes the response shape — structured outputs and native tool calls enforce that.
- `llm.Decoder[TIntent]` is used by adapters when the model returns content rather than a native tool call; the default `JSONDecoder` parses the canonical `{evidence, rationale, intent}` envelope.
- OpenAI code under `llm/openai/` must stay provider-specific: SDK calls, message translation, response-format selection (`json_schema` when `Config.IntentSchema` is set, `json_object` otherwise), tool translation, and provider error wrapping only.
