---
availability:
  - { place: kubernetes-api::cluster-administration }
references:
  - { kind: code, role: implementation, target: "api/v1beta2/globalresourcepermittemplate_types.go" }
  - { kind: code, role: implementation, target: "internal/webhook/resourcepermit/globalresourcepermittemplate_validating.go" }
---

# Create GlobalResourcePermitTemplate

An administrator offers, cluster-wide, resources that can be requested in selected namespaces for a limited time.
