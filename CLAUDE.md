# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Stoa is a Go workshop for building production-grade AI agents. It is not a framework -- it is an architecture and set of patterns for crafting agents that act and verify, not just reason. The name comes from the Greek στοά (covered colonnade), connecting Stoic philosophy (control what you can) with Wang Yangming's 知行合一 (unity of knowing and doing).

The first agent collection targets IIoT (Industrial IoT) pre-deployment workflows:
- **Survey Agent** -- on-site investigation, requirements gathering, equipment inventory
- **Quote Agent** -- pricing based on survey results and business rules
- **Connection Agent** -- real-world connectivity testing against quoted commitments

These agents coordinate through typed handoff contracts, not free-form text.

## Build and Test Commands

```bash
go build ./...          # build all packages
go test ./...           # run all tests
go test ./survey/...    # run tests for a single package
go test -run TestName ./survey/  # run a single test
go run ./cmd/survey-agent        # run the survey agent
```

Requires `ANTHROPIC_API_KEY` environment variable.

## Architecture

Stoa follows **Clean Architecture** with dependencies flowing inward. Code is organized **by feature** (not by layer) -- each agent package contains its own domain models, flow logic, prompts, and adapters.

Key layers (outer to inner):
1. **Infrastructure** -- Anthropic Go SDK, databases, external services
2. **Interface Adapters** -- LLM adapters, prompt templates, output parsers
3. **Use Cases** -- Agent task flows, orchestration (defines interfaces like `ReasoningEngine`)
4. **Domain** -- Pure business entities, rules, validators (no external dependencies)

Critical architectural rules:
- **LLM is infrastructure, not domain.** Business logic never imports an SDK.
- **Prompts hold judgment; code holds contracts.** If a rule can be a validator, it must not be only a prompt instruction.
- **Agents communicate through typed handoff objects** (e.g., `SurveyToQuoteHandoff`), never free-form text or conversation history dumps.
- **Use case layer defines interfaces** -- it doesn't know if the implementation is LLM or rules engine.

## Design Decisions

- No heavy frameworks (LangChain, LangGraph). The agent loop is kept short and fully understood.
- Go is chosen for its type system (domain-model-as-contract), implicit interface satisfaction (Clean Architecture), goroutines (parallel tool calls), and single-binary deployment (IIoT field use).
- Python is a fallback adapter only (subprocess > HTTP/gRPC > message queue), with interfaces defined on the Go side.
- Harness engineering (validation, retry with context, circuit breakers) is orthogonal to frameworks and required for production.
- Error handling feeds context back to the LLM for self-correction rather than blind retries.

## Development Status

Early development. The project is following a phased approach:
1. Domain model extraction (Survey/Quote/ConnectionTest structs and validators)
2. Handoff contract design between agents
3. Clean Architecture implementation per agent (starting with Survey)
4. Golden set testing (5-10 real cases per agent)
5. Agent collaboration
