---
availability:
  - { place: kubernetes-api::tenant-workspace }
domain: resource-management
references:
  - { kind: code, role: implementation, target: "internal/controllers/resourcepools/pool_controller.go" }
  - { kind: code, role: implementation, target: "internal/controllers/resourcepools/utils.go" }
---

# Allocate ResourcePoolClaims

Capsule allocates claims from their pools, keeps each namespace's pool quota equal to its allocated claims plus the pool defaults, and marks which claims current usage needs.
