# ADR-005: Use Crossplane Provider BTP for Database Provisioning

| Status   | Proposed                                      |
|----------|-----------------------------------------------|
| Date     | 2026-09-09                                    |

## Context and Problem Statement

Given the decision to use SAP BTP for managed databases (ADR-004), we need a
mechanism to provision BTP resources from Kubernetes without writing
controllers or managing Terraform state.

## Decision Drivers

* **Kubernetes-native**: provisioning should be declarative and reconciled,
  consistent with how everything else in OCP is managed.
* **Avoid custom controllers**: writing and maintaining our own BTP controller
  is expensive.
* **OCP integration**: the provider must work within the OCP ecosystem.

## Considered Options

1. **Crossplane Provider BTP** — uses existing `provider-btp` managed
   resources; integrates with OCP's Crossplane installation.
2. **Custom Kubernetes controller** — write our own controller against the BTP
   API.
3. **Terraform** — provision via Terraform pipelines outside the cluster.

## Decision Outcome

Chosen option: **"Crossplane Provider BTP"**, because it is Kubernetes-native,
reconciliation is handled automatically, and a BTP provider already exists with
OCP integration — no custom controller needed.

## Consequences

Positive:

- Declarative, reconciled provisioning — no external pipeline state.
- Reuses OCP's existing Crossplane service provider setup.
- Community-maintained provider; no long-term ownership of provisioning logic.

Negative / follow-up:

- **Provider BTP v2 is upcoming** — the API may change; the current
  end-to-end test (see `docs/db-provisioning-example/`) used v1.
- **Status propagation bug observed** — in testing, the BTP instance reached
  `Ready` in BTP but never transitioned to `Ready` in Kubernetes; credential
  binding (the final step) did not complete. Needs a bug report / version
  upgrade before Beta. So might also not be a completely smooth ride.
