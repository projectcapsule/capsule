---
appliesTo:
  - { type: capability, id: create-namespace }
  - { type: entity, id: tenant, facts: [Force tenant prefix] }
references:
  - { kind: code, role: implementation, target: "internal/webhook/namespace/validation/prefix.go" }
---

# A namespace name starts with its tenant name and a dash when the prefix is forced

The tenant's own setting decides whether the prefix is forced; without one, the CapsuleConfiguration decides.
