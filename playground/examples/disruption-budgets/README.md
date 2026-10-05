# PodDisruptionBudget policies

Apply [tenant.yaml](tenant.yaml) as a Capsule administrator. This complete Tenant
example selects namespaces labeled `workload-profile: production`, prevents
PDB overlap, requires a configured allowance of one to two evictable replicas
per selected controller, and permits only `AlwaysAllow` for unhealthy Pod eviction.
Other namespace profiles are unaffected. No PDB is required.

## Targets

| Target | Overlap | Unhealthy eviction policy | Evictable replicas |
| --- | --- | --- | --- |
| Omitted or `pod` | ✅ Pod labels | ✅ PDBs selecting the Pod | ❌ |
| `deployment`, `statefulset`, `replicaset`, `replicationcontroller` | ✅ Template labels | ✅ PDBs selecting the template | ✅ Desired `spec.replicas`, including `/scale` |
| `daemonset`, `job`, `cronjob` | ✅ Template labels | ✅ PDBs selecting the template | ❌ No supported desired replica count |
| Container or volume part targets | ❌ | ❌ | ❌ |

Controller checks are opt-in. Include `pod` to check actual Pod labels as well as
controller templates. CronJobs use their nested Job Pod template. An
`evictableReplicas` rule must explicitly include at least one supported controller
target. Other targets in the same rule still participate in the other checks.

## Evictable replicas

`evictableReplicas.min` and `.max` are inclusive integer bounds. Omitted bounds
are unrestricted. Capsule evaluates every matching PDB against each selected
controller's desired replica count, assuming all those replicas are healthy:

- `minAvailable: N`: allowance is `max(0, replicas - N)`.
- `minAvailable: "P%"`: allowance is `max(0, replicas - ceil(replicas * P / 100))`.
- `maxUnavailable`: allowance is the given integer or rounded-up percentage,
  capped at the desired replica count.
- If both fields are omitted, the allowance is zero, matching Kubernetes' PDB
  controller behavior.

This follows [Kubernetes' percentage rounding](https://kubernetes.io/docs/tasks/run-application/configure-pdb/#rounding-logic-when-specifying-percentages).

| PDB configuration | Desired replicas | Configured allowance | With `min: 1` |
| --- | ---: | ---: | --- |
| `minAvailable: "75%"` | 3 | 0 | Rejected |
| `minAvailable: "75%"` | 4 | 1 | Allowed |
| `maxUnavailable: "25%"` | 1 | 1 | Allowed; the only replica may be evicted |
| `maxUnavailable: 0` | 4 | 0 | Rejected |

Zero-replica controllers are exempt from this count check, so scaling down to zero
is allowed. Scaling back above zero checks the resulting allowance. Overlap and
unhealthy-policy checks still apply to zero-replica templates.

The check runs on controller creation, changed template labels or replicas, PDB
creation or relevant spec changes, and supported `/scale` updates used by
`kubectl scale` and autoscalers. A scale request uses the submitted replica count
and the stored parent template. Unsupported/custom controller scales are outside
this policy's coverage.

This is a **per-controller configuration check**, not a calculation of the live
PDB's `status.disruptionsAllowed`. If a PDB selects multiple controller templates,
each must independently satisfy the bounds; Capsule does not add their replicas
or count a Deployment's ReplicaSets again. This can be stricter than Kubernetes'
combined budget. Partial selectors and arbitrary groups of Pods do not establish
that all desired replicas are covered. Use a PDB selector that covers the complete
intended controller, as recommended in the Kubernetes documentation.

Pending, unhealthy, terminating or missing Pods, rollout state and concurrent
writes can still prevent eviction. Setting `max` does not cap aggregate live
disruptions across multiple controllers, and PDBs do not constrain direct Pod
deletions or controller scale-down. Capsule neither reserves eviction slots nor
guarantees drain completion.

## Unhealthy Pod eviction

`unhealthyPodEvictionPolicies` matches the effective
`spec.unhealthyPodEvictionPolicy`. Omission in the PDB is treated as
`IfHealthyBudget`. An allow rule listing only `AlwaysAllow` therefore rejects a
matching PDB that omits this field. A deny rule listing `IfHealthyBudget` rejects
that value, including omission. An empty list adds no restriction.

`AlwaysAllow` allows unhealthy Running Pods to be evicted even when the healthy
budget is not satisfied. Healthy Pods still obey the PDB. See the
[Kubernetes unhealthy eviction policies](https://kubernetes.io/docs/tasks/run-application/configure-pdb/#unhealthy-pod-eviction-policy).

A PDB with no existing selected workload can be created; later workload admission
checks it. This avoids requiring a particular deployment order. It does not
require a PDB or create one automatically.

## Overlap

Kubernetes accepts overlapping PDBs, but eviction of a Pod selected by multiple
PDBs can fail and block a drain ([Kubernetes eviction behavior](https://kubernetes.io/docs/concepts/scheduling-eviction/api-eviction/#how-api-initiated-eviction-works)).
`allowOverlap: false` checks Pod labels and selected controller templates against
namespace PDB selectors. A null selector matches nothing; `{}` matches every Pod
in the namespace. Both `matchLabels` and `matchExpressions` are supported.

On PDB creation or selector changes, Capsule checks existing Pods and selected
controller templates. Only overlaps involving the submitted PDB are considered.
On selected workload creation or label changes, it checks the resulting Pod labels.
Pod label changes through `/status` are covered too; unchanged status labels skip
all tenant/ruleset and PDB reads.

Two PDBs selecting `app: frontend` can exist before any matching Pod or selected
template. Creating a matching workload is then rejected. If the workload already
exists, creating the second matching PDB is rejected instead. Fix selectors or
delete a conflicting PDB to restore admission.

## Rule composition and lifecycle

For `allowOverlap` and `evictableReplicas`, allow rules require compliance;
deny/audit rules match violations, following resource-constraint semantics.
`unhealthyPodEvictionPolicies` follows enum-list matching: allow accepts listed
values, deny rejects listed values, and audit reports listed values.

The last matching allow/deny decision wins **for each property**. A later
`allowOverlap: true` allow rule can permit overlap; a later `evictableReplicas.min: 0`
allow rule can relax an earlier minimum. Allowing one property does not override
a failure of another property. Omitted properties never override another rule.
Audit rules report without changing the admission decision. Reports are bounded
to one example per kind, property and distinct matching audit constraint.

Namespace selectors, audiences and `enforce.conditions` use the normal rules
pipeline. Conditions inspect the actual incoming object: a PDB on a PDB write,
a controller on a controller write, and an `autoscaling/v1` Scale on `/scale`.
Conditions or audiences restricted to only one write path can leave other paths
unchecked. Include all relevant actors and operations.

Unchanged relevant fields skip checks. DELETE is allowed for remediation. A
namespace profile or rule update governs subsequent relevant admissions; it does
not repair existing PDBs or immediately revalidate stored workloads.

## Consistency and cost

Checks use authoritative, paginated, namespace-scoped reads. A selected workload
change lists namespace PDBs once for all three properties. PDB writes list selected
workload kinds; overlap checks also list other PDBs. Controller lists inspect Pod
template labels because API label selectors filter the controller's own metadata.
A changed `/scale` with an applicable replica constraint adds one parent GET.
No cluster-wide scans or per-item GET loops are used.

Compiled selectors use a bounded shared cache. Live resource state and policy
decisions are request-local. Admission reads and writes across resources are not
atomic: concurrent PDB/controller writes can race. There is no background repair
or automatic PDB deletion. Monitor actual PDB status and rollout health when
planning maintenance.

Run focused integration coverage against a cluster running this implementation:

```sh
make e2e-exec FILTER='&& disruption-budgets'
```
