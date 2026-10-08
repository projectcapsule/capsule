---
availability:
  - { place: kubernetes-api::tenant-workspace }
domain: resource-management
references:
  - { kind: code, role: implementation, target: "internal/webhook/resourcepool/claim_mutating.go" }
  - { kind: code, role: implementation, target: "internal/controllers/resourcepools/claim_controller.go" }
---

# Create ResourcePoolClaim

A tenant owner claims an amount of a pool's resources for one of their namespaces.
