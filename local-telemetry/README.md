# local-telemetry — local OTel dev stack for e2e-kept clusters

Backends run on your Mac (docker compose); the in-cluster piece is the
**odg-observability `telemetry` chart** — collector daemonset, metrics-operator,
Metric CRs, NetworkPolicies — installed straight onto the workload kind cluster
that `task test-e2e -- --keep-clusters` leaves behind.

```
backends (your Mac, Docker)          workload-odg kind cluster
┌──────────────────────────┐         ┌─────────────────────────────────────────┐
│ prometheus   :9090       │──scrape──► localhost:8889 (kubectl port-forward)  │──► collector :8889
│ opensearch   :9200       │◄──push─── otel-collector (daemonset)              │
│ dashboards   :5601       │          │   filelog + k8sattributes + hostmetrics│
└──────────────────────────┘          │   metrics-operator → k8s_pods_running  │
                                      └─────────────────────────────────────────┘
```

## Commands

```bash
./telemetry.sh up       # backends + helm install + port-forward
./telemetry.sh status
./telemetry.sh down     # helm uninstall + stop backends (data kept)
./telemetry.sh reset    # wipe data, fresh up
```

Prereq: `task test-e2e -- --keep-clusters` has run and the `workload-odg*` kind
cluster is still alive (kubeconfig auto-discovered via `kind get clusters`).

The chart comes from the sibling `odg-observability` checkout. Override the path
with `ODG_OBSERVABILITY=/some/path ./telemetry.sh up`.

## What the chart gives you here

- `backend: opensearch` — collector pushes logs+traces to
  `host.docker.internal:9200` (the mac opensearch container)
- `observedNamespace: odg-system` — filelog scope + k8sattributes watch
- `annotationKey: ""` — **all** odg-system pod logs flow (dev mode; production
  uses the `otel.io/pod-logs=true` annotation gate)
- metric `k8s_pods_running` — value per `resource_namespace` label (see
  `odg-observability/charts/telemetry/templates/metrics.yaml`)

## Verify

| Hop | Command |
|---|---|
| Logs landed | `curl -s localhost:9200/logs-otel-v1/_count` |
| Metric CR evaluated | `kubectl --kubeconfig=/tmp/odg-telemetry-wl.kubeconfig -n odg-system get metric` — READY=True |
| Collector serves :8889 | `curl -s localhost:8889/metrics \| grep k8s_pods_running` |
| Prometheus scraped | http://localhost:9090 → Status → Targets → `otel-collector` UP |

## PromQL

```promql
k8s_pods_running                                        # per-namespace series
sum(k8s_pods_running)                                   # cluster-wide total
k8s_pods_running{resource_namespace="odg-system"}       # just odg components
avg(system_cpu_utilization_ratio)                       # mean CPU across cores
```

Note: value = pod count per `resource_namespace` directly (no `count()` needed) —
see `~/code/github.com/metrics-operator/docs/dimensions-configuration.md`.

## Notes

- Backends run with security plugins disabled (lab mode).
- The old ConfigMap-overlay / ProviderConfig-patch flow is gone — the chart
  subsumes it. (`telemetry-values.yaml`, `metrics-operator-cr.yaml` deleted.)
