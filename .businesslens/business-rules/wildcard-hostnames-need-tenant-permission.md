---
appliesTo:
  - { type: capability, id: create-ingress }
  - { type: entity, id: tenant, facts: [Ingress options] }
references:
  - { kind: code, role: implementation, target: "internal/webhook/ingress/validate_wildcard.go" }
---

# Wildcard ingress hostnames are refused unless the tenant allows them

Tenants do not allow wildcard hostnames unless their ingress options say so.
