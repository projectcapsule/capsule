---
availability:
  - { place: kubernetes-api::tenant-workspace }
domain: resource-management
references:
  - { kind: code, role: implementation, target: "internal/webhook/resourcepool/claim_validating.go" }
---

# Delete ResourcePoolClaim

A tenant owner deletes a claim.
