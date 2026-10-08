---
availability:
  - { place: kubernetes-api::cluster-administration }
domain: tenants
references:
  - { kind: code, role: implementation, target: "internal/controllers/tenant/manager.go" }
  - { kind: code, role: implementation, target: "internal/webhook/tenant/validation/protected.go" }
  - { kind: code, role: implementation, target: "internal/controllers/tenant/namespace_cleanup.go" }
---

# Delete tenant

An administrator deletes a tenant together with all of its namespaces.
