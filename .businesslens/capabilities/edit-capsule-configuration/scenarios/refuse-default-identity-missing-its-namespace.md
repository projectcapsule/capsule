---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator names a global default identity without saying where it lives"
    kind: actor
    actor: administrator
    entities:
      - { entity: capsule-configuration, effect: reads, facts: [Default service accounts] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product refuses the CapsuleConfiguration change"
    kind: condition
    entities:
      - { entity: capsule-configuration, effect: reads, facts: [Default service accounts] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Refuse an incomplete configuration

## Trigger

An administrator sets a default identity only partly.

## Outcome

The CapsuleConfiguration keeps its previous settings.

## Edge cases

- A protected namespace pattern or forbidden node metadata pattern that does not compile is refused.
