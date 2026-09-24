# Local Minikube Agent state-machine validation

This overlay runs the benchmark-only E4 Runtime composition and a dedicated MySQL instance in the `stablepay-k8s-e2e` namespace. Runtime and MySQL use separate Deployments, ClusterIP Services, and worker nodes; MySQL state is retained on a PVC.

## Scope and evidence boundary

- `stablepay-multi` is a three-node Minikube cluster using Docker Desktop's Docker driver on one physical Windows host. It exercises Kubernetes scheduling, pod networking, Service DNS, readiness, and persistent storage, but it is not a physically distributed cluster or an ACK cluster.
- The Runtime binary is `commerce-runtime/cmd/e4-local-runtime`. Its Merchant, Payment, authorization, and Verification adapters are deterministic local mocks persisted to the isolated MySQL schema. It does not create a Payment Service client or a blockchain client, and submits no real chain transaction.
- This validates the Runtime payment Episode state machine and economic projections through Kubernetes. It does not claim that the six StablePay production microservices participated in this request path, and it does not test crash recovery.
- No ACK manifests are applied or changed by this overlay.

## Rebuild and deploy

Run from the repository root in PowerShell. The binary path below is invoked directly; Minikube does not need to be added to `PATH`.

```powershell
$env:MINIKUBE_HOME = 'D:\MyLab\projects\Stablepay\.local-run\minikube-home'
$env:DOCKER_CONFIG = 'D:\MyLab\projects\Stablepay\.local-run\docker-config'
$minikube = 'D:\DevTools\Minikube\minikube.exe'

& $minikube start -p stablepay-multi --driver=docker --nodes=3 --cpus=2 --memory=4096

$env:GOOS = 'linux'
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '0'
$env:GOCACHE = (Join-Path (Get-Location) '.local-run\gocache-e4')
$env:GOMODCACHE = (Join-Path (Get-Location) '.local-run\gomodcache-l2')
$env:GOSUMDB = 'off'
New-Item -ItemType Directory -Force '.local-run\minikube-evidence' | Out-Null
Push-Location commerce-runtime
try { go build -trimpath -o ..\.local-run\minikube-evidence\e4-local-runtime ./cmd/e4-local-runtime }
finally { Pop-Location }

$docker = 'C:\Users\Administrator\AppData\Local\Programs\DockerDesktop\resources\bin\docker.exe'
& $docker build --file infra-deployment\k8s\minikube\e4-local-image.Dockerfile --tag stablepay/e4-local-runtime:local .local-run\minikube-evidence
& $minikube image load -p stablepay-multi stablepay/e4-local-runtime:local
& $minikube image load -p stablepay-multi mysql:8.0

# Generate local-only credentials; never reuse .env or wallet credentials.
$dbPassword = [guid]::NewGuid().ToString('N') + [guid]::NewGuid().ToString('N')
$rootPassword = [guid]::NewGuid().ToString('N') + [guid]::NewGuid().ToString('N')
$apiToken = [guid]::NewGuid().ToString() + '-' + [guid]::NewGuid().ToString()
$null = & $minikube -p stablepay-multi kubectl -- create namespace stablepay-k8s-e2e --dry-run=client -o yaml | & $minikube -p stablepay-multi kubectl -- apply -f -
$secretArgs = @('--namespace=stablepay-k8s-e2e', '--from-literal=db-user=e4bench', "--from-literal=db-password=$dbPassword", "--from-literal=mysql-root-password=$rootPassword", "--from-literal=api-token=$apiToken", '--dry-run=client', '-o', 'yaml')
$null = & $minikube -p stablepay-multi kubectl -- create secret generic stablepay-e4-secrets @secretArgs | & $minikube -p stablepay-multi kubectl -- apply -f -
& $minikube -p stablepay-multi kubectl -- apply -f infra-deployment\k8s\minikube\e4-local.yaml
& $minikube -p stablepay-multi kubectl -- rollout status deployment/stablepay-e4-mysql -n stablepay-k8s-e2e
& $minikube -p stablepay-multi kubectl -- rollout status deployment/stablepay-e4-runtime -n stablepay-k8s-e2e
```

## Smoke and small load

In one terminal, keep a loopback-only port-forward running:

```powershell
& $minikube -p stablepay-multi kubectl -- port-forward -n stablepay-k8s-e2e svc/stablepay-e4-runtime 18091:8090 --address 127.0.0.1
```

In another terminal, load the local API token into process memory and run the bounded test:

```powershell
$encoded = & $minikube -p stablepay-multi kubectl -- get secret stablepay-e4-secrets -n stablepay-k8s-e2e -o jsonpath='{.data.api-token}'
$env:MINIKUBE_E4_API_TOKEN = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String([string]$encoded))
.\scripts\benchmark-minikube-e4.ps1 -Count 10 -Concurrency 3
```

The runner assigns a fresh idempotency key to each Episode, polls to terminal state, and records each persisted state timeline. `scripts/audit-minikube-e4.ps1` validates the per-Episode MySQL audit rows for exactly one PaymentIntent and one `PAYMENT_SETTLED`, consistent budget projection, `FULFILLED`/`COMPLETED` terminal state, and zero duplicates/orphans/stuck Episodes. It explicitly reports crash recovery as `NOT_TESTED` when no restart was injected.

The validated run's manifest, load results, and SQL audit are retained under `benchmarks/minikube-local-20260925/`; the working `.local-run` copy also contains the local execution traces. Do not delete the profile or its PVC if the retained cluster state is needed.
