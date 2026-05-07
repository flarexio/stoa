# Review Guidelines: `llm/openai/`

This package is the **OpenAI provider adapter**. It implements
`llm.ReasoningEngine[TIntent]` by translating Stoa's provider-neutral
contracts into OpenAI SDK calls and back. Apply these guidelines on top of
the root `REVIEW.md`.

## Hard rules

- **Adapters only translate.** This package may:
  - Convert `[]llm.Message` into `openai.ChatCompletionMessageParamUnion`.
  - Pick the response format (text vs. JSON object) based on the configured
    `Decoder`.
  - Call the SDK and wrap provider errors with context.
  - Hand the raw response string to the configured `Decoder`.
  It must NOT:
  - Validate `TIntent` against domain rules.
  - Render feature-specific prompts (use a `llm.PromptRenderer` from the
    feature package or `llm.DefaultPromptRenderer`).
  - Decode JSON itself when a `Decoder` is configured.
- **No domain or feature imports.** No `icd/`, no `coder/`. This package is
  generic over `TIntent` for a reason.
- **Configuration is provider-only.** `Config` should hold OpenAI concerns
  (API key, model, output format) plus the renderer and decoder ports. It
  must not grow domain knobs.
- **Error wrapping must preserve the SDK error.** Always use `%w` so callers
  can `errors.Is`/`errors.As` against provider error types.
- **API key handling must not log or echo the key.** Reading from
  `OPENAI_API_KEY` is acceptable; writing it anywhere is not.

## What to flag in a PR

- This package starting to import `github.com/flarexio/stoa/icd` or
  `github.com/flarexio/stoa/coder`.
- Inline JSON parsing that bypasses `llm.Decoder[TIntent]`.
- Hardcoded model names sneaking into call sites instead of `Config.Model`.
- Streaming, tool calling, or function calling features added without
  matching changes to the shared `llm` contract — those features need a
  contract decision before a provider implementation.
- Default model bumps without a note in the commit/PR explaining the
  cost/behavior change.
- The compile-time assertion `var _ llm.ReasoningEngine[struct{}] = ...`
  being removed; it is the cheapest guard against contract drift.

## Tests

- Adapter tests live in `adapter_test.go` and should use the SDK's
  HTTP-level test hooks or a fake transport, not a live API key.
- Live tests (`OPENAI_API_KEY` required) belong in feature integration
  tests, not here.
