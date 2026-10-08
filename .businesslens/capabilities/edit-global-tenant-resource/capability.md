---
availability:
  - { place: kubernetes-api::cluster-administration }
domain: replications
references:
  - { kind: code, role: implementation, target: "api/v1beta2/tenantresource_global.go" }
  - { kind: code, role: implementation, target: "internal/controllers/resources/global.go" }
---

# Edit GlobalTenantResource

An administrator changes what a GlobalTenantResource replicates and into which tenants.
