---
relations:
  - { entity: resource-permit, verb: offers, cardinality: one-to-many }
  - { entity: permit-approver, verb: names, cardinality: many-to-many }
references:
  - { kind: code, role: implementation, target: "api/v1beta2/globalresourcepermittemplate_types.go" }
---

# GlobalResourcePermitTemplate

A cluster-wide offer of resources that can be requested in selected namespaces for a limited time through a ResourcePermit.

## Information kept

- **Namespace selectors** — the namespaces it can be requested in; every namespace when unset
- **Available namespaces** — the namespaces it currently resolves to
- **Impersonation** — the ServiceAccount that renders and applies the resources
- **Resources** — the manifests and templates granted, each with its creation, protection and deletion policy
- **Parameter schema** — the parameters a request may or must supply
- **Context** — other objects loaded for rendering
- **Default duration** — how long a permit lasts when the request gives no duration
- **Maximum duration** — the longest a permit may last
- **Keep for** — how long an expired permit is kept for audit
- **Auto approval** — whether requests matching its conditions are approved without review
- **Approvers** — users and groups that may approve its permits
- **Approval conditions** — expressions over the request, requestor and reviewer that approval needs; any one suffices
