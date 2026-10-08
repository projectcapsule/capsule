---
availability:
  - { place: kubernetes-api::cluster-administration }
domain: replications
references:
  - { kind: code, role: implementation, target: "internal/controllers/resources/global.go" }
---

# Cordon GlobalTenantResource

An administrator pauses a cluster-wide replication: Capsule stops applying and removing its objects until it is uncordoned.
