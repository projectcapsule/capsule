---
availability:
  - { place: kubernetes-api::cluster-administration }
domain: tenants
references:
  - { kind: code, role: implementation, target: "api/v1beta2/tenant_types.go#Permissions" }
  - { kind: code, role: implementation, target: "internal/controllers/tenantowner/manager.go" }
---

# Create TenantOwner

An administrator defines an owner identity once and lets tenants select it by label.
