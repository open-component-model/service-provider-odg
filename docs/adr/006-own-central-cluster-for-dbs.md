# ADR-006: Provision Databases via a Central ODG Control Plane

| Status   | Proposed                                      |
|----------|-----------------------------------------------|
| Date     | 2026-09-09                                    |

## Context and Problem Statement

From zero to database provisioning via Crossplane Provider BTP (ADR-005) requires roughly
ten custom resources. We need to decide who bears that complexity: the customer's ControlPlane
a central ODG-owned ControlPlane or the workload cluster.

A second angle is multi-tenancy: BTP supports provisioning multiple logical
databases (schemas/users) within a single managed DB instance, which could
allow multiple ODG tenants to share one instance rather than each getting their
own.

## Decision Drivers

* **Operational complexity**: the BTP provisioning chain is too involved to
  expose to customers.
* **Migrations and maintenance**: we need to be able to migrate or reconfigure
  databases without customer involvement.
* **Credential management**: DB credentials must flow from the provisioning
  cluster to the ODG workload cluster regardless of where provisioning lives.
* **Multi-tenancy**: a central provisioning point makes it possible to
  co-locate multiple tenants on one DB instance, reducing cost and resource
  overhead at scale.

## Considered Options

1. **Customer ControlPlane** — customer declares the database in their own
   ControlPlane; they own the credentials and data directly.
2. **Central ODG ControlPlane** — ODG operates one (or one per environment)
   ControlPlane dedicated to database provisioning; customers see none of the
   BTP resources.
3. **Workload-ODG Cluster** - Instead provisioning of on the customer control plane,
   we install the Crossplane Operator on the workload-odg cluster and create the database there.

## Decision Outcome (tbd)

Proposed option: **"Central ODG ControlPlane"**, because exposing ~7
custom resources accross two clusters to customers adds unnecessary complexity and prevents us from
doing maintenance or migrations transparently. Centralising provisioning also
unlocks multi-tenancy: the central plane can provision isolated
schemas/users within a shared DB instance and distribute per-tenant credentials
to each workload cluster, avoiding one managed instance per tenant.

Option "Workload-ODG Cluster" would require that we have Crossplane installed 
and configured on the workload or platform cluster with our central BTP accont.
At the moment, Crossplane + BTP operators run (at least partially) on a ControlPlane. 
Thus it would require a lot of effort to re-architect them to be available to not
be bound to a ControlPlane.


## Consequences

Positive (central cluster):

- Customers have no BTP setup burden.
- We can migrate, reconfigure, or patch databases without customer action.
- Environment-specific BTP configuration differences are handled centrally.
- Multi-tenancy possible: multiple ODG tenants can share one BTP-managed DB
  instance with isolated credentials, reducing cost at scale.

Negative / follow-up:

- Database credentials must be propagated from the central ControlPlane to each ODG
  workload cluster — a cross-cluster secret distribution mechanism is needed 
  (but I think available and needed with any option)
- Customer has no access to the database.
- One central ControlPlane is a shared failure domain; a per-environment
  split may be needed for isolation.
- Multi-tenancy would introduce a shared blast radius at the DB level,
  so need to decide if we want to introduce that at some point later on.
