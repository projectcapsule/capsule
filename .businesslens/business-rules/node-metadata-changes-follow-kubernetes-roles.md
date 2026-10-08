---
appliesTo:
  - { type: entity, id: node, effect: changes, facts: [Labels, Annotations] }
permits:
  - configuredBy: kubernetes-role
references:
  - { kind: code, role: implementation, target: "internal/webhook/node/user_metadata.go" }
---

# Only identities a Kubernetes role allows change node labels and annotations

Capsule leaves ordinary node labels and annotations to the cluster's Kubernetes roles; it protects only the keys the CapsuleConfiguration forbids, and only where the node webhook is enabled.
