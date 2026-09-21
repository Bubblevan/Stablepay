# S11 live-local evidence

Run the reproducible isolated external-client suite with:

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-s11-live-local.ps1
```

The suite creates a fresh Runtime, MemoryStore, catalog fixture, and fault
controller for every expanded trial, then invokes the public HTTP/MCP/CLI
surface. It uses deterministic local merchant, payment, verification, memory,
and recovery-provider adapters, so the run is local `live` evidence rather
than a claim that DeepSeek, payment-service, or Devnet was called. No
credentials or raw provider payloads are written.

Generated outputs are intentionally ignored under `.local-run/s11-live-local`:
`episode_results.jsonl`, `grade_results.jsonl`, `metrics.json`, and `report.md`.
Keep API keys, DSNs, private keys, and artifact bodies out of the directory.
