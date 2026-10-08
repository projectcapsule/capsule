---
availability:
  - { place: kubernetes-api::tenant-workspace }
domain: tenants
references:
  - { kind: code, role: implementation, target: "internal/webhook/namespace/validation/handler.go" }
---

# Delete namespace

A tenant owner deletes a namespace of their tenant.
