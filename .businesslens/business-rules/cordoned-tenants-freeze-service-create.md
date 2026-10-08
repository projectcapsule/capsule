---
appliesTo:
  - { type: entity, id: service, effect: creates }
permits:
  - actors: [tenant-owner]
    when:
      - { entity: tenant, fact: Cordoned, is: false }
  - actors: [administrator]
  - unattended: true
references:
  - { kind: code, role: implementation, target: "internal/webhook/generic/cordoning.go" }
---

# Tenant owners create Services only while their tenant is not cordoned

While a tenant is cordoned, its owners cannot create Services in its namespaces; administrators and Capsule itself still can.
