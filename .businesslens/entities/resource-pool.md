---
domain: resource-management
relations:
  - { entity: resource-pool-claim, verb: serves, cardinality: one-to-many }
references:
  - { kind: code, role: implementation, target: "api/v1beta2/resourcepool_types.go" }
  - { kind: code, role: implementation, target: "internal/controllers/resourcepools/pool_controller.go" }
---

# ResourcePool

A budget of resources an administrator offers to selected namespaces, from which those namespaces claim capacity.

## Information kept

- **Namespace selectors** — the namespaces that may claim from it
- **Quota** — the total resources it offers
- **Defaults** — resources every selected namespace receives without claiming them
- **Defaults zero** — whether every offered resource defaults to zero
- **Ordered queue** — whether claims are served strictly in creation order
- **Delete bound claims** — whether deleting the pool also deletes its claims
- **Allocation** — claimed and still available amounts
- **Exhaustions** — resources requested beyond what is available
