---
appliesTo:
  - { type: entity, id: gateway, effect: changes }
permits:
  - actors: [tenant-owner]
    when:
      - { entity: tenant, fact: Cordoned, is: false }
  - actors: [administrator]
  - unattended: true
references:
  - { kind: code, role: implementation, target: "internal/webhook/generic/cordoning.go" }
---

# Tenant owners change Gateways only while their tenant is not cordoned

While a tenant is cordoned, its owners cannot change Gateways in its namespaces; administrators and Capsule itself still can.
