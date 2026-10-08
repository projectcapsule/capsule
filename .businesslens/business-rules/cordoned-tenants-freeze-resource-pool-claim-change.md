---
appliesTo:
  - { type: entity, id: resource-pool-claim, effect: changes }
permits:
  - actors: [tenant-owner]
    when:
      - { entity: tenant, fact: Cordoned, is: false }
  - actors: [administrator]
  - unattended: true
references:
  - { kind: code, role: implementation, target: "internal/webhook/generic/cordoning.go" }
---

# Tenant owners change ResourcePoolClaims only while their tenant is not cordoned

While a tenant is cordoned, its owners cannot change ResourcePoolClaims in its namespaces; administrators and Capsule itself still can.
