---
appliesTo:
  - { type: capability, id: create-workload }
  - { type: entity, id: tenant, facts: [Priority classes, Runtime classes, Container registries, Image pull policies] }
references:
  - { kind: code, role: implementation, target: "internal/webhook/pod/priorityclass.go" }
  - { kind: code, role: implementation, target: "internal/webhook/pod/runtimeclass.go" }
  - { kind: code, role: implementation, target: "internal/webhook/pod/containerregistry_legacy.go" }
  - { kind: code, role: implementation, target: "pkg/api/allowed_list.go" }
---

# Workloads use only the classes and registries their tenant allows

A pod may use only the priority and runtime classes, container registries and image pull policies its tenant allows; a class equal to the tenant's default is always allowed.
