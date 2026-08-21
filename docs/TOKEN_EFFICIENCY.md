# Token-efficiency architecture

## Objective

The gateway reduces repeated or oversized input without changing the OpenAI-
compatible API. Availability and correctness take priority over token savings.
The upstream Codex endpoint is private and undocumented, so every optional
optimization must degrade safely.

## Request path

```text
client
  -> protocol normalization
  -> safe token optimization
       -> preserve/derive prompt cache identity
       -> optional LLMLingua-2 historical-prose compression
       -> bounded exact-result cache
       -> native context-management compaction request
  -> continuation/account resolution
  -> Codex Responses API
```

Native facilities remain preferable to lossy compression:

- `prompt_cache_key`, `prompt_cache_options`, and
  `prompt_cache_breakpoint` pass through unchanged;
- repeated prefixes can become `previous_response_id` continuations;
- `context_management` can request server-side compaction at a configured
  threshold;
- `POST /v1/responses/compact` remains directly available;
- after an upstream compaction item, replay history retains only the latest
  encrypted compaction item instead of the expanded prefix.

## Safe learned compression

The sidecar API accepts only a batch of text and a target ratio. It loads the
revision-pinned multilingual LLMLingua-2 BERT model from the image and performs
CPU inference serially. Neither service logs request content.
The image also preloads the `cl100k_base` Tiktoken vocabulary into `/models/tiktoken`; runtime startup therefore performs no external model or tokenizer downloads.

The gateway considers only natural-language text in user or assistant messages
strictly before the latest user message. It excludes:

- system/developer instructions and the current request;
- tools, tool choices, schemas, tool calls, and tool output;
- valid JSON, fenced code, and tool-like markup;
- images, files, reasoning summaries, and encrypted state;
- any text carrying an explicit prompt-cache breakpoint;
- requests and individual texts below configurable token thresholds.

If compression fails, times out, returns an empty value, or expands the input,
the original text is used. Compression results are cached by SHA-256 of model
policy version, ratio, and exact input. The LRU is memory-only and stores at
most the configured number of values; there is deliberately no semantic
response cache.

## Configuration

| Variable | Default | Purpose |
|---|---:|---|
| `TOKEN_OPTIMIZATION_ENABLED` | `false` | Enables the optimizer |
| `TOKEN_COMPRESSOR_URL` | empty | LLMLingua service base URL |
| `TOKEN_COMPRESSOR_TIMEOUT` | `3s` | Per-batch timeout |
| `TOKEN_COMPRESSION_MIN_REQUEST_TOKENS` | `6000` | Minimum total request size |
| `TOKEN_COMPRESSION_MIN_TEXT_TOKENS` | `800` | Minimum candidate text size |
| `TOKEN_COMPRESSION_TARGET_RATIO` | `0.5` | Requested retained-token ratio |
| `TOKEN_COMPRESSION_CACHE_ENTRIES` | `256` | Exact-result LRU capacity |
| `TOKEN_AUTO_COMPACT_THRESHOLD` | `0` | Native compaction threshold; zero disables injection |

The response headers `X-Token-Optimization`, `X-Original-Input-Tokens`,
`X-Optimized-Input-Tokens`, and `X-Compression-Cache-Hits` expose behavior
without exposing text. Structured JSON logs report the same numeric fields.

## Model and licensing

The container pins
`microsoft/llmlingua-2-bert-base-multilingual-cased-meetingbank` to revision
`5f0c82792b7ea14c6484e015b6a072009496b7f2`. Its local path deliberately keeps
the `bert-base-multilingual-cased` marker required by LLMLingua 0.2.2's token
boundary logic. The selected model supports German
and English and is distributed under Apache-2.0; LLMLingua itself is MIT.

## Operations

The compressor is labeled `temporary: "true"` and scheduled to the temporary
amd64 node. While it is absent, gateway health remains green and requests are
forwarded without learned compression. Monitor the optimization status header
or JSON warning `token optimization degraded` to distinguish this planned
mode from inference errors while the compressor Pod is Ready.

For regression tests, compare representative prompts with optimization on and
off, including tool-heavy, JSON, code, German prose, explicit cache breakpoints,
and continuations. Keep the feature disabled for exact-quotation, legal, or
safety-critical histories.
