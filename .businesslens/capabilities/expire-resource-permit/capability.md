---
availability:
  - { place: kubernetes-api::tenant-workspace }
  - { place: capsule-cli::tenant-workspace }
references:
  - { kind: code, role: implementation, target: "api/v1beta2/resourcepermit_func.go" }
  - { kind: code, role: implementation, target: "cmd/cli/cmd/resourcepermit/expire.go" }
---

# Expire ResourcePermit

A permit ends: its granted resources are removed and the permit is kept for audit until its retention ends.
