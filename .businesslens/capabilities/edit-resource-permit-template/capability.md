---
availability:
  - { place: kubernetes-api::cluster-administration }
references:
  - { kind: code, role: implementation, target: "internal/webhook/resourcepermit/resourcepermittemplate_validating.go" }
  - { kind: code, role: implementation, target: "api/v1beta2/globalresourcepermittemplate_cel.go#ApprovalPolicy" }
---

# Edit ResourcePermitTemplate

An administrator changes what a ResourcePermitTemplate offers, for how long and who approves it.
