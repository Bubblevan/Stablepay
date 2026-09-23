# E3 Runtime / CloudWeGo load benchmark

`runtime_control_plane.js` is the E3A k6 workload. The runner executes it in
the Docker image `grafana/k6` rather than requiring a host-side k6 install; it
is not a Go client loop. HTTP API request throughput and completed episode
throughput are reported as separate metrics.

The default matrix is `1,10,25,50,100,200`. Each tier has a warmup (15 seconds
by default) followed by a 60-second measurement. The run manifest records the
Docker server version, image tag and resolved RepoDigest, Docker network mode,
the container-visible base URL, and the exact matrix/durations.
It also records the Runtime source SHA and MySQL pool settings used by the
local Runtime process.

On Docker Desktop, a host URL such as `http://127.0.0.1:8090` is translated to
`http://host.docker.internal:8090` inside the k6 container. Override it with
`-BaseUrl` if the Runtime is reachable at another address.

```powershell
.\scripts\benchmark-e3-load.ps1 `
  -BaseUrl http://127.0.0.1:8090 `
  -K6Image grafana/k6:latest `
  -DockerNetworkMode bridge
```

The script reads only `API_TOKEN` or `COMMERCE_RUNTIME_API_TOKEN` from the
optional ignored `.env`; it never treats `LLM_API_KEY` as the Runtime HTTP
authorization token. The token is passed to Docker through the inherited
environment and is not written to the manifest or logs.

The workload uses the real Runtime HTTP boundary with deterministic local
dependencies. It does not enable the real DeepSeek provider or Solana Devnet.
E3B is only reported when an existing safe Hertz/Kitex/Thrift/MySQL/RocketMQ
path is explicitly configured; the runner never invents a benchmark-only
CloudWeGo service or spends Devnet assets.

`collection_errors` counts create responses without an episode ID and failed
status polls (non-200 or invalid JSON). A failed status poll ends polling for
that episode rather than retrying an unusable response up to 100 times.
`orphaned_episodes` counts accepted episodes not observed in a terminal state
within the polling window; it is a benchmark-window measure, not a database
orphan audit.
