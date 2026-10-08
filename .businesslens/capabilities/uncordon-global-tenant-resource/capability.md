---
availability:
  - { place: kubernetes-api::cluster-administration }
domain: replications
references:
  - { kind: code, role: implementation, target: "internal/controllers/resources/global.go" }
---

# Uncordon GlobalTenantResource

An administrator resumes a paused cluster-wide replication.
