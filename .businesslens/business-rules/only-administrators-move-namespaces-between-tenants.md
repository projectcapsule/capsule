---
appliesTo:
  - { type: entity, id: namespace, effect: changes, facts: [Tenant] }
permits:
  - actors: [administrator]
references:
  - { kind: code, role: implementation, target: "internal/webhook/namespace/validation/handler.go" }
  - { kind: code, role: implementation, target: "internal/webhook/namespace/mutation/assignment.go" }
---

# Only an administrator moves a namespace into, out of or between tenants

Which tenant a namespace belongs to is changed only by an administrator; everyone else is refused whether they add, remove or change it.
