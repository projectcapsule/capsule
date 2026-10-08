---
domain: rules
references:
  - { kind: code, role: implementation, target: "pkg/api/rules/rule_body_types.go" }
  - { kind: code, role: implementation, target: "pkg/api/rules/enforce_types.go" }
---

# Namespace rule

One entry of a tenant's ordered rules: which of the tenant's namespaces it selects, whose requests it applies to, and the enforcement, mutation, permissions and quotas it gives those namespaces. The rules selecting a namespace compose into that namespace's profile.

## Information kept

- **Position** — its place in the tenant's ordered rules; a later rule wins over an earlier one
- **Namespace selector** — the namespaces of the tenant it applies to; all of them when unset
- **Audience** — the users, groups, ServiceAccounts, administrators, tenant owners or Capsule users whose requests it applies to; everyone when unset
- **Action** — allow, deny or audit; deny when unset
- **Conditions** — expressions over the request and object that must all hold for the rule to apply
- **Metadata rules** — labels and annotations required, allowed, denied, defaulted or managed on chosen kinds
- **Network policy rules** — egress address ranges NetworkPolicies may or may not grant
- **Service rules** — Service types, external IPs, load balancer addresses, node ports and external names
- **Ingress rules** — route kinds and the hostnames they may use
- **Workload rules** — workload kinds, image registries and pull policies, QoS classes, resource defaults, placement, security profiles and disruption budgets
- **Mutations** — scheduling, image pull and security settings Capsule writes into new pods, merged with what the pod sets or replacing each property supplied
- **Permissions** — cluster roles bound to subjects and promotions of labelled ServiceAccounts in the selected namespaces
- **Quotas** — named resource quotas shared by all selected namespaces
