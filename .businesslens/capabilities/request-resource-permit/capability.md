---
availability:
  - { place: kubernetes-api::tenant-workspace }
references:
  - { kind: code, role: implementation, target: "internal/webhook/resourcepermit/resourcepermit_validating.go" }
  - { kind: code, role: implementation, target: "internal/webhook/resourcepermit/resourcepermit_mutating.go" }
  - { kind: code, role: implementation, target: "internal/controllers/resourcepermit/resourcepermit_controller.go" }
---

# Request ResourcePermit

A tenant owner asks for a template's resources in one of their namespaces for a limited time.
