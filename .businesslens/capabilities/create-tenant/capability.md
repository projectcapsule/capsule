---
availability:
  - { place: kubernetes-api::cluster-administration }
domain: tenants
references:
  - { kind: code, role: implementation, target: "internal/webhook/tenant/validation/handler.go" }
  - { kind: code, role: implementation, target: "internal/controllers/tenant/manager.go" }
---

# Create tenant

An administrator creates a Tenant: its owners and the policy every namespace in it will inherit.
