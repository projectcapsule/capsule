---
availability:
  - { place: kubernetes-api::permit-review }
  - { place: capsule-cli::permit-review }
references:
  - { kind: code, role: implementation, target: "api/v1beta2/resourcepermit_func.go" }
  - { kind: code, role: implementation, target: "cmd/cli/cmd/resourcepermit/review.go" }
---

# Deny ResourcePermit

A permit approver refuses a requested ResourcePermit.
