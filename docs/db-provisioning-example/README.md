# DB Provisioning Example

End-to-end walkthrough for provisioning a managed PostgreSQL database on BTP
via Crossplane, starting from a fresh OCP project. See ADR-004, ADR-005, and
ADR-006 for the decisions behind this approach.

## Prerequisites

- Access to an OCP onboarding cluster (Canary V2)
- A BTP technical user with CIS and service account credentials
- `kubectl` configured against the onboarding cluster

## Steps

| # | File | Cluster | What it does |
|---|------|---------|--------------|
| 1 | `01-project.yaml` | Onboarding | Create an OCP Project |
| 2 | `02-workspace.yaml` | Onboarding | Create a Workspace (`dev`) inside the project |
| 3a | `03a-control-plane.yaml` | Onboarding | Order a ControlPlane for the workspace |
| 3b | `03b-control-plane-kubeconfig.sh` | Onboarding | Download the kubeconfig for the new ControlPlane |
| 4 | `04-crossplane-provider.yaml` | Onboarding | Order the BTP Crossplane provider for the ControlPlane |
| 5 | `05-btp-secrets.yaml` | ControlPlane | Create CIS and service account secrets |
| 6 | `06-btp-providerconfig.yaml` | ControlPlane | Configure the Crossplane provider with your global account |
| 7 | `07-subaccount.yaml` | ControlPlane | Create a BTP subaccount |
| 8 | `08-entitlement.yaml` | ControlPlane | Entitle the subaccount for `postgresql-db` (storage + standard plans) |
| 9 | `09-service-manager.yaml` | ControlPlane | Create a ServiceManager in the subaccount |
| 10 | `10-service-instance.yaml` | ControlPlane | Provision the PostgreSQL instance |

Switch `kubectl` context to the ControlPlane after step 3b and keep it there
for steps 5–10.

## Known Issues

- **Provider BTP v2 is upcoming** — these manifests use v1; the API may change.
- **Status propagation bug** — in testing, the BTP instance reached `Ready` in
  BTP but the Kubernetes `ServiceInstance` never transitioned to `Ready`,
  leaving credential binding incomplete. Check the provider version and events
  if this happens. See note below
- **AWS RDS port** — RDS uses a non-standard port; use the port from the
  connection secret rather than assuming 5432.
- **SCI / Cloud Foundry connectivity** — network access from a workload cluster
  to a CF-hosted database is unresolved; a jump host or proxy may be needed.

## Note on "status propagation bug"
Currently, the default timeout for a successful resource creation it 10 minutes.
In a soon after writing (Sept. 10, 2026) to be released v2 version, this will be configurable:
https://github.com/SAP/crossplane-provider-btp/pull/699
Amazon RDS takes about 20 minutes to be created an even more than 10 minutes to just update a firewall rule.
Thus the external ID annotation never gets set and a successful sync doesn't happen.

When revisiting this task, make sure to set a long timeout.

To manually fix a stuck deployment, you have to add the instance uuid manually as an annotation to the ServiceInstance CR.
See https://sap.github.io/crossplane-provider-docs/docs/crossplane-provider-btp/docs/end-user-guides/import-landscape/overview#manual-external-name-annotation

```
metadata:
  annotations:
    crossplane.io/external-name: <resource-id>
```

There is an option in the Service instance to create connection credentials as well, but for me that did not work  - maybe due to the same bug.
Thus I created a ServiceBinding manually. Ideally, this will not be necessary in the future.
