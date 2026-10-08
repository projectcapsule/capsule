---
references:
  - { kind: code, role: implementation, target: "internal/webhook/node/user_metadata.go" }
---

# Node

A cluster node whose labels and annotations Capsule users with node access may change.

## Information kept

- **Labels** — its labels
- **Annotations** — its annotations
- **Protected labels** — its labels whose keys the CapsuleConfiguration forbids
- **Protected annotations** — its annotations whose keys the CapsuleConfiguration forbids
