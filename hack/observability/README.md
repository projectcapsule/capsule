# Observability for disposable test clusters

One upstream `grafana/alloy` Helm release (chart 1.12.1, Alloy v1.19.2) runs in
`capsule-observability` per cluster/run. The shared services are managed in the
`projectcapsule` infrastructure repository. This is test infrastructure supporting
investigation of namespace profiling and admission; it adds no rules API or
controller behavior.

## GitHub environment

Create the **monitoring** environment in
[projectcapsule/capsule settings](https://github.com/projectcapsule/capsule/settings/environments)
and add these **environment secrets**:

| Secret | Value |
| --- | --- |
| `MONITORING_USERNAME` | A user from the monitoring Gateway's `.htpasswd` list, for example `monitoring` |
| `MONITORING_PASSWORD` | That user's original password, not its htpasswd hash |

The `e2e` workflow references `environment: monitoring`. Its setup step reads
these secrets through `env`, explicitly preserves only the necessary environment
variables through `sudo`, and sends a Kubernetes Secret through kubectl stdin.
`make alloy-install` uses `envsubst` to render the Secret and Helm values directly
into kubectl and Helm stdin. Credentials do not enter Helm values, command
arguments, generated files, or the run summary. Alloy mounts the Secret as files.
There is no second manually managed collector Secret in each test cluster.
The target creates or replaces that Secret using `stringData` without a
last-applied annotation. Replacement checks its resource version and removes any
annotation left by the earlier client-side apply implementation.

The `e2e` workflow runs on matching branch pushes and manual `workflow_dispatch`
runs in **projectcapsule/capsule**. Every job checks the repository, event, and
branch ref before starting. Pull-request events, PR refs, tags, and runs in forks
are excluded. All checkouts use the event's exact commit (`github.sha`) from
`projectcapsule/capsule`; no caller-supplied repository or SHA is accepted, and
checkout credentials are not persisted.
This intentionally provides no automatic e2e result for fork PRs. To validate a
contribution with this workflow, a maintainer must first review and promote it to
a trusted branch in `projectcapsule/capsule`.

In the **monitoring** environment settings, restrict deployment branches to the
repository branches trusted to execute with these credentials. These GitHub
settings are managed separately from the workflow. Keep monitoring credentials
as environment secrets. Do not add `pull_request_target` or check out PR code in
these jobs; see [GitHub's guidance on untrusted code checkout](https://docs.github.com/en/actions/reference/security/secure-use#mitigating-the-risks-of-untrusted-code-checkout).

With both secrets absent, tests run with remote collection disabled. A partially
configured credential pair fails setup. Environment secrets authorize their
holder to access the shared monitoring endpoints, including queries; basic auth
is not write-only.

## Collected data

- Pod logs across the disposable cluster, excluding Alloy's own namespace, via
  `loki.source.kubernetes` and the Kubernetes API. No host log mounts are needed.
- Kubernetes events across all namespaces, including transient tenant fixtures.
- Each Capsule manager pod's `:8080/metrics`, every 15 seconds. This includes
  controller-runtime, workqueue, process/Go, and Capsule resource metrics.
- Alloy's own metrics every 15 seconds, including delivery errors and queues.
- Capsule CPU, memory and goroutine profiles from `:8082`, every 60 seconds.
  `capsule-values.yaml` enables pprof and JSON logging only for instrumented tests.
  The unauthenticated metrics and pprof ports remain internal to this disposable
  cluster; no Service, Ingress or Gateway exposes pprof publicly.
- Admission webhook traces, sent by Capsule to Alloy's internal OTLP/gRPC port
  `4317`, then forwarded over HTTPS to Tempo. The test overlay enables tracing
  with a sample ratio of 1.0. Alloy also accepts OTLP/HTTP on internal port `4318`.
  Traces use the existing monitoring credential Secret; no new GitHub secrets
  are required.

Metrics, logs and profiles carry the same `run_id`, `repository`, and `revision`.
Alloy adds the same fields as resource attributes on traces, preserving Capsule's
`service.name`. In Grafana's Tempo data source, use
`{ resource.service.name = "capsule" && resource.run_id = "<run-id>" }`.
All four central backends have seven-day retention policies; actual physical
deletion follows their background cleanup schedules.
CI IDs include workflow run, attempt, job, and matrix version. Local runs use a
UUID unless `OBSERVABILITY_RUN_ID` is supplied. Run metadata is part of the Helm
pod template, so installing with a different run ID automatically rolls Alloy.
Start a fresh cluster/collector for each run; never reuse one global run label for
overlapping runs. Keep individual test names and admission UIDs in logs rather
than additional metric labels.

The Actions summary links to the **Capsule runs** Grafana dashboard with the run
selected. It is provisioned by the infrastructure repository. For profiles, use
the Pyroscope data source in Explore with `service_name="capsule"` and that run ID.

The collector has read-only access to pods, pod logs, and events. It cannot read
Kubernetes Secrets, edit workloads, or access nodes. All containers use restricted
PSS settings; the OpenShift overlay lets the SCC assign UID/groups. This is an
administrator-operated collector for a dedicated test cluster, not a tenant-scoped
installation for a shared production cluster.
With `rbac.namespaces: []`, chart 1.12.1 combines `rbac.rules` and
`rbac.clusterRules` in a ClusterRole, with a ClusterRoleBinding for Alloy's
ServiceAccount. Pod/event discovery therefore has cluster-wide list/watch access.

## Local scoped run

Use a dedicated disposable cluster. These commands use the existing `capsule`
KinD cluster name; do not run them against a cluster you want to keep. Supply the
two credential variables through your local secret manager/environment first.
The install target needs Helm, kubectl, Bash, `envsubst` (gettext), and `jq`;
local run ID generation also needs `uuidgen`. Set `OBSERVABILITY_RUN_ID` to choose
your own ID, and optionally set `OBSERVABILITY_REPOSITORY`,
`OBSERVABILITY_REVISION`, and `OBSERVABILITY_RUN_URL`.

```sh
make e2e-cluster
make alloy-install
make e2e-install E2E_OBSERVABILITY=true
make e2e-exec FILTER='&& !skip && scheduler'
# Capture diagnostics before stopping collection if a test failed.
make alloy-uninstall
make e2e-destroy
```

Choose a filter for the subsystem under investigation. For OpenShift, use
`e2e-cluster-openshift`, `alloy-install-openshift`, `e2e-install-openshift`, and
`e2e-destroy-openshift`, adding `!skip-on-openshift` to the test filter.
The original `make e2e` targets still work without observability; the GitHub
workflow separates their stages to restrict credentials to collector setup.

The collector uses a bounded ephemeral volume for its metrics WAL. Stop it while
the cluster still exists: Helm waits for shutdown with a 120-second pod grace
period, then `make alloy-uninstall` deletes the client Secret. Secret deletion is
also attempted if Helm fails or times out, and the target still reports the Helm
failure. A failed Secret deletion also fails the target. A terminated runner,
prolonged backend outage, pod replacement, or exhausted volume can lose unsent telemetry.
This is deliberately a single, non-HA collector, not a durable ingestion queue.
GitHub preserves failed clusters for diagnostics until runner cleanup.

Node filesystem logs, audit logs and GitHub runner/Ginkgo output are not pod logs
and are not collected by this configuration. Runner output stays in Actions.
The trace queue is bounded to 100 batches, with a memory limiter and finite
retries. Failed exports and forced collector shutdowns can lose queued traces.

## Verification

```sh
alloy validate hack/observability/config.alloy
export RUN_ID_JSON='"render-check"' REPOSITORY_JSON='"projectcapsule/capsule"'
export REVISION_JSON='"local"' RUN_URL_JSON='""'
envsubst '${RUN_ID_JSON} ${REPOSITORY_JSON} ${REVISION_JSON} ${RUN_URL_JSON}' \
  < hack/observability/values.yaml | \
helm template capsule-alloy alloy --repo https://grafana.github.io/helm-charts \
  --version 1.12.1 --namespace capsule-observability \
  --values - \
  --set-file alloy.configMap.content=hack/observability/config.alloy
```

Repeat rendering with `--values hack/observability/openshift-values.yaml`.
After an instrumented scoped e2e run with real tenant fixtures, verify both
controller pods appear in `up{job="capsule",run_id="<run>"}`, tenant condition
metrics appear, allowed and denied operations have their expected outcomes, pod
logs/events have that run label, and profiles and admission traces are available.
A different run must
not appear when filtering by this run. Check the collector logs for authentication
or delivery errors. Rendering does not establish live ingestion,
OpenShift SCC admission, or the outcome of tenant e2e tests.

API log streaming adds kubelet/API-server traffic proportional to the number of
containers; metrics and profiling add scrape work per controller replica. There
is no per-admission code change. Run labels increase historical series/streams
with the number of retained runs; central retention and storage bound history,
and collector resources/WAL size bound its local resource use.
