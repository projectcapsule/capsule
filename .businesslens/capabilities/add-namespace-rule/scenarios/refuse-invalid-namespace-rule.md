---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator adds a namespace rule that manages metadata without naming concrete kinds"
    kind: actor
    actor: administrator
    entities:
      - { entity: namespace-rule, effect: reads, facts: [Metadata rules] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product refuses the Tenant change and names the invalid field"
    kind: condition
    entities:
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Refuse an invalid rule

## Trigger

An administrator adds a rule that cannot be applied.

## Outcome

The Tenant keeps its previous rules; the refusal names the invalid rule field.

## Edge cases

- Hostname rules without route kinds are refused.
- A quota name used twice in one Tenant, or one that cannot name a quota, is refused.
- An unknown custom audience is refused.
