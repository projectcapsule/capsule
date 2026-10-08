---
availability:
  - { place: kubernetes-api::tenant-workspace }
domain: resource-management
references:
  - { kind: code, role: implementation, target: "internal/controllers/resourcepools/utils.go" }
---

# Release ResourcePoolClaim

A tenant owner gives a claim in use back to its pool.
