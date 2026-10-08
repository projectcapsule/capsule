---
domain: resource-management
references:
  - { kind: code, role: implementation, target: "api/v1beta2/resourcepoolclaim_types.go" }
  - { kind: code, role: implementation, target: "internal/controllers/resourcepools/claim_controller.go" }
---

# ResourcePoolClaim

A namespace's request for an amount of a ResourcePool's resources.

## Information kept

- **Pool** — the pool it claims from
- **Claimed resources** — the amounts it requests
- **Release** — whether its owner asked to release it

## States

### Unassigned

Not yet allocated from a pool.

### Allocated

Its amounts are reserved in the pool and added to the namespace quota, but current usage does not need them.

### In use

Current usage in its namespace needs its amounts.

### Exhausted

The pool cannot cover what it requests, or claims queued before it.
