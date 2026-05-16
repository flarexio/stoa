# Stoa 🏛️

[正體中文](README.zh-TW.md)

> A stoa for AI agents — where knowing meets doing.

**Stoa** is a Go workshop for crafting production-grade AI agents. It's not a framework. It's a way of thinking about agents.

Built on two convictions:

1. **Agents are knowing meeting doing.** An LLM that only reasons is a chatbot. An agent acts, and its actions must be verifiable.

2. **Control what you can, let the LLM be the LLM.** Architecture, contracts, validation, and error handling are ours to master. The model's probabilistic nature is something we harness, not fight.

---

## Philosophy

Named after the Greek στοά — the covered colonnade beside the agora, where **Zeno of Citium** taught and **Stoicism** was born. A space where thinking and doing meet.

Two traditions, two thousand years apart, converge on the same principle:

- **Stoicism** (West): *dichotomy of control* — focus on what you can control, accept what you cannot.
- **Wang Yangming** (East): *知行合一 (unity of knowing and doing)* — knowledge that is not acted upon is not truly known.

Both speak directly to the craft of building AI agents today.

### Core beliefs

**Domain models are an agent's conscience.**
They collapse general capability into specific business judgment. A good domain model tells the LLM not just *what* to produce, but *what counts as valid* in your world.

**Harness is practice on the work itself.**
Stability comes from grinding against real tasks, not from clever prompts. We don't trust prompts to enforce business rules — we trust validators, types, and explicit contracts.

**Every agent is knowing-and-doing unified.**
Not just text generation. Verifiable action. If the agent can't act, it's not an agent. If we can't verify the action, we haven't finished the work.

**We control what we can.**
Architecture. Contracts. Validation. Error handling. These are deterministic and ours to master. The model's probabilistic nature is bounded, channeled, and harnessed — never fought.

---

## Why Stoa exists

Most AI agent tooling today falls into two camps:

- **Heavy frameworks** (LangChain, etc.) — they abstract for generality, which becomes a tax once you know what you want.
- **Bare SDK scripts** — fast to start, but without structure they collapse under production demands (error handling, multi-agent coordination, observability).

Stoa sits in between. It's not a framework you adopt; it's an architecture you follow. A set of patterns, contracts, and harness components you can read in an afternoon, modify in an evening, and understand fully.

The goal isn't to hide complexity. It's to **locate complexity where it belongs** — business rules in domain models, orchestration in use cases, LLM quirks in adapters.

---

## Design principles

- 🏛️ **Knowing and doing are one.** Reasoning without verified action is incomplete.
- 🔑 **Domain model comes before prompt.** Don't let LLM capabilities dictate your business model.
- 🔑 **Prompts hold judgment; code holds contracts.** If it can be a validator, it shouldn't be a prompt instruction.
- 🔑 **LLM is infrastructure, not domain.** It lives in the outer layer. Your business logic never imports an SDK.
- 🔑 **Contracts are structured, not textual.** Agents talk to each other through typed handoffs, not free-form text.
- 🔑 **Narrow and deep beats broad and shallow.** Pick a domain. Master it. Generalize later.
- 🔑 **Open adapters only when you must.** Don't architect for hypothetical needs.
- 🔑 **Practice on the work itself.** Real tasks reveal what thought experiments cannot.

---

## Architecture

Stoa follows **Clean Architecture** — dependencies flow inward. The LLM, frameworks, and external services live at the outermost layer. Your business logic doesn't know they exist.

```
┌─────────────────────────────────────────────────────┐
│  Framework / Infrastructure                         │
│  (LLM Provider, databases, external services)       │
│  ┌───────────────────────────────────────────────┐  │
│  │  Interface Adapters                           │  │
│  │  (LLM adapters, prompt templates, parsers)    │  │
│  │  ┌─────────────────────────────────────────┐  │  │
│  │  │  Use Cases                              │  │  │
│  │  │  (Agent task flows, orchestration)      │  │  │
│  │  │  ┌───────────────────────────────────┐  │  │  │
│  │  │  │  Domain                           │  │  │  │
│  │  │  │  (Pure business models & rules)   │  │  │  │
│  │  │  └───────────────────────────────────┘  │  │  │
│  │  └─────────────────────────────────────────┘  │  │
│  └───────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────┘
         ← dependencies flow this way ←
```

| Layer | Responsibility | Examples |
|-------|---------------|----------|
| **Domain** | Pure entities, rules, validators | Business entities and invariants |
| **Use Case** | Agent task flows, decision logic | Orchestration logic |
| **Adapter** | Translate between domain and infrastructure | LLM adapters, prompt templates |
| **Infrastructure** | Concrete SDKs, DBs, external tools | LLM Provider SDK, PostgreSQL |

See [`docs/architecture.md`](docs/architecture.md) for the full breakdown.

---

## Primary direction: game NPC harness

> **Stoa: A Domain-Validated Harness for LLM-Driven Game NPCs**

Stoa's primary focus is proving that an LLM-powered NPC can propose a typed intent, have that intent validated by hard game-domain rules, execute only after validation, and self-correct after structured feedback — all without game logic leaking into the LLM layer.

```text
world situation
→ LLM proposes NPCIntent (say, emotion, action)
→ world.Validator enforces game rules
→ executor mutates/observes world state
→ validation errors feed back as typed events for correction
```

The `world/` package owns game entities and rules (no LLM dependency). The `npc/` package owns the use-case loop. Provider adapters live in `llm/openai/` and are swappable.

A tavern scenario ships in `testdata/scenarios/tavern.json`: Mira is a cautious merchant who owns healing potions; the player has low reputation; north road has bandits. The NPC tests use an equivalent in-code fixture; the JSON file is the reference shape for future demos and loaders.

### Demo: run an NPC turn from the command line

`cmd/stoa` is a small CLI that loads a scenario JSON file, runs the same `npc.Agent` loop as the tests with a deterministic scripted reasoning engine, and prints a JSON report. No `OPENAI_API_KEY` or network access is required.

```bash
go run ./cmd/stoa npc-run testdata/scenarios/tavern.json --actor mira
```

The scripted engine intentionally proposes an invalid intent on its first turn (giving an item the actor does not own). The world validator rejects it, the loop feeds the rejection back as a typed event, and the engine corrects on the next turn. The final JSON report includes:

- `scenario` and `summary` from the scenario file
- `actor` and `task`
- `turns` taken, the final `intent`, and the resulting `observation`
- the full `events` trace and a `feedback` summary of any validation/execution errors

Use `--task` to override the in-world prompt and `--max-turns` to bound the loop.

---

## Example: bookkeeping agent

The accounting slice applies the same architecture to double-entry bookkeeping. A natural-language request is turned into a validated journal entry — the agent proposes a typed `JournalIntent`, the accounting domain validates it, and only a balanced, period-correct, account-valid entry is posted to the ledger.

```text
bookkeeping request
→ LLM proposes JournalIntent (accounts, amounts, period)
→ accounting.Validator enforces accounting invariants
→ a validated entry is published as a JournalPosted event and projected into the ledger
→ validation errors feed back as typed events for self-correction
```

`accounting/` owns the domain model — chart of accounts, periods, journal entries, and validation rules — with no LLM dependency. `bookkeeper/` owns the use-case loop and the feature-specific prompt renderer.

`cmd/stoa book-run` runs this loop from the command line; see [`docs/accounting.md`](docs/accounting.md) for the runnable demo and configuration.

---

## Conversational TUI

`stoa tui` is a [Bubble Tea](https://github.com/charmbracelet/bubbletea) terminal UI over the same reason → validate → execute loop. Instead of a one-shot JSON report, it streams each cycle event — model output, validation feedback, observation — as it happens, and keeps the session open for follow-up requests in the same run.

```bash
go run ./cmd/stoa tui testdata/scenarios/tavern.json testdata/accounting/aws_bill.json
```

Pass one or more scenario files: accounting scenarios become bookkeeper sessions, world scenarios become one npc session per actor. Choose an agent on the start screen, type a request, watch the loop unfold, and press `ctrl+c` to cancel a running turn or quit. The TUI is presentation only — it observes the harness loop through a `harness/loop.EventSink` and reuses the same composition as `book-run` / `npc-run`.

---

## Project layout

Stoa organizes code **by feature**, not by architectural layer. A feature is split into a domain package and an agent package so domain models remain independently importable while the agent loop stays explicit.

```
stoa/
├── cmd/
│   └── stoa/              # Demo CLI (npc-run, book-run, tui subcommands)
│       └── tui/           # Bubble Tea conversational UI (presentation only)
├── world/                 # Game domain: world state, actors, items, NPCIntent, validator
├── npc/                   # NPC use-case loop and prompt rendering
├── accounting/            # Accounting domain: ledger, accounts, periods, validator, events
├── bookkeeper/            # Bookkeeping agent loop, prompt rendering, event ports
├── persistence/           # LedgerRepository adapters (memory, postgres)
├── messaging/             # EventBus adapters (inproc, nats)
├── config/                # config.yaml loader for cmd/stoa
├── harness/
│   └── loop/              # Typed reason-validate-execute runner
├── llm/                   # Shared reasoning contracts and prompt rendering
│   └── openai/            # OpenAI provider adapter
├── testdata/
│   ├── scenarios/         # NPC scenario fixtures (e.g. tavern.json)
│   └── accounting/        # Bookkeeping scenario fixtures (e.g. aws_bill.json)
└── docs/
    └── architecture.md
```

Future features should follow the same shape: a domain package for business concepts and invariants, and an agent package for orchestration and feature-specific prompting. Provider adapters stay outside the feature package unless the feature genuinely owns that infrastructure.

### Why feature-based, not layer-based

Go idiom organizes packages by **what they provide**, not what they contain. A `models/`, `services/`, `repositories/` split scatters one business concept across multiple directories — changing one feature means hopping between folders. Feature packages keep related code together and let dependency direction be expressed through **interfaces**, not directory structure.

---

## Quickstart

> ⚠️ Stoa is in early development. APIs will change.

### Prerequisites

- Go 1.25+
- LLM Provider API key or OAuth

### Install

```bash
git clone https://github.com/flarexio/stoa.git
cd stoa
go mod download
```

### Run

`cmd/stoa` is a small CLI with `npc-run`, `book-run`, and `tui` subcommands. Bring up the local Postgres + NATS stack and apply the schema with [golang-migrate](https://github.com/golang-migrate/migrate):

```bash
docker compose up -d

migrate -path persistence/postgres/migrations \
  -database "postgres://stoa:stoa@localhost:5432/stoa?sslmode=disable" up
```

Point `config.yaml` at the `postgres` and `nats` backends — see `config.example.yaml` — then run a subcommand:

```bash
go run ./cmd/stoa book-run testdata/accounting/aws_bill.json \
  --request "Paid AWS bill 100 USD using company credit card"
```

An empty `config.yaml` instead selects the all-offline defaults — in-memory ledger, in-process bus, no services needed.

---

## What Stoa is not

- **Not a framework.** You read the code; you own the code.
- **Not a LangChain replacement.** Different category entirely.
- **Not general-purpose.** Start narrow. Generalize when patterns emerge.
- **Not magic.** Every decision is explicit. Every abstraction earns its place.

---

## Contributing

Stoa is currently a personal workshop project. Issues and discussions are welcome; PRs will be considered once the core architecture stabilizes.

If you're interested in the philosophy or approach, feel free to open a discussion.

---

## License

MIT — see [LICENSE](LICENSE).

---

## Acknowledgments

- **Zeno of Citium**, for teaching in the colonnade.
- **Wang Yangming (王陽明)**, for insisting that knowing and doing are one.
- **Anthropic's *Building Effective Agents***, for articulating why most agent code doesn't need a framework.

---

<p align="center">
  <i>Control what you can. Harness what you cannot. Let the work itself be the teacher.</i>
</p>
