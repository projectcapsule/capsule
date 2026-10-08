---
availability:
  - { place: kubernetes-api::tenant-workspace }
domain: replications
references:
  - { kind: code, role: implementation, target: "internal/controllers/resources/namespaced.go" }
---

# Uncordon TenantResource

A tenant owner resumes a paused replication.
