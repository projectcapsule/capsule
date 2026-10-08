---
relations:
  - { entity: granted-resource, verb: grants, cardinality: one-to-many }
references:
  - { kind: code, role: implementation, target: "api/v1beta2/resourcepermit_types.go" }
  - { kind: code, role: implementation, target: "api/v1beta2/resourcepermit_func.go" }
---

# ResourcePermit

A time-boxed request, made in a namespace, to have a template's resources created there, reviewed before anything is created and removed again when it expires.

## Information kept

- **Template** — the template requested; fixed once created
- **Parameters** — the values supplied for the template
- **Requestor** — the identity that made the request
- **Reason** — why it is requested
- **Duration** — how long it lasts once active; unlimited when zero
- **Start time** — when it may become active
- **Keep for** — how long it is kept for audit once it has expired; the template's value unless a reviewer changes it
- **Rendered resources** — the resources the template produced for this request
- **Resolved identity** — the ServiceAccount that applies the resources
- **Review** — the reviewer, the verdict and the reviewer's message
- **Failure** — the stage that failed and why
- **Active until** — when an active permit expires
- **Keep until** — when an expired permit is deleted
- **Transitions** — every phase change with who made it and why

## States

### Created

Submitted; Capsule is rendering the template.

### Requested

Rendered and checked; waiting for review.

### Denied

A reviewer refused it.

### Approved

Approved; waiting for its start time.

### Active

Its resources exist until it expires.

### Failed

Checking or applying its resources failed.

### Retrying

Its requester asked Capsule to try the failed stage again.

### Expired

Its resources are removed; it is kept until its retention ends.
