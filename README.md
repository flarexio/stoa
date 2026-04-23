# Stoa 🏛️

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

## Project layout

Stoa organizes code **by feature**, not by architectural layer. Each agent lives in its own package with everything it needs — domain models, flow logic, prompts, and adapters — in one place.

```
stoa/
├── harness/               # Cross-agent reusable components
│   ├── validator/
│   ├── retry/
│   └── handoff/
├── llm/                   # Shared LLM client abstractions
├── tools/                 # Shared tool definitions
├── cmd/                   # Executable entry points (if multiple)
├── testdata/              # Golden sets for agent evaluation
└── docs/
    ├── architecture.md
    ├── philosophy.md
    └── decisions/         # Architecture Decision Records (ADRs)
```

Agent packages are added per-agent as development progresses. Each follows the same shape: `model.go` for domain types & invariants, `agent.go` for the use case / task flow, `prompt.go` for prompt templates, `llm.go` for the LLM adapter, and `*_test.go` alongside.

### Why feature-based, not layer-based

Go idiom organizes packages by **what they provide**, not what they contain. A `models/`, `services/`, `repositories/` split scatters one business concept across multiple directories — changing one feature means hopping between folders. Feature packages keep related code together and let dependency direction be expressed through **interfaces**, not directory structure.

---

## Quickstart

> ⚠️ Stoa is in early development. APIs will change.

### Prerequisites

- Go 1.22+
- LLM Provider API key or OAuth

### Install

```bash
git clone https://github.com/flarexio/stoa.git
cd stoa
go mod download
```

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
