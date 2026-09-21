# S11 live-local evidence

Run the reproducible external-client smoke with:

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-s11-live-local.ps1
```

The script starts the public Runtime HTTP surface and invokes the external
`stablepay-agent-eval` client. It uses deterministic local merchant, payment,
verification, memory, and recovery-provider adapters, so the run is local
`live` evidence rather than a claim that DeepSeek, payment-service, or Devnet
was called. No credentials or raw provider payloads are written.

Generated outputs are intentionally ignored under `.local-run/s11-live-local`:
`episode_results.jsonl`, `grade_results.jsonl`, `metrics.json`, and `report.md`.
Keep API keys, DSNs, private keys, and artifact bodies out of the directory.
