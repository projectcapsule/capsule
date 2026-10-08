---
appliesTo:
  - { type: capability, id: create-namespace }
  - { type: entity, id: capsule-configuration, facts: [Protected namespace pattern] }
references:
  - { kind: code, role: implementation, target: "internal/webhook/namespace/validation/handler.go" }
---

# No tenant namespace has a name matching the protected namespace pattern

A tenant namespace whose name matches the CapsuleConfiguration's protected namespace pattern is refused for everyone.
