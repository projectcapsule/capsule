---
availability:
  - { place: kubernetes-api::cluster-administration }
domain: tenants
references:
  - { kind: code, role: implementation, target: "api/v1beta2/tenantowner_types.go" }
  - { kind: code, role: implementation, target: "internal/controllers/tenantowner/manager.go" }
---

# Edit TenantOwner

An administrator changes the cluster roles or labels of a TenantOwner, and with its labels the tenants that select it.
