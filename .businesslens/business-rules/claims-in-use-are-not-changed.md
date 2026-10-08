---
appliesTo:
  - { type: entity, id: resource-pool-claim, effect: changes, facts: [Pool, Claimed resources] }
permits:
  - actors: [tenant-owner]
    when:
      - { state: Unassigned }
  - actors: [tenant-owner]
    when:
      - { state: Allocated }
  - actors: [tenant-owner]
    when:
      - { state: Exhausted }
references:
  - { kind: code, role: implementation, target: "internal/webhook/resourcepool/claim_validating.go" }
---

# A ResourcePoolClaim in use keeps its pool and amounts

A tenant owner changes a claim's pool or amounts only while current usage does not need it.
