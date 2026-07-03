# Health checks for `TenantResource` / `GlobalTenantResource`

Both `TenantResource` and `GlobalTenantResource` expose a `Ready` condition that
reflects whether the declared manifests were successfully **applied**. This says
nothing about whether the **resulting objects** are actually healthy (e.g. a
replicated `Deployment` may be applied but never become available).

The optional `spec.healthChecks` field adds a second condition, `Healthy`, which
reflects the health of the replicated objects.

## How health is evaluated

Every successfully-applied item in `status.processedItems` is evaluated:

1. **Custom CEL expressions** — if a `healthChecks` entry matches the object's
   `apiVersion` and `kind`, the object is classified using its expressions, in
   order:
   - `failed` is true → **unhealthy**
   - otherwise `inProgress` is true → **in progress** (holds back a premature
     healthy verdict while the object is still settling)
   - otherwise `current` is true → **healthy**
   - otherwise → **in progress**
2. **kstatus (default)** — objects without a matching entry are evaluated with
   [kstatus], which understands the built-in workloads (Deployments, Jobs,
   StatefulSets, …) and the `Ready`-condition convention used by most custom
   resources. `Current` → healthy, `Failed` → unhealthy, anything else → in
   progress.

The per-object outcomes are aggregated into the `Healthy` condition:

| Aggregate                        | `Healthy` status | reason        |
| -------------------------------- | ---------------- | ------------- |
| any object unhealthy             | `False`          | `Failed`      |
| any object in progress           | `Unknown`        | `Progressing` |
| all objects healthy / nothing to check | `True`     | `Succeeded`   |

While the resource is reconciling, cordoned, gated on `dependsOn`, or being
deleted, `Healthy` is set to `Unknown`.

`Healthy` is independent of `Ready`: it does **not** gate `Ready`, and
`dependsOn` still only waits on `Ready`.

## CEL variable model

The expressions follow the Flux [`healthCheckExprs`][flux] convention: the
object's top-level fields are exposed directly, so an expression can reference
`status`, `metadata`, `spec`, `data`, etc. For example, the issue's original
request works as-is:

```cel
status.conditions.filter(e, e.type == 'Synced').all(e, e.status == 'True')
```

Notes:

- Expressions must evaluate to a **boolean**.
- Referencing an absent field (e.g. `status` before the controller writes it)
  does not error the reconcile — it simply does not match, so the object is
  treated as *in progress*.
- The Secret `type` field is not exposed, because `type` collides with the CEL
  built-in identifier.

## Example

```yaml
apiVersion: capsule.clastix.io/v1beta2
kind: GlobalTenantResource
metadata:
  name: example
spec:
  resyncPeriod: 60s
  tenantSelector:
    matchLabels:
      env: prod
  healthChecks:
    - apiVersion: example.com/v1
      kind: Database
      current: "status.conditions.filter(e, e.type == 'Ready').all(e, e.status == 'True')"
      failed: "status.conditions.exists(e, e.type == 'Ready' && e.status == 'False' && e.reason == 'Fatal')"
  resources:
    - namespaceSelector: {}
      rawItems:
        - apiVersion: example.com/v1
          kind: Database
          metadata:
            name: shared-db
          spec: {}
```

## Where to look

- **`status.conditions[type=Healthy]`** — the aggregate condition. Its message
  names up to the first few offending objects, e.g.
  `3 unhealthy: team-a/shared-db (Database), team-b/shared-db (Database), team-c/shared-db (Database) and 5 more`.
- **`status.processedItems[*].healthy` / `healthMessage`** — the complete,
  non-truncated per-object health. Use this to pinpoint every unhealthy replica
  on large fleets, e.g. `kubectl get gtr example -o json | jq '.status.processedItems[] | select(.healthy=="False")'`.
- **Metrics** — the `Healthy` condition is exported alongside `Ready` on the
  `capsule_global_resource_condition` / `capsule_resource_condition` gauges.

## Re-evaluation cadence

Health is recomputed on every reconcile, and the controller requeues at least
every `spec.resyncPeriod`. There are no dynamic watches on the checked GVKs, so a
change in an object's health becomes visible within one resync period rather than
instantly.

## Validation

`spec.healthChecks` expressions are compiled by a validating admission webhook,
so invalid expressions (or entries declaring neither `current` nor `failed`) are
rejected at `kubectl apply` time. If the webhook is disabled, a compile error
instead surfaces at runtime as `Healthy=False` with the error in the message.

The RBAC of the impersonated ServiceAccount (or the controller ServiceAccount
when none is set) must allow `get` on the checked GVKs; otherwise the objects are
reported as *in progress*.

[kstatus]: https://github.com/kubernetes-sigs/cli-utils/tree/master/pkg/kstatus
[flux]: https://fluxcd.io/flux/components/kustomize/kustomizations/#health-check-expressions
