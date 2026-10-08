---
appliesTo:
  - { type: entity, id: namespace, effect: creates }
permits:
  - related: [{ verb: groups, entity: tenant }, { verb: owns, entity: tenant-owner }]
    when:
      - { entity: tenant, fact: Cordoned, is: false }
  - actors: [administrator]
references:
  - { kind: code, role: implementation, target: "internal/webhook/namespace/validation/handler.go" }
  - { kind: code, role: implementation, target: "pkg/tenant/get_by.go#GetTenantByLabelsAndUser" }
---

# Only an owner of a tenant creates namespaces in it, and not while it is cordoned

A namespace is created in a tenant only by one of the tenant's effective owners while the tenant is not cordoned, or by an administrator. Administrators are effective owners of every tenant.
