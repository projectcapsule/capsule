---
appliesTo:
  - { type: entity, id: persistent-volume-claim, effect: changes }
permits:
  - actors: [tenant-owner]
    when:
      - { entity: tenant, fact: Cordoned, is: false }
  - actors: [administrator]
  - unattended: true
references:
  - { kind: code, role: implementation, target: "internal/webhook/generic/cordoning.go" }
---

# Tenant owners change PersistentVolumeClaims only while their tenant is not cordoned

While a tenant is cordoned, its owners cannot change PersistentVolumeClaims in its namespaces; administrators and Capsule itself still can.
