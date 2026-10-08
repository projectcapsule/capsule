---
appliesTo:
  - { type: capability, id: replicate-tenant-resources }
  - { type: entity, id: tenant-resource, facts: [Resources] }
references:
  - { kind: code, role: implementation, target: "internal/controllers/resources/collect.go" }
  - { kind: code, role: implementation, target: "pkg/template/validator_namespaces.go" }
---

# A TenantResource reads from and writes to its own tenant's namespaces only

A TenantResource copies objects only from its own namespace, places them only in namespaces of its tenant, and never creates cluster-scoped objects.
