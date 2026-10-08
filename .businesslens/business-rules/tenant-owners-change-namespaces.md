---
appliesTo:
  - { type: entity, id: namespace, effect: changes }
permits:
  - related: [{ verb: groups, entity: tenant }, { verb: owns, entity: tenant-owner }]
    when:
      - { entity: tenant, fact: Cordoned, is: false }
  - actors: [administrator]
  - unattended: true
references:
  - { kind: code, role: implementation, target: "internal/webhook/namespace/validation/handler.go" }
  - { kind: code, role: implementation, target: "pkg/tenant/owned.go#NamespaceIsOwned" }
---

# Only an owner of its tenant changes a tenant namespace, and not while the tenant is cordoned

A tenant namespace is changed only by an effective owner of its tenant while the tenant is not cordoned, by an administrator, or by Capsule keeping the tenant's policy applied.
