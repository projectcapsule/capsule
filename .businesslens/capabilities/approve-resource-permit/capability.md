---
availability:
  - { place: kubernetes-api::permit-review }
  - { place: capsule-cli::permit-review }
references:
  - { kind: code, role: implementation, target: "internal/webhook/resourcepermit/resourcepermit_validating.go" }
  - { kind: code, role: implementation, target: "cmd/cli/cmd/resourcepermit/review.go" }
---

# Approve ResourcePermit

A permit approver approves a requested ResourcePermit, optionally adjusting its duration, retention or start time.
