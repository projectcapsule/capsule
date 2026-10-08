---
availability:
  - { place: kubernetes-api::tenant-workspace }
references:
  - { kind: code, role: implementation, target: "internal/controllers/resourcepermit/resourcepermit_controller.go" }
---

# Activate ResourcePermit

Capsule creates an approved permit's resources at its start time and keeps them until it expires.
