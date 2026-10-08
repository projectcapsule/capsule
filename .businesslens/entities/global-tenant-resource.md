---
domain: replications
references:
  - { kind: code, role: implementation, target: "api/v1beta2/tenantresource_global.go" }
---

# GlobalTenantResource

An administrator's instruction to replicate objects into the namespaces of selected tenants, once per tenant or once for the cluster, and keep them in sync.

## Information kept

- **Tenant selector** — the tenants it replicates into
- **Scope** — once per selected namespace, once per tenant, or once for the cluster
- **Resources** — blocks of namespace selector, objects to copy, literal manifests, generator templates, context, extra metadata and lifecycle policy
- **Resync period** — how often it is reconciled again; 60 seconds by default
- **Service account** — the ServiceAccount it acts as
- **Cordoned** — whether replication is paused
- **Depends on** — other GlobalTenantResources that must be ready first
- **Selected tenants** — the tenants it currently replicates into
- **Processed items** — each replicated object with its outcome
- **Ready** — whether the last reconciliation succeeded, and why not
