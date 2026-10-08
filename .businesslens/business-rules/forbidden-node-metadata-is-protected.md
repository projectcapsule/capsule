---
appliesTo:
  - { type: entity, id: node, effect: changes, facts: [Protected labels, Protected annotations] }
permits:
  - actors: [administrator]
references:
  - { kind: code, role: implementation, target: "internal/webhook/node/user_metadata.go" }
---

# Only administrators change node labels and annotations the configuration forbids

Capsule users cannot add, change or remove node labels and annotations whose keys the CapsuleConfiguration forbids; other keys stay theirs to change.
