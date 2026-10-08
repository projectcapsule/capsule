---
appliesTo:
  - { type: entity, id: resource-claim, effect: removes }
permits:
  - actors: [tenant-owner]
    when:
      - { entity: tenant, fact: Cordoned, is: false }
  - actors: [administrator]
  - unattended: true
references:
  - { kind: code, role: implementation, target: "internal/webhook/generic/cordoning.go" }
---

# Tenant owners delete ResourceClaims only while their tenant is not cordoned

While a tenant is cordoned, its owners cannot delete ResourceClaims in its namespaces; administrators and Capsule itself still can.
