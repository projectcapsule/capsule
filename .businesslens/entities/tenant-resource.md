---
domain: replications
references:
  - { kind: code, role: implementation, target: "api/v1beta2/tenantresource_namespaced.go" }
  - { kind: code, role: implementation, target: "api/v1beta2/tenantresource_types.go" }
---

# TenantResource

A tenant owner's instruction, kept in one of the tenant's namespaces, to replicate objects into chosen namespaces of the same tenant and keep them in sync.

## Information kept

- **Resources** — blocks of namespace selector, objects to copy, literal manifests, generator templates, context, extra metadata and lifecycle policy
- **Resync period** — how often it is reconciled again; 60 seconds by default
- **Service account** — the ServiceAccount of its namespace it acts as
- **Cordoned** — whether replication is paused
- **Depends on** — other TenantResources that must be ready first
- **Processed items** — each replicated object with its outcome
- **Ready** — whether the last reconciliation succeeded, and why not
