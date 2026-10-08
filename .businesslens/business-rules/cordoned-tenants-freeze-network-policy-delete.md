---
appliesTo:
  - { type: entity, id: network-policy, effect: removes }
permits:
  - actors: [tenant-owner]
    when:
      - { entity: tenant, fact: Cordoned, is: false }
  - actors: [administrator]
  - unattended: true
references:
  - { kind: code, role: implementation, target: "internal/webhook/generic/cordoning.go" }
---

# Tenant owners delete NetworkPolicies only while their tenant is not cordoned

While a tenant is cordoned, its owners cannot delete NetworkPolicies in its namespaces; administrators and Capsule itself still can.
