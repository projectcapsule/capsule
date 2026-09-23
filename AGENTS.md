# Agent instructions for Capsule

These instructions apply throughout this repository. Capsule's central design goal
is **namespace profiling through the rules API**: declaratively defining the
policies, permissions, metadata, and resource behavior that apply to each namespace.
Tenants provide ownership, isolation, and a scope for distributing those rules.
Every feature must serve this namespace-profiling goal. Tenant isolation, API
compatibility, and admission latency remain core requirements. Extend the existing
design and follow the conventions of the package you are changing.

## Required outcomes

- **Implement new features primarily through the rules API.** Extend the existing
  rule model and its consumers as the default approach to namespace behavior.
- **Design around the effective profile of each namespace.** Tenant membership
  establishes scope; namespaces within one tenant can require different profiles.
- Align every feature with the current repository structure and architecture.
- Search for existing implementations before adding code. Reuse or extend existing
  types, helpers, handlers, controllers, caches, and test fixtures wherever possible.
- Every change requires unit tests and end-to-end (e2e) tests. Add or extend coverage
  for the changed behavior; an unrelated passing suite is insufficient.
- Every e2e change must cover positive and negative cases with one or more real
  Tenant objects present. Add multiple tenants whenever isolation or shared state
  is involved.
- **Run e2e locally only for new feature tests and subsystems/components impacted by
  the change. The full e2e suite runs in GitHub Actions.** Collect observations with
  minimal reasoning during execution; perform forensics after the relevant suites
  have finished.
- New or materially changed performance-sensitive execution paths require benchmark
  coverage; extend existing benchmarks where appropriate.
- **All admission changes are performance critical**, including changes to shared
  helpers, configuration, rules, lookups, caches, or registration used by admission.
- **Reuse existing local mutex-protected caches and indexed lookups wherever their
  consistency guarantees fit the operation.** Admission must avoid repeated
  compilation, redundant reads, and full-list filtering when these facilities apply.
- A change is not fully validated until its required checks pass. Report missing
  coverage, unavailable environments, and unrun checks explicitly.
- **Every change requires a self-review of scalability, performance, and security.**
  Explain the impact in each area, address findings and feedback, and repeat the
  review and relevant validation until the completion criteria below are met.

## Primary design direction: namespace profiling through rules

Namespace profiling means composing the applicable rules into the desired behavior
of a namespace and the resources within it. Start feature design by identifying the
namespace behavior to express, how namespaces are selected, how rules compose, and
how that behavior is reconciled or enforced. Ownership and tenant lifecycle support
this goal; operating on a Tenant object alone does not establish that a feature is
designed at the right level.

- Start with `pkg/api/rules/`, especially `NamespaceRuleBodyNamespace` and
  `NamespaceRuleBodyTenant`. Keep reusable namespace behavior in the namespace rule
  body, with tenant distribution and namespace selection in the existing wrapper.
- Prefer extending the rules exposed through `Tenant.spec.rules` over adding
  standalone Tenant policy fields, tenant-wide switches, or special-case handlers.
  Existing legacy Tenant fields are compatibility surfaces, not the default model
  for new features. Preserve their behavior when maintaining them.
- Reuse namespace selection, rule ordering, audience filtering, templating, and
  effective-rule evaluation. Do not assume every namespace in a tenant has the
  same profile or reduce rule composition to a tenant-wide boolean.
- Follow the relevant existing path through `pkg/tenant/rules.go`, tenant and
  `rulestatus` controllers, `RuleStatus`, `pkg/ruleengine/`, and
  `internal/webhook/rules/`. `RuleStatus.status.rules` holds effective namespace
  enforcement rules; preserve its ordering and the established fallback behavior.
  Other rule effects, such as quota generation and permissions, must extend their
  existing reconciliation paths rather than being forced into enforcement status.
- A feature implemented outside the rules API must explain why the rule model is
  unsuitable and how the feature still supports namespace profiling. Infrastructure
  and tenant lifecycle changes should enable that model without adding a parallel
  policy mechanism. This direction does not authorize unrelated API migrations or
  a new NamespaceProfile resource.
- Demonstrate the resulting namespace behavior in tests. For rules features, cover
  selected and non-selected namespaces, different profiles within one tenant,
  composition with existing rules, and changes to rules or namespace labels. Keep
  the required positive/negative cases and cross-tenant isolation coverage.

## Start with the repository

Read [CONTRIBUTING.md](CONTRIBUTING.md), [DEVELOPMENT.md](DEVELOPMENT.md), and the
relevant [Makefile](Makefile) targets. Check [go.mod](go.mod),
[.golangci.yaml](.golangci.yaml), and [.github/workflows](.github/workflows) for the
actual toolchain, formatting, and CI requirements. Prefer executable targets and
current code when older development examples disagree with them; do not copy stale
version numbers or nonexistent targets.

Before implementing a change:

1. Inspect the working tree and preserve unrelated work.
2. Identify the namespace profile behavior and its rules API extension point, then
   trace the closest existing feature through rule selection, reconciliation or
   admission, registration, and its unit/e2e tests.
3. Search the shared packages for reusable behavior, local caches, and field
   indexers; inspect their callers, lifetime, and consistency requirements.
4. Identify tenant boundaries, compatibility requirements, and admission impact.
5. Define the unit tests and positive/negative tenant e2e scenarios needed to prove
   the change. Select local e2e tests for the new feature and impacted components;
   leave full-suite execution to GitHub Actions. Identify new or materially changed
   performance-sensitive execution paths and plan benchmark coverage for them.

## Repository map and extension points

| Location | Responsibility and placement guidance |
| --- | --- |
| `cmd/controller/` | Controller entry point, dependency construction, schemes, indexes, controller and webhook registration. Keep domain logic in the existing packages. |
| `api/v1beta2/` | Current Capsule API types, status, helpers, and conversion hub. Extend the relevant resource instead of introducing a parallel API. |
| `api/v1beta1/` | Older Tenant API and conversion support. Preserve supported conversion behavior when changing shared fields. |
| `pkg/api/rules/` | Primary extension point for new namespace-profiling features: shared namespace rule bodies, tenant distribution/selection, enforcement, permissions, and quotas. |
| `pkg/api/` | Shared API structures and domain types, including `meta`, `rbac`, `rules`, `runtime`, `errors`, and `processor`. Reuse these across resources. |
| `internal/controllers/` | Reconciliation grouped by domain, such as `tenant`, `rulestatus`, `resources`, `resourcepools`, quotas, and `admission`. Extend the owning controller. |
| `internal/webhook/` | Admission router, resource handlers, rule handlers, and route registration. Add behavior to the relevant existing chain. |
| `pkg/runtime/handlers/` | Shared typed admission wrappers, tenant/user/ruleset resolution, and audience/privilege gates. |
| `pkg/runtime/admission/` | Shared admission responses, dynamic webhook configuration, and match-condition helpers. |
| `pkg/tenant/`, `pkg/users/` | Tenant lookup, ownership, namespace rules, user identities, groups, and service accounts. |
| `pkg/ruleengine/`, `pkg/template/` | Rule evaluation, audience filtering, template preparation/rendering, and resource references. |
| `internal/cache/` | Shared compiled-expression, selector/target, registry, discovery, and impersonation caches. Check invalidation in `internal/controllers/cfg/invalidator/`. |
| `pkg/runtime/indexers/`, `pkg/runtime/predicates/` | Indexed lookups and event filtering. Register needed indexes through the existing manager setup. |
| `pkg/runtime/`, `pkg/utils/` | Existing runtime and utility helpers. Prefer the most specific existing package over adding miscellaneous helpers. |
| `internal/metrics/` | Existing metric recorders and lifecycle cleanup. |
| `e2e/` | Ginkgo/Gomega integration tests against an existing Kubernetes cluster, with shared clients and lifecycle helpers. |
| `e2e/stress/` | KWOK/KinD setup and tenant workload seeding for scalability investigations. |
| `charts/capsule/` | Helm templates, values, schema, generated CRDs, and chart documentation. |
| `hack/`, `.github/` | Development tooling, generation, lint configuration, and CI. Extend the existing workflows. |

Do not create a second controller framework, webhook dispatcher, rule engine,
tenant lookup mechanism, or testing framework for an individual feature. Introduce
a package or abstraction only when the existing structure cannot reasonably own
the behavior, and explain that decision in the change description. Keep dependencies
consistent with the existing layering and avoid circular imports.

## Coding and API conventions

- Match neighboring naming, file organization, constructor patterns, and interfaces.
  Keep changes focused; avoid unrelated renames, formatting churn, and refactors.
- Use the Go version and dependencies declared in `go.mod`. Prefer existing
  dependencies and the standard library before adding a dependency.
- Format Go code according to `.golangci.yaml`, including standard-library,
  external, and `github.com/projectcapsule/capsule` import groups. Format test files
  too, even where lint configuration excludes them.
- Include the repository's copyright and Apache-2.0 SPDX headers in new Go files.
  Follow the current source/linter convention; leave generated headers to tooling.
- Pass the caller's `context.Context` through API calls and work that can block.
  Preserve cancellation and deadlines; avoid detached background work per request.
- Inject clients, readers, configuration, caches, loggers, and recorders through
  existing constructors or manager setup. Avoid hidden global dependencies.
- Preserve errors with useful context and use existing Kubernetes/Capsule error
  helpers. Handle expected NotFound/conflict cases deliberately; do not swallow
  unexpected errors or turn them into successful authorization decisions.
- Use existing metadata constants, condition helpers, finalizers, and owner-reference
  utilities instead of duplicating strings or ownership logic.
- Treat shared/cache-owned objects as read-only. Copy before mutation, but avoid
  copying entire Tenant objects or large status trees when a small projection is
  sufficient. Verify input immutability where helpers are shared.
- Use structured logging and the existing events/metrics infrastructure. Keep
  routine admission logging inexpensive and avoid unbounded metric label values.
- Keep public JSON fields, defaults, validation markers, nil/empty semantics, and
  conversion behavior compatible unless the requested change explicitly alters
  that contract. Add tests for defaults and old representations when affected.

For controllers, follow the domain's `SetupWithManager` and reconciliation patterns.
Reconciliation must remain idempotent and converge after retries. Use existing
patch/status helpers, ownership rules, predicates, and indexed queries; avoid
unnecessary writes or requeues. Cover deletion, finalizers, status conditions, and
`observedGeneration` when the change affects them.

## Tenant isolation is part of correctness

- Resolve tenants and users with `pkg/tenant`, `pkg/users`, and the shared handler
  wrappers. Preserve owner-reference identity/UID checks and ownership checks;
  user-provided labels alone must not become proof of authorization.
- Scope reads, writes, selectors, resource references, replication, quotas, and
  policy evaluation to the intended tenant and namespaces. Cluster-scoped resources
  still need explicit tenant ownership and authorization handling.
- Cache keys must include every input that changes the result, including tenant,
  namespace, identity, or resource version where relevant. Shared immutable compiled
  expressions may be reused; tenant-specific results must not leak between callers.
- Preserve distinctions among tenant owners, groups, service accounts, Capsule
  administrators, and unrelated users. Reuse existing impersonation and audience
  handling rather than adding alternate privilege checks.
- Account for missing/deleted tenants, stale owner references, namespace termination,
  policy updates, and reassignment where the affected path supports them.
- Test both the intended effect on the selected tenant and the absence of an
  unintended effect on other tenants. Global policies must affect exactly their
  intended tenant set.

## Admission: performance and behavior requirements

Treat the complete request path as performance critical: routing, decoding, tenant
and user resolution, ruleset retrieval, expression evaluation, mutation, API I/O,
logging, and response construction. A small shared-helper change can affect every
Kubernetes write.

### Preserve the admission pipeline

- Reuse `internal/webhook/router.go`, routes, and the wrappers in
  `pkg/runtime/handlers/`. Preserve registration and handler order; the controller
  entry point explicitly documents that webhook order matters.
- Preserve the router's chain semantics: a nil handler response continues the
  chain, while a non-nil response ends it. An explicit allow can therefore skip
  later checks. Test the composed chain when changing short-circuit behavior.
- Check each wrapper's actual `OnUpdate` call convention before changing old/new
  object handling. Cover CREATE, UPDATE, DELETE, and subresources when relevant.
- Preserve deny/allow/audit behavior, rule precedence, audience filtering,
  defaulting, dry-run behavior, and configured failure policies. Performance work
  must not weaken tenant isolation or bypass required validation.
- Align webhook registration, match conditions, selectors, operation/resource
  filters, and chart configuration with the handler behavior. Test both requests
  that should reach the webhook and those that should be excluded.

### Bound work on every request

- Reject or skip irrelevant work as early as correctness permits, using existing
  predicates and gates. Avoid tenant/ruleset lookups for provably irrelevant
  operations; add assertions for API-call counts on these paths.
- Reuse decoded objects and resolved tenant/user/rule context through the existing
  wrappers. Reuse `internal/webhook/utils/request_reader.go` for request-local read
  caching where applicable; do not introduce duplicate reads in adjacent handlers.
- Keep the distinction between the manager's cached client and direct API reader.
  Preserve freshness requirements for authorization and quota decisions. An index
  registered on the manager cache does not make that field query valid against the
  API server; use indexes with the appropriate client.
- Avoid cluster-wide scans, per-item API queries, repeated serialization, repeated
  template parsing, and expression/regex compilation on the steady-state path.
  Extend existing caches and prepared rules. Prefer work proportional to the
  applicable rules/resources over total cluster size.
- Plan cache invalidation, pruning, update/delete handling, and concurrent access
  alongside cache reuse. Test cold, warm, invalidated, and missing-entry behavior.
- Do not introduce unbounded goroutines, retries, blocking network operations, or
  per-request client construction. Keep concurrency bounded and lock scope small.
- Measure added allocations, copies, lock contention, and API round trips. Avoid
  adding high-volume logs or expensive formatting to common allow/skip paths.

### Reuse local mutex-protected caches

- Prefer the existing process-local caches in `internal/cache/` for reusable
  compiled regexes, CEL expressions, JSONPaths, registry rules, and compiled targets.
  Use their `GetOrCompile`/`GetOrBuild` APIs and inject shared instances from the
  controller setup. Do not create a new cache per admission request or duplicate an
  existing cache with a handler-local map.
- Match the cache lifetime to the data. Shared immutable compiled artifacts can
  outlive requests; request-specific reads belong in the existing request caching
  reader. Preserve that reader's per-request lifetime and copy semantics. Do not
  promote request snapshots, lookup errors, or authorization decisions into a
  process-wide cache without an explicit freshness and invalidation design.
- Follow the existing `sync.Mutex`/`sync.RWMutex` patterns when extending a cache.
  Protect map access and publication, recheck after acquiring the write lock on a
  miss, and keep the hit path short. Never hold a shared cache lock across API I/O.
  For expensive builds, use the appropriate existing build/publication pattern and
  measure contention before introducing another concurrency mechanism.
- A mutex protects access, not data freshness or the contents of a returned
  pointer. Keep published values immutable or copy the mutable portions before
  returning/changing them; a shallow slice copy does not isolate nested maps or
  pointers. Namespace profile results must retain their tenant, namespace, rule,
  and identity distinctions in cache keys wherever those inputs affect the result.
- Reuse the applicable invalidation/reset/pruning hooks, including
  `internal/controllers/cfg/invalidator/`. Define how updates, deletion, and rule
  changes retire entries, and keep memory growth bounded by the intended working
  set or an explicit retention policy. Process-local caches are independent across
  controller replicas and cannot coordinate quota reservations or authorization.
- Add tests for reuse, concurrent misses, invalidation, mutation isolation, and
  distinct namespace profiles/tenants. For new or materially changed cache paths,
  benchmark cold misses, warm hits, and concurrent access; report allocations and
  contention along with saved work.

### Use indexers for admission lookups

- Prefer an existing keyed `Get` when the object name is known. For reverse or
  relational lookups, use the registered field indexes in `pkg/runtime/indexers/`
  with `client.MatchingFields` on the manager's cached client. Avoid listing all
  tenants/resources and filtering them in Go when an index can select candidates.
- Reuse index field constants and existing lookup helpers. Examples include
  `tenant.NamespaceIndexerFieldName` for namespace-to-tenant lookup and
  `tenant.OwnerKindIndexerFieldName` for owner-to-tenant lookup in
  `pkg/runtime/indexers/tenant/`. Apply namespace scoping to namespaced resources
  and retain all required ownership, identity, and rule checks on the results.
- If a necessary index is missing, extend the relevant domain under
  `pkg/runtime/indexers/` and register it through `AddToManager`. Keep extraction
  deterministic, cheap, and free of API calls. Account for multi-valued relationships
  and update/delete behavior; do not build a second hand-maintained reverse map.
- Custom indexes exist in the controller-runtime cache. Do not pass their synthetic
  fields to `GetAPIReader()` or another direct API client. Check which reader a
  handler actually receives and that the queried resource uses the registered cache.
  Do not silently replace an index registration failure with a full-cluster scan.
- Indexed reads are eventually consistent. Preserve required authoritative reads
  for security and quota decisions; use indexed candidate selection only where its
  freshness is sufficient. Re-reading returned candidates cannot repair a stale
  index that omitted a match. An empty indexed result must not become an unsafe
  allow decision or proof that a conflict does not exist.
- Test extraction and lookup behavior, including no matches, multiple matches,
  changed relationships, deletion, and tenant/namespace separation. Register the
  same indexes with fake clients using `WithIndex`, and exercise real cache updates
  in e2e tests. For new or materially changed lookup paths, benchmark increasing
  unrelated tenant/resource counts and verify that the lookup avoids full-list work
  and redundant API calls.

### Require performance evidence

Assess performance impact for every admission change. New or materially changed
admission execution paths require benchmarks, including applicable allow, deny,
and skip cases. Compare before/after measurements for changed paths; report absolute
measurements and scaling for new paths. Exercise single-tenant and multiple-tenant
workloads, representative rule/object sizes, and cache/concurrency states. Include
counting-reader/client tests when lookup behavior changes.

Report timing, allocations, API-call changes, and scaling behavior. Fix regressions
or explicitly document their measured cost and the required correctness tradeoff
for review; do not silently accept them. If API round trips, contention, or webhook
selection changes, also validate with a real-cluster workload. Fake-client benchmark
timings alone cannot establish production admission latency.

## Unit tests: required for every change

- Add or extend neighboring `*_test.go` files using Go's `testing` package and the
  assertions already used in that package. Prefer table-driven cases for comparable
  inputs and outcomes; use descriptive case names.
- Assert observable behavior rather than duplicating implementation details. Bug
  fixes need a regression case that fails before the fix.
- Cover positive, negative, boundary, malformed/nil/empty, and dependency-error
  cases relevant to the change. Assert meaningful errors, mutations, status, and
  preservation of unrelated fields as appropriate.
- Include tenant/namespace/identity scope where the code depends on it. Shared
  tenant-independent utilities still need e2e coverage through their consuming
  feature's tenant scenarios.
- Reuse controller-runtime fake clients and existing test doubles. Register the
  correct schemes, indexes, and status subresources. Inject errors where needed;
  fake-client success does not prove Kubernetes admission or RBAC behavior.
- Use isolated fixtures and cleanup. Run parallel tests only when they do not
  share mutable state. Exercise races for new caches or concurrent code.

## E2E tests: positive and negative tenant scenarios

Use the existing Ginkgo v2/Gomega suite in `e2e/`. It uses
`envtest.Environment{UseExistingCluster: true}`: the cluster must have the changed
Capsule controller, CRDs, RBAC, and webhooks installed. Merely compiling the suite,
using a fake client, or running against an old controller image is not an e2e pass.

### Local execution scope and GitHub coverage

- Execute locally only the new e2e tests for newly developed features and the
  existing suites for subsystems/components impacted by the change. Trace shared
  helper, rules, cache, API, and admission dependencies to identify impacted suites;
  include those consumers in the selection.
- Use Ginkgo labels or spec filters to select that scope. Verify the selection
  includes the intended new tests and affected regressions; zero selected tests
  is not a successful validation. Avoid broad labels that unnecessarily select
  unrelated components.
- When the e2e workflow is triggered by a matching pull-request path, the full e2e suite runs in GitHub Actions. Do not run an unfiltered full suite
  locally as a completion step or broaden a local run merely for extra confidence.
  A passing scoped run satisfies local e2e execution requirements; report GitHub
  full-suite results separately, including when they are pending or unavailable.
- Preserve the parallel non-configuration and serial `config` phases within the
  selected scope. Run relevant OpenShift scenarios when platform behavior is
  impacted, using the same scoped approach.

### Observe during execution; perform forensics afterward

1. Before running, choose the relevant suites and prepare observation capture.
   Record the commands/filters, controller build or image, cluster context, Ginkgo
   seed, and artifact locations so the run can be reproduced.
2. While e2e tests execute, **keep reasoning to a minimum and collect observations**.
   Capture runner output, pass/fail/skip counts, durations, failed assertions,
   controller/webhook logs, and relevant Kubernetes events or resource status.
   Preserve transient evidence before test or cluster cleanup removes it. Keep
   progress updates brief and factual; avoid speculative diagnoses or repeated
   analysis of partial output.
3. Let the planned relevant suites finish before performing forensics. Do not edit
   code, change the environment, or repeatedly restart tests in response to an
   intermediate failure. If one runner command fails before later planned phases
   start, execute the remaining relevant phases when the environment is usable.
   If the environment blocks execution, record the blocker and the unrun suites.
4. After the relevant suites finish, analyze the collected evidence together.
   Correlate failures with tenant/namespace state, admission decisions, controller
   reconciliation, and timing. Distinguish product regressions, test/fixture issues,
   and environment failures before choosing a fix.
5. Apply fixes after that analysis, then rerun the failed and newly impacted suites
   with the updated build. Preserve the original observations and report the final
   scoped results separately from full-suite GitHub results.

### Required scenario coverage

For every change, add or extend a scenario set with all applicable rows below.
**Positive and negative cases with at least one Tenant actually created are
mandatory.** A Tenant value constructed only in Go, without creating it in the
cluster, does not establish tenant context. A no-tenant case is supplementary;
for missing-tenant rejection, keep another valid tenant present.

| Scenario | Required assertions |
| --- | --- |
| Positive | An authorized actor in tenant A performs a valid operation; verify persisted state and expected reconciliation, mutation, or status. |
| Negative | An invalid or disallowed operation is rejected for the intended reason; verify no unauthorized resource or state change remains. |
| Namespace profiles | For rules features, create namespaces with different selectors/profiles within tenant A; verify each receives its intended behavior and non-selected namespaces remain unaffected. Cover relevant rule composition and label/policy updates. |
| Multiple tenants | When isolation/shared state is involved, create tenants A and B with distinct ownership or policy; prove A's rules, data, quota usage, and cached results do not incorrectly affect B. |
| Cross-tenant access | When authorization or resource selection is involved, prove an actor from A cannot read, select, mutate, or claim B's resources through the changed feature. |
| Lifecycle | Exercise affected update/delete/recreation, policy-change, namespace-selection, and cache-invalidation behavior. |

Test implementation requirements:


- Reuse helpers from `e2e/suite_test.go` and `e2e/utils_test.go`, such as
  `ownerClient`, `impersonationClient`, `NewNamespace`, `TenantReady`, and
  `TenantNamespaceReady`, plus existing cleanup helpers.
- Use tenant-owner or impersonated clients for operations whose authorization is
  under test. Use the administrator client for fixture setup where needed.
- Label test resources with `env: e2e` where the suite expects it. Use unique names
  and per-test cleanup so parallel scenarios cannot interfere.
- Wait for tenant, namespace, ruleset, and policy readiness before testing a
  decision. Use `Eventually`/`Consistently` and existing timeout/poll constants;
  do not add arbitrary sleeps to hide races.
- Negative assertions must identify the expected denial/reason. A timeout,
  transport error, unrelated RBAC rejection, or malformed fixture is not evidence
  that the intended Capsule rule works. Re-read state after rejected mutations.
- Preserve descriptive Ginkgo labels. Tests changing shared CapsuleConfiguration
  must use the `config` label and restore configuration; those tests run serially.
  Ordinary tests must remain safe under parallel execution.
- Do not weaken assertions, increase timeouts without diagnosis, mark new coverage
  skipped, or commit focused specs to make a failing run appear successful.

## Benchmarks: performance-sensitive execution paths

New or materially changed performance-sensitive execution paths require benchmark
coverage; extend existing benchmarks where appropriate. These include admission,
cache/index lookups, rule evaluation, template rendering, and reconciliation work
whose cost grows with tenant, namespace, or resource counts. Material changes
include changes to API calls, allocations, algorithms, locking, concurrency, or
scaling behavior.

Documentation-only changes and changes that do not introduce or materially alter
performance-sensitive execution paths do not require benchmarks.

Add Go `Benchmark...` functions in adjacent `*_test.go` or `*_bench_test.go` files.
Extend existing benchmarks rather than introducing a separate framework. Examples:
[pkg/tenant/rules_bench_test.go](pkg/tenant/rules_bench_test.go) and
[internal/controllers/resources/collect_bench_test.go](internal/controllers/resources/collect_bench_test.go).

- Benchmark the affected performance-sensitive behavior at a meaningful operation
  boundary, including helpers through their callers. Trivial wrapper-only
  measurements are insufficient if the affected work happens elsewhere.
- Use deterministic fixtures, `b.ReportAllocs()`, and sub-benchmarks for relevant
  input sizes. Include one and multiple tenants for tenant-dependent work, and
  vary namespaces/rules/resources where cost depends on their count. For rules
  features, include different namespace profiles and matching/non-matching rules.
- Keep fixture construction outside the timed region unless setup is what the
  benchmark measures. Use `b.Loop()` or the established `b.N`/`b.ResetTimer()` style;
  reset mutable inputs so later iterations do not accidentally measure no-ops.
- Check results/errors so the benchmark cannot silently measure a broken shortcut.
  Separate warm-cache work from cold compilation and invalidation. Use
  `b.RunParallel` when shared-state concurrency is part of the behavior.
- Compare repeated runs on the same machine, Go version, settings, and workload.
  Record `ns/op`, `B/op`, and `allocs/op`; do not benchmark with the race detector
  when making performance comparisons. Record API calls separately where relevant.
- Capture the baseline before changing existing behavior and rerun the same
  benchmark afterward. For entirely new paths, report absolute measurements and
  scaling, and confirm existing neighboring paths have not regressed.
- Do not add arbitrary timing thresholds to unit tests. Use benchmark comparisons
  and workload evidence; keep correctness assertions deterministic.

## Commands and validation

Run commands from the repository root. Use the checked-in Makefile's tool versions
and `go.mod` instead of independently selecting newer tools.

| Purpose | Command / guidance |
| --- | --- |
| Focused unit tests | `go test -race ./path/to/changed/package/...` (replace the path). |
| Full unit suite | `make test` runs non-e2e packages with race detection and coverage, and invokes generation. Inspect resulting generated diffs. |
| Go lint | `make golint`. Format changed files first and inspect any automatic fixes. |
| Deep-copy generation | `make generate`. |
| CRD generation | `make manifests` (also invokes generation). |
| Build controller | `go build -o bin/manager ./cmd/controller`. The entry point is under `cmd/controller/`. |
| Prepare local e2e cluster | `make e2e-build` creates a KinD cluster and builds/installs Capsule. Requires Docker and the target's cluster tooling; follow with a scoped test run. |
| Scoped local e2e | `make e2e-exec FILTER='&& !skip && scheduler'`. Replace the example label with the new feature/impacted component selection. Runs selected non-configuration tests in parallel, then selected configuration tests serially. Ensure the cluster runs the current changes. |
| Scoped configuration e2e | `make e2e-exec-config FILTER='&& !skip && feature-label'`. Replace `feature-label` with the relevant existing label. |
| Scoped OpenShift e2e | Prepare with `make e2e-build-openshift`, then use `make e2e-exec FILTER='&& !skip && !skip-on-openshift && feature-label'` for impacted platform behavior, replacing `feature-label`. |
| Full e2e in GitHub Actions | `make e2e` and `make e2e-openshift` are the full-suite CI entry points; local execution uses the scoped commands above. |
| Local e2e cleanup | `make e2e-destroy` or `make e2e-destroy-openshift` after preserving observations and completing the relevant run. |
| Focused benchmarks | `go test ./path/to/changed/package -run '^$' -bench 'BenchmarkName' -benchmem -count=5` (replace path/name). |
| Existing benchmark examples | `go test ./pkg/tenant ./internal/controllers/resources -run '^$' -bench . -benchmem -count=5`. |
| Chart checks | `make helm-lint`; use `make helm-test` for installation behavior. |
| Chart documentation/schema | `make helm-docs` and `make helm-schema` when chart values or documentation change. |
| Diff hygiene | `git diff --check` and review the full diff, including new files. |

Do not use `go test ./...` as a unit-only shortcut: `e2e/` contains a suite that uses
the configured cluster. E2E and chart installation commands mutate cluster state;
use a dedicated test cluster and verify the context. Inspect lifecycle targets
before overriding cluster names, including their cleanup behavior.

For scalability work, follow [e2e/stress/README.md](e2e/stress/README.md). This
environment supplements unit benchmarks and functional e2e tests; seeding a workload
alone is not performance evidence. Record the workload, controller version, resource
usage, request latency/throughput, and errors for any reported comparison.


## Mandatory self-review and iteration

Every change, including documentation, configuration, tests, and generated changes,
must receive a self-review before completion. Review the final diff and affected
callers against the requested behavior and this repository's requirements. Provide
a concise assessment supported by code inspection, tests, or measurements for each
area; if no impact is expected, explain why rather than omitting the area.

- **Scalability:** Assess how work and retained state grow with tenants, namespaces,
  rules, resources, and concurrent requests. Look for full-list scans, per-item API
  calls, unbounded caches or queues, reconciliation fan-out, and contention. Explain
  relevant bounds and behavior as unrelated tenants or resources are added.
- **Performance:** Assess admission and reconciliation latency, CPU, allocations,
  memory, API round trips, repeated compilation/serialization, and cache reuse.
  Follow the benchmark and real-cluster evidence requirements above for affected
  paths; distinguish measured results from expectations and identify regressions.
- **Security:** Assess tenant and namespace isolation, authorization and ownership,
  privilege boundaries, untrusted input, resource/reference scoping, cache freshness,
  failure behavior, sensitive data exposure, and denial-of-service risks. Check
  negative cases and ensure optimizations do not bypass enforcement.

Use the following loop for the initial change and every subsequent revision:

1. Review the current diff and available validation evidence. Identify concrete
   findings, assumptions, missing coverage, and feedback from the user or reviewers.
2. Revise the change to address actionable findings and feedback. Add or update
   regression coverage and performance evidence where required. If feedback does
   not warrant a change, explain the decision with evidence.
3. Rerun checks affected by the revision and inspect the resulting diff. Keep local
   e2e runs scoped to the affected components and complete planned suites before
   analyzing failures, as required above.
4. Repeat the self-review across all three areas until no actionable findings remain
   unresolved, feedback has been addressed, and required checks pass. Review fixes
   for new regressions; do not stop at identifying issues or rely on passing tests
   alone as evidence that the review is complete.

In the handoff or PR, summarize the scalability, performance, and security
assessments, findings addressed, validation evidence, and remaining risks or
limitations. Disclose blocked checks and unresolved findings explicitly; do not
declare the change fully validated while required evidence is missing.

## Generated files, charts, and completion

- Edit API source and Kubebuilder markers, then regenerate. Do not hand-edit
  `zz_generated.deepcopy.go` or generated CRDs under `charts/capsule/crds/`.
- Keep API types, defaults/validation, conversions, CRDs, RBAC, webhook rules, and
  tests consistent. Review generated changes for unintended schema/default changes.
- Update chart values, templates, schema, and documentation together when affected.
  Edit `charts/capsule/README.md.gotmpl` and regenerate its README. Follow
  `DEVELOPMENT.md` for chart changelog annotations; release version bumps belong to
  the release process.
- Keep credentials, kubeconfigs, certificates/private keys, test artifacts, and
  benchmark output out of commits. Preserve unrelated local files.
- Describe the resulting namespace profile behavior, rules API integration, reused
  extension points, tenant scenarios, commands/results, and benchmark evidence when
  required in the handoff or PR. Identify unrun checks and their concrete blockers;
  never claim success from compilation alone or omit required e2e/performance
  evidence. Separate scoped local e2e results from the full-suite GitHub status and
  summarize forensic findings after the relevant suites finish.
- If preparing commits or a PR, follow the repository's Conventional Commit and
  DCO requirements in `CONTRIBUTING.md`.

Before declaring completion, verify that the change advances namespace profiling,
uses the rules API as its primary feature extension point, follows the existing
structure, reuses available code, includes unit and tenant-aware positive/negative
e2e coverage, includes benchmarks for new or materially changed performance-sensitive
execution paths, and preserves isolation/API contracts. Assess performance impact
for every admission change and supply the required evidence. Explain any necessary
departure from the rules API approach. Complete the self-review and iteration loop
above, including the scalability, performance, and security assessments.
