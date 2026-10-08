---
availability:
  - { place: kubernetes-api::tenant-workspace }
domain: replications
references:
  - { kind: code, role: implementation, target: "api/v1beta2/tenantresource_namespaced.go" }
  - { kind: code, role: implementation, target: "internal/controllers/resources/namespaced.go" }
---

# Edit TenantResource

A tenant owner changes what a TenantResource replicates and where.
