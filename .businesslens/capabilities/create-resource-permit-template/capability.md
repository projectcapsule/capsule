---
availability:
  - { place: kubernetes-api::cluster-administration }
references:
  - { kind: code, role: implementation, target: "api/v1beta2/resourcepermittemplate_types.go" }
  - { kind: code, role: implementation, target: "internal/webhook/resourcepermit/resourcepermittemplate_validating.go" }
---

# Create ResourcePermitTemplate

An administrator offers, in one namespace, resources that can be requested there for a limited time.
