# Upstream analysis and fork strategy

## Scope and revision

The fork started from `10/chatgpt-codex-proxy` at upstream commit
`89973f46dc565874527520f822f1971a9c6a5652`. The project is MIT-licensed and
implemented as a single Go service. It exposes OpenAI Chat Completions,
Responses, Images, and Anthropic Messages APIs while calling the private
`chatgpt.com/backend-api/codex/*` endpoints with ChatGPT device-login accounts.

## Existing architecture

The request flow is deliberately compact:

1. Gin validates API-key authentication and decodes the public protocol.
2. OpenAI or Anthropic adapters normalize requests into a common turn model.
3. continuation logic resolves explicit or implicit replay and account affinity;
4. a quota-aware account manager selects a ready account;
5. Codex HTTP/SSE or WebSocket clients call the private backend;
6. an accumulator translates upstream events back to the client protocol.

Persistent state is a JSON account store and a model-cache file. Continuations
and Anthropic replay state are bounded by a TTL and live only in process memory.
The proxy already forwards cached-token usage and supports the native compact
endpoint.

## Token-saving behavior found upstream

The upstream implementation already contains useful mechanisms that should be
preserved rather than replaced:

- explicit `prompt_cache_key` passthrough;
- deterministic cache-key derivation from stable request prefixes;
- implicit prefix-replay detection and conversion to `previous_response_id`;
- explicit continuation pinned to the account that created the response;
- `POST /v1/responses/compact` passthrough;
- cached-input and reasoning-token usage translation;
- cached model/quota metadata.

The upstream cache is not a semantic response cache. Conversation continuation
data is in memory and expires after the configured TTL, which limits persisted
prompt exposure but loses reuse on restart.

## Fork additions

This fork keeps the protocol and account layers intact and inserts a separate,
fail-open optimizer after normalization. It adds:

- `prompt_cache_options` and explicit content cache-breakpoint passthrough;
- native automatic compaction configuration;
- replay-history truncation after a valid upstream compaction item;
- safe historical-prose selection and O200k token accounting;
- an exact, bounded, in-memory compression-result LRU;
- an isolated multilingual LLMLingua-2 CPU service;
- optimization metrics through headers and structured logs;
- hardened multi-architecture containers and k3s manifests.

The learned service is intentionally not embedded in Go. It can run on the
temporary amd64 node, restart independently, and disappear without making the
gateway unavailable. The Go gateway remains small enough for the always-on
arm64 nodes.

## Risks and controls

The decisive operational risk is the private and undocumented upstream API.
OpenAI may change paths, payload fields, authentication, or availability
without notice. This fork therefore avoids invasive translation changes and
must regularly rebase or merge upstream releases with the full compatibility
suite.

Learned compression is lossy. Selection excludes instructions, current intent,
structured/tool data, code, media, reasoning state, and explicit cache
boundaries. Calls time out and fail open. These controls reduce but do not
eliminate semantic risk.

The public endpoint must remain internal-only and protected by a long random
API key. Device-login tokens in the data volume are confidential. Payload debug
logging stays disabled in production.

## Upstream maintenance

Use `upstream` for the original repository and `origin` for the family fork:

```bash
git fetch upstream
git switch main
git merge --ff-only upstream/main
```

Bring upstream changes into a feature branch, run all Go/Python/container tests,
and specifically retest streaming, continuations, prompt-cache fields,
compaction, device login, and one live request before merging.
