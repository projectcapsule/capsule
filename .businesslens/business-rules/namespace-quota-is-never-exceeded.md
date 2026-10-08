---
appliesTo:
  - { type: capability, id: create-namespace }
  - { type: entity, id: tenant, facts: [Namespace quota] }
references:
  - { kind: code, role: implementation, target: "internal/webhook/namespace/validation/quota.go" }
  - { kind: code, role: implementation, target: "api/v1beta2/tenant_func.go" }
---

# A tenant never holds more namespaces than its namespace quota

A new namespace is refused once the tenant holds as many namespaces as its namespace quota allows.
