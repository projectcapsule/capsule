---
scope: The Capsule controller, its admission webhooks, its API types and the Capsule CLI.
method: Static reading of source, tests and chart configuration; nothing was built or run.
covered:
  - description: Current API types and shared API structures.
    paths: [api/v1beta2/, pkg/api/]
  - description: Admission webhooks, routes and request handlers.
    paths: [internal/webhook/, pkg/runtime/]
  - description: Controllers reconciling tenants, rules, replications, quotas, pools and permits.
    paths: [internal/controllers/, internal/quota/]
  - description: Tenant, user, rule engine and template packages.
    paths: [pkg/tenant/, pkg/users/, pkg/ruleengine/, pkg/template/, pkg/utils/]
  - description: Controller entry point and webhook registration.
    paths: [cmd/controller/]
  - description: Capsule CLI.
    paths: [cmd/cli/]
exclusions:
  - description: Process-local caches, metrics and test doubles.
    paths: [internal/cache/, internal/metrics/, internal/mocks/, internal/version/]
  - description: Helm chart, installation hooks and generated CRDs.
    paths: [charts/]
  - description: End-to-end and stress test suites.
    paths: [e2e/]
  - description: Playground examples and demo environments.
    paths: [playground/]
  - description: Development tooling, CI and release configuration.
    paths: [hack/, .github/, Makefile, .goreleaser.yml, .ko.yaml, Dockerfile.tracing]
  - description: Logos and documentation images.
    paths: [assets/]
  - description: Webhook serving certificate management.
    paths: [internal/controllers/tls/]
  - description: Quota reservation ledgers kept for admission bookkeeping.
    paths: [api/v1beta2/quantityledgers_types.go, api/v1beta2/quantityledgers_status.go]
  - description: Field indexes for cached lookups.
    paths: [pkg/runtime/indexers/]
unmapped:
  - description: Legacy v1beta1 Tenant API and its conversion.
    paths: [api/v1beta1/, api/v1beta2/tenant_conversion_hub.go]
  - description: Legacy annotation-based custom resource counter.
    paths: [internal/webhook/generic/custom_resource_quota.go]
  - description: Disruption budget rule enforcement.
    paths: [internal/webhook/rules/generic/validation/disruption_budgets.go, internal/webhook/rules/generic/validation/disruption_budget_properties.go, internal/webhook/rules/generic/validation/disruption_budget_workloads.go]
  - description: Tenant labelling of every namespaced object.
    paths: [internal/webhook/generic/metadata.go]
  - description: Deprecated pod and Service metadata controllers.
    paths: [internal/controllers/pod/, internal/controllers/servicelabels/]
  - description: ServiceAccount deletion guard for identities in use.
    paths: [internal/webhook/serviceaccounts/references.go]
  - description: Manual activation of an approved permit through the CLI or a status update.
    paths: [cmd/cli/cmd/resourcepermit/activate.go]
limitations: []
---

# Coverage
