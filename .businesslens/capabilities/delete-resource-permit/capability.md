---
availability:
  - { place: kubernetes-api::tenant-workspace }
references:
  - { kind: code, role: implementation, target: "internal/webhook/resourcepermit/resourcepermit_validating.go" }
---

# Delete ResourcePermit

A tenant owner withdraws a ResourcePermit before it is reviewed, or removes an expired one.
