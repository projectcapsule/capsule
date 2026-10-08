---
appliesTo:
  - { type: capability, id: create-ingress }
  - { type: entity, id: tenant, facts: [Ingress options] }
references:
  - { kind: code, role: implementation, target: "internal/webhook/ingress/validate_collision.go" }
---

# A hostname and path are used by one Ingress within the tenant's collision scope

Within the tenant's hostname collision scope, an Ingress cannot use a hostname and path another Ingress already uses.
