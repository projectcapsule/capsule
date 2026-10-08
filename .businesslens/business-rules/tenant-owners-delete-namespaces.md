---
appliesTo:
  - { type: entity, id: namespace, effect: changes, to: Terminating }
permits:
  - related: [{ verb: groups, entity: tenant }, { verb: owns, entity: tenant-owner }]
    when:
      - { entity: tenant, fact: Cordoned, is: false }
  - actors: [administrator]
  - unattended: true
references:
  - { kind: code, role: implementation, target: "internal/webhook/namespace/validation/handler.go" }
---

# Only an owner of its tenant deletes a tenant namespace, and not while the tenant is cordoned

A tenant namespace is deleted only by an effective owner of its tenant while the tenant is not cordoned, by an administrator, or by Capsule when the tenant is deleted.
