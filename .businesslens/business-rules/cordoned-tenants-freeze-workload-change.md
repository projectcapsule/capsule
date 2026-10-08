---
appliesTo:
  - { type: entity, id: workload, effect: changes }
permits:
  - actors: [tenant-owner]
    when:
      - { entity: tenant, fact: Cordoned, is: false }
  - actors: [administrator]
  - unattended: true
references:
  - { kind: code, role: implementation, target: "internal/webhook/generic/cordoning.go" }
---

# Tenant owners change workloads only while their tenant is not cordoned

While a tenant is cordoned, its owners cannot change workloads in its namespaces; administrators and Capsule itself still can.
