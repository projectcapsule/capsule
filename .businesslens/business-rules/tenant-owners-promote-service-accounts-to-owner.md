---
appliesTo:
  - { type: entity, id: service-account, effect: changes, facts: [Owner promotion] }
permits:
  - related: [{ verb: contains, entity: namespace }, { verb: groups, entity: tenant }, { verb: owns, entity: tenant-owner }]
    when:
      - { entity: capsule-configuration, fact: Service account promotion, is: true }
      - { entity: tenant, fact: Owner promotion, is: true }
  - actors: [administrator]
    when:
      - { entity: capsule-configuration, fact: Service account promotion, is: true }
      - { entity: tenant, fact: Owner promotion, is: true }
references:
  - { kind: code, role: implementation, target: "internal/webhook/serviceaccounts/owner_promotion.go" }
---

# Only a tenant owner promotes a ServiceAccount to owner, and only where promotion is allowed

A ServiceAccount is promoted to owner of its tenant only by an effective owner of that tenant or an administrator, while the CapsuleConfiguration allows promotion and the tenant allows owner promotion.
