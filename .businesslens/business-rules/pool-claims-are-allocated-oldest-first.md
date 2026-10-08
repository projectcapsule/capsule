---
appliesTo:
  - { type: capability, id: allocate-resource-pool-claims }
  - { type: entity, id: resource-pool, facts: [Ordered queue] }
references:
  - { kind: code, role: implementation, target: "internal/controllers/resourcepools/utils.go" }
---

# Pool claims are allocated and marked in use oldest first

Claims are considered in creation order; with an ordered queue, no later claim is allocated a resource an earlier claim is still waiting for.
