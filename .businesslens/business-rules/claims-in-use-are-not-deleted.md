---
appliesTo:
  - { type: entity, id: resource-pool-claim, effect: removes }
permits:
  - actors: [tenant-owner]
    when:
      - { state: Unassigned }
      - { entity: tenant, fact: Cordoned, is: false }
  - actors: [tenant-owner]
    when:
      - { state: Allocated }
      - { entity: tenant, fact: Cordoned, is: false }
  - actors: [tenant-owner]
    when:
      - { state: Exhausted }
      - { entity: tenant, fact: Cordoned, is: false }
references:
  - { kind: code, role: implementation, target: "internal/webhook/resourcepool/claim_validating.go" }
  - { kind: code, role: implementation, target: "internal/webhook/generic/cordoning.go" }
---

# A ResourcePoolClaim in use is not deleted

A tenant owner deletes a claim only while current usage does not need it and the tenant is not cordoned; a claim in use must be released first.
