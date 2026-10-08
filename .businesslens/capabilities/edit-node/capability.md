---
availability:
  - { place: kubernetes-api::tenant-workspace }
references:
  - { kind: code, role: implementation, target: "internal/webhook/node/user_metadata.go" }
---

# Edit node

A Capsule user with access to nodes changes node labels and annotations, except the keys the CapsuleConfiguration forbids. This protection exists only where the node webhook is enabled.
