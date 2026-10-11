# Six-node E3 resource-headroom remediation

Prepared from the read-only 2026-10-11 Docker/cgroup snapshot. This patch set is
**not applied**. It preserves all six Kubernetes nodes and Deployments, but
will roll out the Broker, NameServer, and Verification Service. Do not use it while a
payment or verification flow is in flight. The Verification Deployment uses
a no-surge rolling update to avoid overlapping consumers; expect a short
verification-service interruption.

The observed `m03` memory was 1.893/2 GiB. Broker, NameServer, and Verification
used about 0.92 GiB, 0.74 GiB, and 25 MiB respectively, and had no resource
requests/limits. The fragments move the NameServer to `m06`, Verification to
`m05`, and declare requests/limits. They are strategic-merge patch fragments,
not full Deployment manifests; `kubectl apply -f` is incorrect for these files.
The Broker and Verification Service patches set `maxSurge: 0` and
`maxUnavailable: 1`, so old singleton processes stop before replacements start
and broker/consumer instances do not overlap.

From the repository root, validate the exact target objects first:

```powershell
$minikube = 'D:\DevTools\Minikube\minikube.exe'
& $minikube -p stablepay-six kubectl -- patch deployment stablepay-rocketmq-broker -n stablepay-k8s-e2e --type=strategic --patch-file .\benchmarks\minikube-six-node-devnet\resource-headroom-patches\rocketmq-broker.patch.yaml --dry-run=server
& $minikube -p stablepay-six kubectl -- patch deployment stablepay-rocketmq-nameserver -n stablepay-k8s-e2e --type=strategic --patch-file .\benchmarks\minikube-six-node-devnet\resource-headroom-patches\rocketmq-nameserver.patch.yaml --dry-run=server
& $minikube -p stablepay-six kubectl -- patch deployment stablepay-verification-service -n stablepay-k8s-e2e --type=strategic --patch-file .\benchmarks\minikube-six-node-devnet\resource-headroom-patches\verification-service.patch.yaml --dry-run=server
```

Only after a quiet maintenance window and an explicit go-ahead, apply each
validated patch by removing `--dry-run=server`. Then wait for all three
Deployments to become Ready, confirm all 12 application Deployments remain
Ready, and rerun the resource preflight. Do not start the remaining E3 tiers
unless every node has at least 0.5 GiB headroom, the isolated MySQL has at least
0.1 GiB effective headroom after reclaimable cache, and host/system-drive
preflight passes. This remediation is not itself load-test evidence.
