---
appliesTo:
  - { type: capability, id: create-workload }
  - { type: entity, id: namespace-rule, facts: [Audience] }
references:
  - { kind: code, role: implementation, target: "pkg/ruleengine/audience.go#FilterNamespaceRulesByAudience" }
---

# A rule applies only to requests from its audience

Enforcement and mutation in a rule apply to every requester, including administrators and cluster controllers, unless its audience narrows them. A rule's quotas apply whatever the audience.
