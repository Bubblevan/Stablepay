# E3 Runtime / CloudWeGo load benchmark

`runtime_control_plane.js` is the E3A k6 workload. The runner executes it in
the Docker image `grafana/k6` rather than requiring a host-side k6 install; it
is not a Go client loop. HTTP API request throughput and completed episode
throughput are reported as separate metrics.

The default matrix is `1,10,25,50,100,200`. Each tier has a warmup (15 seconds
by default) followed by a 60-second measurement. The run manifest records the
Docker server version, image tag and resolved RepoDigest, Docker network mode,
the container-visible base URL, and the exact matrix/durations.
It also records the Runtime source SHA, MySQL pool settings, database placement,
and Kubernetes profile when supplied.

On Docker Desktop, a host URL such as `http://127.0.0.1:8090` is translated to
`http://host.docker.internal:8090` inside the k6 container. Override it with
`-BaseUrl` if the Runtime is reachable at another address.

```powershell
$runDir = Join-Path .\.local-run\resume-benchmark\e3-load ("run-" + [DateTime]::UtcNow.ToString("yyyyMMddTHHmmssZ"))
.\scripts\benchmark-e3-load.ps1 `
  -OutputRoot $runDir `
  -BaseUrl http://127.0.0.1:8090 `
  -ConfirmDeterministicLocalDependencies `
  -StopOnThresholdFailure `
  -RuntimeDatabasePlacement isolated-kubernetes `
  -KubernetesProfile stablepay-six `
  -K6Image grafana/k6:latest `
  -DockerNetworkMode bridge
```

The runner will not pull the k6 image by default. It uses a locally cached image
or exits with `NOT RUN`; pass `-PullK6Image` only when you explicitly approve a
Docker image download. It also refuses a non-empty `OutputRoot` rather than
overwriting prior evidence; choose a new per-run directory.
For resource-sensitive runs, `-StopOnThresholdFailure` finalizes a stopped-early
result after the first actual k6 threshold violation or runner/evidence failure
instead of automatically starting higher concurrency tiers. The manifest
separates `thresholds_failed` (read from k6 summary flags) from `process_failed`
(non-zero exit or missing/unreadable evidence), and records
`stopped_early_reason`; a non-zero exit alone is not mislabeled as a threshold
violation.

Before starting k6, the runner records a read-only host/disk resource snapshot.
For `-RuntimeDatabasePlacement isolated-kubernetes`, it additionally requires
the explicitly named six-node kubectl context to have all six nodes Ready and
Docker memory statistics for every node container, plus cgroup memory headroom
inside the isolated E3 MySQL container. MySQL reports both raw cgroup usage and
effective usage after subtracting `inactive_file` cache (bounded by current
usage); the `0.1 GiB` safety threshold applies to effective headroom. The run
is marked `NOT RUN` without starting k6 if telemetry is unavailable, MySQL has
under 0.1 GiB of effective headroom, host free RAM is below 8 GiB,
the Windows system drive has under 10 GiB free, the output drive has under
20 GiB free, or any node has under 0.5 GiB memory headroom. This guard does not
install a metrics server or pull images; resolve resource/telemetry blockers
before retrying with a new output directory. After each completed concurrency
tier, it captures another resource snapshot and stops higher tiers with status
`STOPPED_EARLY_AFTER_RESOURCE_GUARD` if any threshold is crossed; these checks
are recorded in both the manifest and metrics.

Validate the summary parser and threshold classification without running Docker
or k6:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-benchmark-e3-load.ps1
```

`/readyz` does not identify whether the target Runtime can cause a real payment.
The required `-ConfirmDeterministicLocalDependencies` flag is an operator
attestation: use it only after verifying the target cannot submit Devnet or
provider transactions. Each k6 iteration creates a new Episode, so never aim
this matrix at a real-payment Runtime without first designing an explicit,
bounded payment budget and a separate workload.
The current request body permits up to `1,000,000` USDC minor units (1 USDC)
per Episode; that is a per-Episode ceiling, not an aggregate run-spend cap.

The script reads only `API_TOKEN` or `COMMERCE_RUNTIME_API_TOKEN` from the
optional ignored `.env`; it never treats `LLM_API_KEY` as the Runtime HTTP
authorization token. The token is passed to Docker through the inherited
environment and is not written to the manifest or logs.

The workload uses the real Runtime HTTP boundary with deterministic local
dependencies. It does not enable the real DeepSeek provider or Solana Devnet.
E3B is only reported when an existing safe Hertz/Kitex/Thrift/MySQL/RocketMQ
path is explicitly configured; the runner never invents a benchmark-only
CloudWeGo service or spends Devnet assets.

After an isolated six-node Minikube run, save the read-only database audit with:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\audit-e3-local-load.ps1 `
  -RunDirectory .\.local-run\<e3-run-directory>
```

The audit refuses a non-`stablepay-six` context or a namespace lacking the
`deterministic-local-only` payment-boundary label, does not modify MySQL, and
binds `audit-summary.json` to the run manifest and metrics hashes.

`collection_errors` counts create responses without an episode ID and failed
status polls (non-200 or invalid JSON). A failed status poll ends polling for
that episode rather than retrying an unusable response up to 100 times.
`orphaned_episodes` counts accepted episodes not observed in a terminal state
within the polling window; it is a benchmark-window measure, not a database
orphan audit. Every request and episode is tagged as warmup, measurement, or
rampdown. Measurement-only thresholds cover the steady window, and the runner
records its exact 60-second HTTP and completed-episode rates, status counts,
and p95 latency in `metrics.json` and `report.md`.
