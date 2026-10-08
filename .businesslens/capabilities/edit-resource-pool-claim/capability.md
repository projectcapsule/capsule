---
availability:
  - { place: kubernetes-api::tenant-workspace }
domain: resource-management
references:
  - { kind: code, role: implementation, target: "internal/webhook/resourcepool/claim_validating.go" }
---

# Edit ResourcePoolClaim

A tenant owner changes the amounts or the pool of a claim.
