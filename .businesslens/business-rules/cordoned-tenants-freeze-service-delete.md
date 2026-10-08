---
appliesTo:
  - { type: entity, id: service, effect: removes }
permits:
  - actors: [tenant-owner]
    when:
      - { entity: tenant, fact: Cordoned, is: false }
  - actors: [administrator]
  - unattended: true
references:
  - { kind: code, role: implementation, target: "internal/webhook/generic/cordoning.go" }
---

# Tenant owners delete Services only while their tenant is not cordoned

While a tenant is cordoned, its owners cannot delete Services in its namespaces; administrators and Capsule itself still can.
