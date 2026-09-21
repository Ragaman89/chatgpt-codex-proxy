# Retired deployment

The laegerfeld deployment is replaced by `Ragaman89/codex-orchestrator` and
`Ragaman89/k8s-codex-orchestrator`.

The replacement owns Codex five-hour/weekly quota monitoring, banked reset
metadata, Home Assistant discovery and quota-aware task resume. Request proxying
and prompt compression are no longer in the task execution path.

Both deployments are deliberately scaled to zero instead of deleted. Keep the
namespace, Secret and PVC for one rollback window. Do not run the proxy and the
new quota manager concurrently: both can rotate the same OAuth refresh token.

Rollback requires stopping `codex-quota-manager` first, restoring the proxy's
credential state, then scaling only `codex-proxy` (and optionally its compressor)
back to one.
