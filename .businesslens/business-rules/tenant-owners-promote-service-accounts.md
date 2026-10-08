---
appliesTo:
  - { type: entity, id: service-account, effect: changes, facts: [Promotion] }
permits:
  - related: [{ verb: contains, entity: namespace }, { verb: groups, entity: tenant }, { verb: owns, entity: tenant-owner }]
    when:
      - { entity: capsule-configuration, fact: Service account promotion, is: true }
  - actors: [administrator]
    when:
      - { entity: capsule-configuration, fact: Service account promotion, is: true }
references:
  - { kind: code, role: implementation, target: "internal/webhook/serviceaccounts/promotion.go" }
---

# Only a tenant owner promotes a ServiceAccount, and only while promotion is enabled

A ServiceAccount is marked for promotion only by an effective owner of its tenant or an administrator, while the CapsuleConfiguration allows promotion.
