---
availability:
  - { place: kubernetes-api::tenant-workspace }
  - { place: capsule-cli::tenant-workspace }
references:
  - { kind: code, role: implementation, target: "api/v1beta2/resourcepermit_func.go" }
  - { kind: code, role: implementation, target: "cmd/cli/cmd/resourcepermit/retry.go" }
---

# Retry ResourcePermit

A tenant owner asks Capsule to try a failed permit's failed stage again.
