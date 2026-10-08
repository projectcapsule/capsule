---
kind: person
acts: external
references:
  - { kind: code, role: implementation, target: "pkg/runtime/handlers/admission_user.go#ResolveAdmissionUser" }
  - { kind: code, role: implementation, target: "api/v1beta2/capsuleconfiguration_types.go" }
---

# Administrator

A platform operator who runs Capsule for the cluster: one of the users, groups or ServiceAccounts the CapsuleConfiguration names as administrators, or anyone else the cluster's Kubernetes roles let write Capsule's cluster-wide resources. Administrators create tenants and the shared offers tenants draw from, and are effective owners of every tenant.
