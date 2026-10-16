# local-telemetry — local OTel dev stack

Standalone dev helper for ODG telemetry. **Not** wired into `task test-e2e`; it runs against
the *retained* kind clusters that `--keep-clusters` leaves behind.

```
backends (your Mac, Docker)          workload-odg kind cluster
┌──────────────────────────┐         ┌────────────────────────────────────────┐
│ prometheus   :9090       │──scrape──► localhost:8889 (kubectl port-forward) │──► otel-collector :8889
│ opensearch   :9200       │◄──push── otel-collector (daemonset)             │
│ dashboards   :5601       │         │   filelog / k8sattributes              │
└──────────────────────────┘         │   metrics-operator (counts CRs)        │
                                     └────────────────────────────────────────┘
```

Metrics flow by **pull**: Prometheus on the Mac scrapes the collector's `:8889` through a
`kubectl port-forward` opened by `telemetry.sh` — no Docker Desktop host-reachability needed.
Logs/traces still push to OpenSearch via `host.docker.internal` (Docker Desktop's host gateway).
The metrics-operator counts ODG resources (see `metrics-operator-cr.yaml`) and pushes OTLP to
the collector (`:4317`); the collector re-exports them on `:8889`.

## Commands

```bash
cd local-telemetry
./telemetry.sh up       # start backends + install collector/metrics-operator + apply CRs + data view
./telemetry.sh status   # is anything up?
./telemetry.sh down     # remove collector/CRs, stop backends (data kept)
./telemetry.sh reset    # wipe all data, start fresh
```

## Prerequisites

`task test-e2e -- --keep-clusters` must have run and still be alive (kubeconfig is auto-discovered
from `kind get clusters`).

## Prometheus — grouping queries

Raw `odg_pods_running` returns one series per pod (~26). Group to make it readable.
Paste into the **Graph** tab at http://localhost:9090:

| Query | Result |
|---|---|
| `count(odg_pods_running)` | one number: total pods in odg-system |
| `count by (pod)(odg_pods_running)` | one line per pod |
| `sum by (pod)(odg_pods_running)` | pods, summed (here still 1 each) |

Host metrics come per-CPU-core (`cpu` label). Avg/max to collapse:

| Query | Result |
|---|---|
| `avg(system_cpu_utilization_ratio{state="user"})` | mean user-CPU across cores |
| `max(system_cpu_utilization_ratio)` | hottest core % |
| `avg by (cpu)(system_cpu_utilization_ratio)` | one line per core |
| `avg(system_memory_utilization_ratio)` | host memory usage ratio |

## Dashboards — data view auto-created

`up` pre-creates the **`logs-otel-v1`** data view (index pattern, time field `@timestamp`) via the
OpenSearch Dashboards API — no manual setup. Just open http://localhost:5601 → menu ☰ → **Discover**
and pick `logs-otel-v1`.

Useful filters (search bar):
- `resource.k8s.deployment.name: "delivery-service"` — one component
- `resource.k8s.pod.name: "otel-collector-*"` — collector's own logs
- `body: "ERROR"` — error lines

Fields available: `resource.k8s.{namespace,pod,deployment,container}.name`, `body`, `@timestamp`.

Raw API (no UI): `curl -s localhost:9200/logs-otel-v1/_search`

## Verify data lands

- Prometheus: http://localhost:9090 → **Status → Targets** shows `otel-collector` **UP**
  once the port-forward is live. Series appear in **Graph** within ~1 min of `up`.
- Dashboards Discover (`logs-otel-v1`) shows logs within ~30s of `up`.

## Verify each hop of the metric pipeline

Path: `Metric / ManagedMetric CR` → metrics-operator evaluates + builds OTLP → pushes to
collector `:4317` → collector serves on `:8889` → Prometheus scrapes.

| # | Check | Command |
|---|---|---|
| 1 | CR Ready + has a value (operator evaluated the query) | `kubectl --kubeconfig=/tmp/odg-telemetry-wl.kubeconfig -n telemetry-local get metric odg-pods-running` — expect `READY=True VALUE=<n>` |
| 2 | operator exports without errors | `kubectl --kubeconfig=/tmp/odg-telemetry-wl.kubeconfig -n telemetry-local logs deploy/metrics-operator-local \| grep -i "failed to export"` — expect no output |
| 3 | collector serves the metric on :8889 | `curl -s localhost:8889/metrics \| grep odg_pods_running` — expect series lines |
| 4 | Prometheus scraped it | Graph tab → `odg_pods_running` — expect values per pod |

ManagedMetrics (created during e2e for the MCP) verify the same way:
`kubectl get managedmetrics.metrics.open-control-plane.io -A`.

## Notes

- Log scope = pods in `odg-system` only (filelog `include` glob + k8sattributes namespace filter).
- Backends run with security disabled (lab mode). SAP/BTP production uses mTLS — see
  `~/code/discussions/12-otel/04-btp-metrics-view.md`.
