---
title: Enforcement
weight: 2
description: >
  Configure policies and restrictions and enforce rules per namespace
---

Namespace rules can enforce admission behavior for selected resources in Tenant namespaces. Each `enforce` block can define an `action` and one or more matchers.

Rules are evaluated in declaration order. If multiple `allow` or `deny` rules match the same request, the **last matching allow or deny rule wins**. If at least one `allow` rule is configured for a workload matcher and no `allow` or `deny` rule matches the evaluated value, Capsule denies the request. In other words, `allow` rules create an allow-list for that matcher. `audit` rules are purely observational: they never influence the allow/deny decision, but all matching audit rules emit Kubernetes events and add admission warnings.

## Action

Each `enforce` block supports an `action` field:

| Action | Behavior |
|---|---|
| `allow` | Allows the matching request and enables allow-list behavior for the matcher. If at least one allow rule exists and no allow or deny rule matches a value, Capsule denies that value. Additional constraints, such as image pull policy, must also be satisfied. |
| `deny` | Denies the matching request. A later matching `allow` rule can override it. |
| `audit` | Emits a Kubernetes event and returns an admission warning when it matches. It does not allow or deny the request. |

If `action` is omitted, Capsule treats the rule as `deny`.

Allow-list behavior is evaluated per workload matcher and per evaluated value. For example, if a registry allow rule exists for `harbor/.*`, a Pod image from `docker.io/library/nginx:latest` is denied unless another later or earlier allow rule also matches that image. Audit rules do not satisfy this allow-list requirement.

This precedence model allows both broad defaults and specific exceptions. For example, you can allow all Harbor images but deny a customer path afterwards:

```yaml
rules:
  - enforce:
      action: allow
      workloads:
        registries:
          - exp: "harbor/.*"

  - enforce:
      action: deny
      workloads:
        registries:
          - exp: "harbor/customer/.*"
```

In this example, `harbor/nginx:1.14.2` is allowed, while `harbor/customer/app:1.0.0` is denied because the later, more specific deny rule also matches.

You can also deny broadly and allow a more specific exception afterwards:

```yaml
rules:
  - enforce:
      action: deny
      workloads:
        registries:
          - exp: "harbor/customer/.*"

  - enforce:
      action: allow
      workloads:
        registries:
          - exp: "harbor/customer/prod-image/.*"
```

In this example, `harbor/customer/test-image/app:1.0.0` is denied, while `harbor/customer/prod-image/app:1.0.0` is allowed.

## Audience

Use `audience` to restrict a rule to requests made by specific users, groups,
service accounts, or Capsule-defined subject categories. The property belongs to
the root of a rule, alongside `enforce`:

```yaml
spec:
  rules:
    - audience:
        - kind: Group
          name: system:authenticated
      enforce:
        action: allow
        metadata:
          - kinds:
              - ConfigMap
            annotations:
              example.corp/cost-center:
                required: true
                values:
                  - exp: "^INV-[0-9]{4}$"
```

When `audience` is omitted or empty, the rule applies to every request, which is
the same behavior as rules created before audience filtering was introduced.

When an audience is configured, the rule applies if the requesting subject
matches **at least one** entry. In other words, entries are combined with logical
OR semantics. Audience matching is performed consistently for both validation
and mutation, so a subject excluded from a rule is neither validated nor mutated
by that rule.

The supported standard audience kinds are `User`, `Group`, and
`ServiceAccount`:

```yaml
spec:
  rules:
    - audience:
        # Match one exact Kubernetes username.
        - kind: User
          name: alice@example.com

        # Match any request carrying this group.
        - kind: Group
          name: oidc:engineering

        # Match one Kubernetes service account.
        - kind: ServiceAccount
          name: system:serviceaccount:delivery:deployer
      enforce:
        action: deny
        metadata:
          - apiGroups:
              - v1
            kinds:
              - Namespace
            labels:
              pod-security.kubernetes.io/enforce:
                managed: restricted
```

For `User`, `Group`, and `ServiceAccount`, `name` is compared with the identity
information provided in the Kubernetes admission request. A service account is
represented by its canonical Kubernetes username:

```text
system:serviceaccount:<namespace>:<service-account-name>
```

For example, a request from the `deployer` service account in the `delivery`
namespace has the username
`system:serviceaccount:delivery:deployer`.

### Custom

The `Custom` kind exposes audiences based on Capsule's internal identity and
tenant resolution. Its `name` must be one of the supported values below.

| **name** | **description** |
|:---|:---|
| `CapsuleUser` | Matches subjects listed by `configuration.Users()`. |
| `Administrator` | Matches subjects listed by `configuration.Administrators()`. |
| `TenantOwner` | Matches an owner of the tenant resolved for the current request. A request cannot match when no tenant can be resolved. |
| `Controller` | Matches the service account used by the Capsule controller. |

Custom audiences can be combined with standard audiences. The following rule
applies to Capsule users, Capsule administrators, tenant owners, the Capsule
controller, or members of the Kubernetes `system:masters` group:

```yaml
spec:
  rules:
    - audience:
        - kind: Custom
          name: CapsuleUser
        - kind: Custom
          name: Administrator
        - kind: Custom
          name: TenantOwner
        - kind: Custom
          name: Controller
        - kind: Group
          name: system:masters
      enforce:
        action: allow
        metadata:
          - apiGroups:
              - v1
            kinds:
              - Namespace
            annotations:
              example.corp/cost-center:
                default: II-1
```

`TenantOwner` is request-scoped. Capsule first resolves the tenant associated
with the admission request and then checks the requesting subject against that
tenant's owners. This makes it suitable for rules that should affect tenant
owners but not unrelated Capsule users:

```yaml
spec:
  rules:
    - audience:
        - kind: Custom
          name: TenantOwner
      enforce:
        action: allow
        metadata:
          - kinds:
              - ConfigMap
            labels:
              owner-managed:
                default: "true"
```

`Controller` specifically identifies the Capsule controller service account. It
is useful when internal reconciliation requests need different policy behavior
from requests made by ordinary users:

```yaml
spec:
  rules:
    - audience:
        - kind: Custom
          name: Controller
      enforce:
        action: allow
        metadata:
          - kinds:
              - Secret
            labels:
              capsule.clastix.io/reconciled:
                managed: "true"
```

Unknown audience kinds and unsupported `Custom` names are rejected when the
rule is admitted. This catches spelling mistakes and prevents a rule from being
silently configured with an audience that can never match.


## Match expressions

Several workload rule types use a common match expression structure. A matcher must define at least one of `exact` or `exp`. Both fields may be set together; in that case, the matcher succeeds when either the exact list or the regular expression matches.

```yaml
exact:
  - value-a
  - value-b
exp: "value-[0-9]+"
```

| Field | Description |
|---|---|
| `exact` | A list of exact values. The matcher succeeds when the evaluated value equals one of the listed values. |
| `exp` | A regular expression matched against the evaluated value. |
| `negate` | Negates the final match result. This applies to both `exact` and `exp`. |

For example, this matcher matches `registry.local/team-a/app:1.0.0`, `registry.local/team-b/app:1.0.0`, or any reference under `registry.local/shared/*`:

```yaml
exact:
  - registry.local/team-a/app:1.0.0
  - registry.local/team-b/app:1.0.0
exp: "registry.local/shared/.*"
```

With `negate: true`, the final match result is inverted. This means negation applies to exact values as well as regular expressions:

```yaml
exact:
  - registry.local/blocked/app:1.0.0
exp: "registry.local/deprecated/.*"
negate: true
```

This matcher succeeds for every value except `registry.local/blocked/app:1.0.0` and values matching `registry.local/deprecated/.*`.

## Audit

Use `action: audit` to observe workload usage without directly blocking the request. Audit rules emit Kubernetes events and add warnings to the admission response, but they do not allow or deny the request. If an allow-list is active for the same matcher and no allow rule matches the evaluated value, the request is still denied even when an audit rule matches.

For registry enforcement:

```yaml
---
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: solar
spec:
  ...
  rules:
    - enforce:
        action: audit
        workloads:
          targets:
            - pod/containers
          registries:
            - exp: "docker.io/.*"
```

Applying a Pod with `docker.io/library/nginx:latest` succeeds in this audit-only example because no registry allow-list is configured. The API server response contains an admission warning and Capsule emits a related event for the Pod.

For QoS enforcement:

```yaml
rules:
  - enforce:
      action: audit
      workloads:
        qosClasses:
          - Burstable
```

Applying a `Burstable` Pod succeeds in this audit-only example because no QoS allow-list is configured. Capsule emits an event and returns an admission warning.

For scheduler enforcement:

```yaml
rules:
  - enforce:
      action: audit
      workloads:
        schedulers:
          - exact:
              - custom-scheduler
```

Applying a Pod with `spec.schedulerName: custom-scheduler` succeeds in this audit-only example because no scheduler allow-list is configured. Capsule emits an audit event and returns an admission warning.

When audit rules are used together with allow rules, the matching value must still be allowed explicitly. For example, an audited registry reference that does not match any registry `allow` rule is denied by the allow-list, but Capsule still emits the audit event before denying the request.

## Workloads

Enforcement for workloads mainly targets `Pods` and their associated resources.

Workload enforcement is configured under `spec.rules[].enforce.workloads`. Each rule can define an `action`, optional workload `targets`, and one or more workload matchers such as registry match expressions, scheduler match expressions, or QoS classes.

### QoS Classes

QoS class enforcement allows administrators to allow, deny, or audit Pods based on their [computed Kubernetes QoS class](https://kubernetes.io/docs/concepts/workloads/pods/pod-qos/).

QoS rules are configured under `enforce.workloads.qosClasses`.

Supported QoS classes are:

| QoS class | Description |
|---|---|
| `Guaranteed` | The Pod has CPU and memory requests and limits set so that requests equal limits. |
| `Burstable` | The Pod has at least one CPU or memory request or limit, but does not qualify as `Guaranteed`. |
| `BestEffort` | The Pod has no CPU or memory requests or limits. |

Capsule evaluates the QoS class of the incoming Pod during create and update admission. If Kubernetes has already populated `status.qosClass`, Capsule can use that value; otherwise it computes the QoS class from the Pod specification.

Deny `BestEffort` Pods:

```yaml
---
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: solar
spec:
  ...
  rules:
    - enforce:
        action: deny
        workloads:
          qosClasses:
            - BestEffort
```

With this rule, a Pod without CPU or memory requests and limits is denied:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: best-effort
spec:
  containers:
    - name: shell
      image: harbor/platform/debian:latest
      command: ["sleep", "infinity"]
```

Example rejection:

```bash
Error from server (Forbidden): error when creating "pod.yaml": admission webhook "pods.projectcapsule.dev" denied the request: QoS class "BestEffort" at status.qosClass is denied by namespace rule
```

Audit `Burstable` Pods:

```yaml
rules:
  - enforce:
      action: audit
      workloads:
        qosClasses:
          - Burstable
```

A matching Pod is admitted in this audit-only example, but Capsule emits an event and the API server response contains an admission warning. If a QoS allow-list is also configured and the Pod's QoS class is not allowed, the Pod is denied while the audit event is still emitted.

Allow `BestEffort` only for selected namespaces:

```yaml
rules:
  - enforce:
      action: deny
      workloads:
        qosClasses:
          - BestEffort

  - namespaceSelector:
      matchLabels:
        allow-best-effort: "true"
    enforce:
      action: allow
      workloads:
        qosClasses:
          - BestEffort
```

Because later matching allow or deny rules take precedence, namespaces labeled `allow-best-effort=true` can run `BestEffort` Pods, while other namespaces cannot.

### Scheduler Names

Scheduler enforcement allows administrators to allow, deny, or audit Pods based on `spec.schedulerName`.

Scheduler rules are configured under `enforce.workloads.schedulers`. Each scheduler matcher uses the common match expression structure with `exact`, `exp`, and optional `negate`.

Capsule evaluates `spec.schedulerName` during Pod create and update admission. If `spec.schedulerName` is empty or omitted, scheduler enforcement does not match it and does not normalize it to `default-scheduler`.

Allow only selected explicit schedulers:

```yaml
---
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: solar
spec:
  ...
  rules:
    - enforce:
        action: allow
        workloads:
          schedulers:
            - exact:
                - tenant-scheduler
                - batch-scheduler
```

A Pod using one of the listed schedulers is admitted:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: scheduled-by-tenant
spec:
  schedulerName: tenant-scheduler
  containers:
    - name: shell
      image: harbor/platform/debian:latest
      command: ["sleep", "infinity"]
```

A Pod using another explicit scheduler is denied:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: scheduled-by-other
spec:
  schedulerName: other-scheduler
  containers:
    - name: shell
      image: harbor/platform/debian:latest
      command: ["sleep", "infinity"]
```

Example rejection:

```bash
Error from server (Forbidden): error when creating "pod.yaml": admission webhook "pods.projectcapsule.dev" denied the request: scheduler "other-scheduler" at spec.schedulerName is not allowed by namespace rule
```

Use a regular expression to allow a scheduler family:

```yaml
rules:
  - enforce:
      action: allow
      workloads:
        schedulers:
          - exp: "tenant-[a-z0-9-]+"
```

Use `exact` and `exp` together to allow a fixed list plus a pattern:

```yaml
rules:
  - enforce:
      action: allow
      workloads:
        schedulers:
          - exact:
              - default-scheduler
              - batch-scheduler
            exp: "tenant-[a-z0-9-]+"
```

This matcher allows `default-scheduler`, `batch-scheduler`, and scheduler names matching `tenant-[a-z0-9-]+`.

Deny a known unsafe scheduler:

```yaml
rules:
  - enforce:
      action: deny
      workloads:
        schedulers:
          - exact:
              - unsafe-scheduler
```

Use `negate: true` to deny every explicit scheduler except a trusted set:

```yaml
rules:
  - enforce:
      action: deny
      workloads:
        schedulers:
          - exact:
              - default-scheduler
              - tenant-scheduler
            negate: true
```

Because `negate` applies to `exact`, this rule matches any explicit scheduler name except `default-scheduler` and `tenant-scheduler`.

Audit usage of a custom scheduler:

```yaml
rules:
  - enforce:
      action: audit
      workloads:
        schedulers:
          - exact:
              - custom-scheduler
```

A matching Pod is admitted in this audit-only example, but Capsule emits an audit event and returns an admission warning. If a scheduler allow-list is also configured and the scheduler name is not allowed, the Pod is denied while the audit event is still emitted.

### OCI Registries

Registry enforcement allows administrators to allow, deny, or audit Pod image references. Registry matchers are evaluated against the full OCI reference string, including registry, repository path, image name, tag, or digest.

Registry rules are configured under `enforce.workloads.registries`. The workload-level `targets` field under `enforce.workloads.targets` controls which Pod image references are validated.

Registry matchers use the common match expression structure:

```yaml
registries:
  - exact:
      - harbor/platform/debian:latest
      - harbor/platform/busybox:latest
  - exp: "harbor/platform/.*"
```

Use `exact` for a fixed list of complete references and `exp` for path or registry patterns. A single matcher may contain both fields:

```yaml
registries:
  - exact:
      - harbor/platform/debian:latest
    exp: "harbor/shared/.*"
```

This matcher succeeds for `harbor/platform/debian:latest` or any reference matching `harbor/shared/.*`.

The following example allows Harbor images by default, denies a more specific customer path for regular containers and image volumes, allows and audits regular container images from an audit registry, and allows a production image path only for namespaces matching `env=prod`:

```yaml
---
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: solar
spec:
  ...
  rules:
    - enforce:
        action: allow
        workloads:
          registries:
            - exp: "harbor/.*"

    - enforce:
        action: deny
        workloads:
          targets:
            - pod/containers
            - pod/volumes
          registries:
            - exp: "harbor/customer/.*"

    - enforce:
        action: allow
        workloads:
          targets:
            - pod/containers
          registries:
            - exp: "audit/.*"

    - enforce:
        action: audit
        workloads:
          targets:
            - pod/containers
          registries:
            - exp: "audit/.*"

    - namespaceSelector:
        matchExpressions:
          - key: env
            operator: In
            values: ["prod"]
      enforce:
        action: allow
        workloads:
          targets:
            - pod/containers
            - pod/volumes
          registries:
            - exp: "harbor/customer/prod-image/.*"
              policy: ["Always"]
```

Apply the following Pod in namespace `solar-test`, which does not match the `env=prod` selector:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: image-volume
spec:
  containers:
    - name: shell
      command: ["sleep", "infinity"]
      imagePullPolicy: IfNotPresent
      image: harbor/customer/test-image/debian:latest
      volumeMounts:
        - name: volume
          mountPath: /volume
  volumes:
    - name: volume
      image:
        reference: quay.io/crio/artifact:v2
        pullPolicy: IfNotPresent
```

The request is denied:

```bash
kubectl apply -f pod.yaml -n solar-test

Error from server (Forbidden): error when creating "pod.yaml": admission webhook "pods.projectcapsule.dev" denied the request: containers[0] reference "harbor/customer/test-image/debian:latest" is denied by registry rule "harbor/customer/.*"
```

The Pod is denied because the regular container image matches both `harbor/.*` and `harbor/customer/.*`. Since the deny rule is declared later, it has higher precedence.

The image volume reference is not denied by the shown deny rule because it does not match `harbor/customer/.*`. If the image volume used a matching reference, for example `harbor/customer/volume-artifact:v1`, the same deny rule would apply because it targets both `pod/containers` and `pod/volumes`.

In a namespace matching `env=prod`, the more specific production allow rule is also considered:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: prod-image
spec:
  containers:
    - name: shell
      command: ["sleep", "infinity"]
      imagePullPolicy: Always
      image: harbor/customer/prod-image/debian:latest
```

The request is allowed because the namespace-specific rule matches later and allows `harbor/customer/prod-image/.*` with `imagePullPolicy: Always`.

Target-specific registry rules allow different behavior for different parts of the same Pod. For example, this rule denies the registry only for init containers:

```yaml
rules:
  - enforce:
      action: deny
      workloads:
        targets:
          - pod/initcontainers
        registries:
          - exp: "harbor/init-only/.*"
```

A matching reference under `spec.initContainers` is denied. The same reference under `spec.containers` is ignored by this rule.

#### Registry exact match examples

Use `exact` when you want to allow or deny a fixed set of complete image references:

```yaml
rules:
  - enforce:
      action: allow
      workloads:
        targets:
          - pod/containers
        registries:
          - exact:
              - harbor/platform/debian:latest
              - harbor/platform/busybox:1.36
```

A Pod using `harbor/platform/debian:latest` or `harbor/platform/busybox:1.36` is admitted. A Pod using `harbor/platform/nginx:latest` is denied because an allow rule exists for registry enforcement but does not match that reference.

You can combine `exact` and `exp` in the same registry matcher:

```yaml
rules:
  - enforce:
      action: allow
      workloads:
        registries:
          - exact:
              - harbor/platform/debian:latest
            exp: "harbor/shared/.*"
```

This rule allows the exact Debian image and any image under `harbor/shared/*`.

#### PullPolicy

Define the allowed image pull policies for a matching registry rule. Supported policies are:

* `Always`: The image is always pulled.
* `IfNotPresent`: The image is pulled only if it is not already present on the node.
* `Never`: The image is never pulled. If the image is not present on the node, the Pod fails to start.

The `policy` field is optional. If no policy is specified, all image pull policies are accepted for the matching registry rule.

```yaml
---
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: solar
spec:
  ...
  rules:
    - enforce:
        action: allow
        workloads:
          targets:
            - pod/containers
          registries:
            - exp: "harbor/v2/customer-registry/.*"
              policy: ["IfNotPresent", "Always"]
```

If the final matching registry decision is `allow` and that matching registry rule defines `policy`, the Pod must use one of the configured pull policies. For example, this rule allows the registry but only with `Always`:

```yaml
rules:
  - enforce:
      action: allow
      workloads:
        targets:
          - pod/containers
        registries:
          - exp: "harbor/v2/customer-registry/.*"
            policy: ["Always"]
```

A Pod using `imagePullPolicy: Never` for that registry is rejected:

```bash
Error from server (Forbidden): error when creating "pod.yaml": admission webhook "pods.projectcapsule.dev" denied the request: containers[0] reference "harbor/v2/customer-registry/debian:latest" uses pullPolicy=Never which is not allowed (allowed: Always)
```

Policy is checked only after the final registry decision is `allow`. A final `deny` decision always denies the request, regardless of the configured pull policy.

#### Negation

A registry matcher can be negated with `negate: true`. Negation applies to the final result of the matcher, including both `exact` and `exp`.

For example, the following rule denies every regular container image that is not from the trusted registry path:

```yaml
---
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: solar
spec:
  ...
  rules:
    - enforce:
        action: deny
        workloads:
          targets:
            - pod/containers
          registries:
            - exp: "trusted/.*"
              negate: true
```

With this rule:

* `trusted/backend/api:1.0.0` is allowed in this deny-only example because it does not match the negated deny rule and no registry allow-list is configured.
* `docker.io/library/nginx:latest` is denied because it does not match `trusted/.*`, so the negated matcher evaluates to true.

Negation also applies to exact values:

```yaml
rules:
  - enforce:
      action: deny
      workloads:
        targets:
          - pod/containers
        registries:
          - exact:
              - trusted/backend/api:1.0.0
              - trusted/frontend/web:1.0.0
            negate: true
```

This rule denies every explicit container image except the two exact references listed, as long as no separate registry allow-list requires an explicit allow. If an allow rule is configured for the same matcher scope, the excepted references must also match an allow rule.

You can combine exact values, regular expressions, negation, namespace selectors, and action precedence. For example, deny all untrusted container images by default, but allow a controlled exception in production namespaces:

```yaml
rules:
  - enforce:
      action: deny
      workloads:
        targets:
          - pod/containers
        registries:
          - exact:
              - trusted/base/debian:latest
            exp: "trusted/platform/.*"
            negate: true

  - enforce:
      action: allow
      workloads:
        targets:
          - pod/containers
        registries:
          - exact:
              - trusted/base/debian:latest
            exp: "trusted/platform/.*"

  - namespaceSelector:
      matchLabels:
        env: prod
    enforce:
      action: allow
      workloads:
        targets:
          - pod/containers
        registries:
          - exp: "partner-registry/prod-approved/.*"
```

The second rule explicitly allows the trusted references that were excluded from the negated deny rule, which is required when registry allow-list behavior is active. In a namespace labeled `env=prod`, `partner-registry/prod-approved/app:1.0.0` is allowed because the later matching allow rule overrides the earlier negated deny rule.



### Targets

The `targets` field defines which parts of a workload a rule applies to.

Targets are configured under `enforce.workloads.targets` and are authoritative for target-aware workload enforcement. Registry entries do not define their own validation targets.

```yaml
rules:
  - enforce:
      action: deny
      workloads:
        targets:
          - pod/containers
        registries:
          - exp: "harbor/customer/.*"
```

If `targets` is omitted or empty, the rule applies to all workload targets supported by the matching hook.

Supported workload targets are:

| Target | Description |
|---|---|
| `pod/initcontainers` | Applies to images used by `spec.initContainers`. |
| `pod/containers` | Applies to images used by `spec.containers`. |
| `pod/ephemeralcontainers` | Applies to images used by `spec.ephemeralContainers`. |
| `pod/volumes` | Applies to image volumes under `spec.volumes[].image`. |

Targets are currently used only by a subset of workload hooks. For example, the registry enforcement hook uses targets to decide which Pod image references are validated. Other hooks may ignore `targets` until they explicitly support target-aware enforcement.

Examples:

```yaml
rules:
  - enforce:
      action: deny
      workloads:
        targets:
          - pod/initcontainers
        registries:
          - exp: "harbor/init-only/.*"
```

This rule denies matching images only when they are used by `initContainers`. The same image reference is not denied when used by regular containers, ephemeral containers, or image volumes unless another rule matches those targets.

```yaml
rules:
  - enforce:
      action: deny
      workloads:
        targets:
          - pod/containers
          - pod/ephemeralcontainers
        registries:
          - exp: "debug/.*"
```

This rule applies to regular containers and ephemeral containers, but not to init containers or image volume

## Services

Service enforcement allows administrators to allow, deny, or audit Kubernetes `Service` resources in Tenant namespaces.

Service rules are configured under `spec.rules[].enforce.services`. Each rule can define an `action`, a list of allowed or denied Service `types`, and optional type-specific constraints for `LoadBalancer`, `ExternalName`, and `NodePort` Services.

```yaml
rules:
  - enforce:
      action: allow
      services:
        types:
          - ClusterIP
          - NodePort
          - LoadBalancer
          - ExternalName
        loadBalancers:
          cidrs:
            - 10.0.0.2/32
        externalNames:
          hostnames:
            - exp: ".*\\.example\\.com"
              exact:
                - internal.git.com
        nodePorts:
          ports:
            - from: 30000
              to: 32767
```

Service enforcement follows the same action and precedence model as other namespace rules:

* `allow` creates an allow-list for the evaluated Service value.
* `deny` denies matching values.
* `audit` emits events and admission warnings but does not allow or deny the request.
* If multiple `allow` or `deny` rules match the same value, the last matching allow or deny rule wins.
* If at least one `allow` rule exists for a Service matcher and no allow or deny rule matches the evaluated value, Capsule denies the request.
* Audit rules never satisfy allow-list behavior.

Service rules are evaluated during Service create and update admission.

### Service Types

The `services.types` field controls which Kubernetes Service types are allowed, denied, or audited by a rule.

Supported values are:

| Type           | Description                                                |
| -------------- | ---------------------------------------------------------- |
| `ClusterIP`    | Allows, denies, or audits Services of type `ClusterIP`.    |
| `NodePort`     | Allows, denies, or audits Services of type `NodePort`.     |
| `LoadBalancer` | Allows, denies, or audits Services of type `LoadBalancer`. |
| `ExternalName` | Allows, denies, or audits Services of type `ExternalName`. |

Allow only `ClusterIP` Services:

```yaml
rules:
  - enforce:
      action: allow
      services:
        types:
          - ClusterIP
```

With this rule, a `ClusterIP` Service is admitted:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: internal-api
spec:
  type: ClusterIP
  ports:
    - name: http
      port: 8080
      targetPort: 8080
```

A Service of another type, for example `ExternalName`, is denied because an allow-list exists for Service types and `ExternalName` is not listed:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: external-api
spec:
  type: ExternalName
  externalName: internal.git.com
  ports:
    - name: http
      port: 443
      targetPort: 443
```

Example rejection:

```bash
Error from server (Forbidden): error when creating "svc.yaml": admission webhook "services.validating.projectcapsule.dev" denied the request: service type "ExternalName" at spec.type is not allowed by namespace rule: value did not match any allowed rule. Allowed service types: ClusterIP
```

Deny `LoadBalancer` Services:

```yaml
rules:
  - enforce:
      action: deny
      services:
        types:
          - LoadBalancer
```

Allow `ClusterIP` and `ExternalName`, but deny `ExternalName` again for selected namespaces:

```yaml
rules:
  - enforce:
      action: allow
      services:
        types:
          - ClusterIP
          - ExternalName

  - namespaceSelector:
      matchLabels:
        external-services: blocked
    enforce:
      action: deny
      services:
        types:
          - ExternalName
```

Because later matching allow or deny decisions win, namespaces labeled `external-services=blocked` cannot create `ExternalName` Services, while other matching namespaces can.

#### Important caveats for `services.types`

The `services.types` field is the Service capability gate. Type-specific sections such as `loadBalancers`, `externalNames`, and `nodePorts` do not automatically allow a Service type by themselves.

For example, this rule restricts LoadBalancer CIDRs, but it does not by itself allow `LoadBalancer` Services if another type allow-list exists that excludes `LoadBalancer`:

```yaml
rules:
  - enforce:
      action: allow
      services:
        types:
          - ClusterIP

  - enforce:
      action: allow
      services:
        loadBalancers:
          cidrs:
            - 10.0.0.2/32
```

In this example, a `LoadBalancer` Service is denied by the Service type allow-list because `LoadBalancer` is not included in `services.types`.

To allow and constrain `LoadBalancer` Services, configure both:

```yaml
rules:
  - enforce:
      action: allow
      services:
        types:
          - LoadBalancer
        loadBalancers:
          cidrs:
            - 10.0.0.2/32
```

### LoadBalancer

LoadBalancer rules allow administrators to restrict the IPs and source ranges used by Services of type `LoadBalancer`.

LoadBalancer constraints are configured under `enforce.services.loadBalancers.cidrs`.

Capsule evaluates the following Service fields:

| Field                             | Description                                            |
| --------------------------------- | ------------------------------------------------------ |
| `spec.loadBalancerIP`             | Explicit LoadBalancer IP requested by the Service.     |
| `spec.loadBalancerSourceRanges[]` | Source CIDR ranges allowed to access the LoadBalancer. |

Allow LoadBalancer Services only with a specific IP:

```yaml
rules:
  - enforce:
      action: allow
      services:
        types:
          - LoadBalancer
        loadBalancers:
          cidrs:
            - 10.0.0.2/32
```

This Service is admitted:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: public-api
spec:
  type: LoadBalancer
  loadBalancerIP: 10.0.0.2
  ports:
    - name: http
      port: 80
      targetPort: 8080
```

This Service is denied because the requested IP is outside the allowed CIDR:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: public-api
spec:
  type: LoadBalancer
  loadBalancerIP: 10.0.171.239
  ports:
    - name: http
      port: 80
      targetPort: 8080
```

Example rejection:

```bash
Error from server (Forbidden): error when creating "svc.yaml": admission webhook "services.validating.projectcapsule.dev" denied the request: loadBalancer CIDR "10.0.171.239" at spec.loadBalancerIP is not allowed by namespace rule: value did not match any allowed rule. Allowed CIDRs: 10.0.0.2/32
```

Allow a LoadBalancer IP range:

```yaml
rules:
  - enforce:
      action: allow
      services:
        types:
          - LoadBalancer
        loadBalancers:
          cidrs:
            - 10.0.1.0/24
```

The following Service is admitted because `10.0.1.44` is contained in `10.0.1.0/24`:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: public-api
spec:
  type: LoadBalancer
  loadBalancerIP: 10.0.1.44
  ports:
    - name: http
      port: 80
      targetPort: 8080
```

Restrict `loadBalancerSourceRanges`:

```yaml
rules:
  - enforce:
      action: allow
      services:
        types:
          - LoadBalancer
        loadBalancers:
          cidrs:
            - 10.0.1.0/24
```

This Service is admitted because the requested source range is fully contained in the allowed CIDR:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: public-api
spec:
  type: LoadBalancer
  loadBalancerSourceRanges:
    - 10.0.1.0/25
  ports:
    - name: http
      port: 80
      targetPort: 8080
```

This Service is denied because the requested source range is not fully contained in the allowed CIDR:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: public-api
spec:
  type: LoadBalancer
  loadBalancerSourceRanges:
    - 10.0.1.0/23
  ports:
    - name: http
      port: 80
      targetPort: 8080
```

#### Required LoadBalancer fields when CIDRs are configured

If any matching rule configures `loadBalancers.cidrs`, then a `LoadBalancer` Service must explicitly set at least one of:

* `spec.loadBalancerIP`
* `spec.loadBalancerSourceRanges`

This is intentional. If CIDR restrictions are configured, Capsule requires the Service request to provide a value that can be evaluated.

For example, this Service is denied when `loadBalancers.cidrs` is configured:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: public-api
spec:
  type: LoadBalancer
  ports:
    - name: http
      port: 80
      targetPort: 8080
```

Example rejection:

```bash
Error from server (Forbidden): error when creating "svc.yaml": admission webhook "services.validating.projectcapsule.dev" denied the request: loadBalancer service requires spec.loadBalancerIP or spec.loadBalancerSourceRanges because loadBalancer CIDR constraints are enforced by namespace rule
```

If no `loadBalancers.cidrs` constraint is configured, Capsule does not require these fields. In that case, a `LoadBalancer` Service can be admitted as long as the Service type itself is allowed.

#### Denying selected LoadBalancer CIDRs

You can also deny specific LoadBalancer CIDRs:

```yaml
rules:
  - enforce:
      action: allow
      services:
        types:
          - LoadBalancer
        loadBalancers:
          cidrs:
            - 10.0.0.0/8

  - enforce:
      action: deny
      services:
        loadBalancers:
          cidrs:
            - 10.0.66.0/24
```

A Service using `10.0.66.10` is denied because the later deny rule matches:

```bash
Error from server (Forbidden): error when creating "svc.yaml": admission webhook "services.validating.projectcapsule.dev" denied the request: loadBalancer CIDR "10.0.66.10" at spec.loadBalancerIP is denied by namespace rule: 10.0.66.10 is contained in 10.0.66.0/24
```

A later namespace-specific allow rule can override an earlier allow miss or deny decision:

```yaml
rules:
  - enforce:
      action: allow
      services:
        types:
          - LoadBalancer
        loadBalancers:
          cidrs:
            - 10.0.0.2/32

  - namespaceSelector:
      matchLabels:
        environment: prod
    enforce:
      action: allow
      services:
        loadBalancers:
          cidrs:
            - 10.0.171.0/24
```

In namespaces labeled `environment=prod`, a Service using `10.0.171.239` is admitted. In other namespaces, it is denied because it does not match the default allowed CIDR.

### ExternalName

ExternalName rules allow administrators to restrict `spec.externalName` for Services of type `ExternalName`.

ExternalName constraints are configured under `enforce.services.externalNames.hostnames`.

Each hostname matcher uses the common match expression structure with `exact`, `exp`, and optional `negate`.

Allow selected ExternalName hostnames:

```yaml
rules:
  - enforce:
      action: allow
      services:
        types:
          - ExternalName
        externalNames:
          hostnames:
            - exact:
                - internal.git.com
            - exp: ".*\\.example\\.com"
```

The following Services are admitted:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: git
spec:
  type: ExternalName
  externalName: internal.git.com
  ports:
    - name: https
      port: 443
      targetPort: 443
```

```yaml
apiVersion: v1
kind: Service
metadata:
  name: api
spec:
  type: ExternalName
  externalName: api.example.com
  ports:
    - name: https
      port: 443
      targetPort: 443
```

A non-matching hostname is denied:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: api
spec:
  type: ExternalName
  externalName: api.bad.com
  ports:
    - name: https
      port: 443
      targetPort: 443
```

Example rejection:

```bash
Error from server (Forbidden): error when creating "svc.yaml": admission webhook "services.validating.projectcapsule.dev" denied the request: externalName hostname "api.bad.com" at spec.externalName is not allowed by namespace rule: value did not match any allowed rule. Allowed hostnames: exact: internal.git.com, exp: .*\.example\.com
```

Use `exact` and `exp` together in the same matcher:

```yaml
rules:
  - enforce:
      action: allow
      services:
        types:
          - ExternalName
        externalNames:
          hostnames:
            - exact:
                - combined.internal.git.com
              exp: "combined\\..*\\.example\\.com"
```

This matcher allows both:

* `combined.internal.git.com`
* hostnames matching `combined\\..*\\.example\\.com`

#### Negation for ExternalName hostnames

`negate: true` inverts the final matcher result. This applies to both `exact` and `exp`.

Deny every ExternalName except trusted hostnames:

```yaml
rules:
  - enforce:
      action: deny
      services:
        externalNames:
          hostnames:
            - exp: "trusted\\..*"
              negate: true

  - enforce:
      action: allow
      services:
        types:
          - ExternalName
        externalNames:
          hostnames:
            - exp: "trusted\\..*"
```

With these rules:

* `trusted.api` is admitted.
* `api.example.com` is denied by the negated deny rule.

Example rejection:

```bash
Error from server (Forbidden): error when creating "svc.yaml": admission webhook "services.validating.projectcapsule.dev" denied the request: externalName hostname "api.example.com" at spec.externalName is denied by namespace rule: "api.example.com" matched hostname rule not exp: trusted\..*
```

Important: when an allow-list exists for ExternalName hostnames, values excluded from a negated deny rule still need a matching allow rule. The deny rule prevents untrusted values, while the allow rule satisfies allow-list behavior for trusted values.

#### Namespace-specific ExternalName rules

You can use `namespaceSelector` to apply ExternalName restrictions only to selected namespaces:

```yaml
rules:
  - enforce:
      action: allow
      services:
        types:
          - ExternalName
        externalNames:
          hostnames:
            - exp: ".*\\.example\\.com"

  - namespaceSelector:
      matchLabels:
        external-policy: restricted
    enforce:
      action: deny
      services:
        externalNames:
          hostnames:
            - exact:
                - blocked.example.com
```

In namespaces labeled `external-policy=restricted`, `blocked.example.com` is denied. Other hostnames matching `.*\\.example\\.com` remain allowed.

### NodePort

NodePort rules allow administrators to restrict explicitly requested `spec.ports[].nodePort` values.

NodePort constraints are configured under `enforce.services.nodePorts.ports`.

Each port range contains:

| Field  | Description                                |
| ------ | ------------------------------------------ |
| `from` | First allowed or denied port in the range. |
| `to`   | Last allowed or denied port in the range.  |

The `from` value must be lower than or equal to `to`. Equal values are valid and represent a single port.

Allow selected NodePort ranges:

```yaml
rules:
  - enforce:
      action: allow
      services:
        types:
          - NodePort
        nodePorts:
          ports:
            - from: 30000
              to: 30100
            - from: 30500
              to: 30500
```

This Service is admitted because `30080` is in the allowed range:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: tenant-api
spec:
  type: NodePort
  ports:
    - name: http
      port: 8080
      targetPort: 8080
      nodePort: 30080
```

This Service is also admitted because `30500` matches the single-port range:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: tenant-api-single
spec:
  type: NodePort
  ports:
    - name: http
      port: 8080
      targetPort: 8080
      nodePort: 30500
```

This Service is denied because `32080` is outside the allowed ranges:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: tenant-api
spec:
  type: NodePort
  ports:
    - name: http
      port: 8080
      targetPort: 8080
      nodePort: 32080
```

Example rejection:

```bash
Error from server (Forbidden): error when creating "svc.yaml": admission webhook "services.validating.projectcapsule.dev" denied the request: nodePort "32080" at spec.ports[0].nodePort is not allowed by namespace rule: value did not match any allowed rule. Allowed ranges: 30000-30100, 30500
```

#### Required explicit nodePort when ranges are configured

If any matching rule configures `nodePorts.ports`, then a `NodePort` Service must explicitly set `spec.ports[].nodePort`.

This is intentional. Kubernetes can allocate a node port automatically when the field is omitted, but the validating webhook cannot know the allocated value at admission time. To enforce configured port ranges reliably, Capsule requires the requested node port to be explicit.

The following Service is denied when `nodePorts.ports` is configured:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: tenant-api
spec:
  type: NodePort
  ports:
    - name: http
      port: 8080
      targetPort: 8080
```

Example rejection:

```bash
Error from server (Forbidden): error when creating "svc.yaml": admission webhook "services.validating.projectcapsule.dev" denied the request: service requires explicit spec.ports[*].nodePort because nodePort ranges are enforced by namespace rule
```

If no `nodePorts.ports` constraint is configured, Capsule does not require explicit `nodePort` values. In that case, a `NodePort` Service can be admitted as long as the Service type itself is allowed.

#### Denying selected NodePorts

You can allow a broad range and deny a specific port afterwards:

```yaml
rules:
  - enforce:
      action: allow
      services:
        types:
          - NodePort
        nodePorts:
          ports:
            - from: 30000
              to: 30100

  - enforce:
      action: deny
      services:
        nodePorts:
          ports:
            - from: 30090
              to: 30090
```

A Service using `30080` is admitted. A Service using `30090` is denied because the later deny rule also matches.

Example rejection:

```bash
Error from server (Forbidden): error when creating "svc.yaml": admission webhook "services.validating.projectcapsule.dev" denied the request: nodePort "30090" at spec.ports[0].nodePort is denied by namespace rule: nodePort 30090 is within allowed range 30090
```

Although the detail says the port is within the matched range, the rule action is `deny`, so the request is rejected.

#### LoadBalancer Services and NodePorts

Kubernetes `LoadBalancer` Services may allocate node ports unless `spec.allocateLoadBalancerNodePorts` is explicitly set to `false`.

Therefore, NodePort range enforcement also applies to `LoadBalancer` Services when node port allocation is enabled.

This rule allows LoadBalancer Services, restricts the LoadBalancer IP, and restricts the allocated node port:

```yaml
rules:
  - enforce:
      action: allow
      services:
        types:
          - LoadBalancer
        loadBalancers:
          cidrs:
            - 10.0.0.2/32
        nodePorts:
          ports:
            - from: 30000
              to: 30100
```

This Service is admitted because the LoadBalancer IP and node port are both allowed:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: public-api
spec:
  type: LoadBalancer
  loadBalancerIP: 10.0.0.2
  ports:
    - name: http
      port: 80
      targetPort: 8080
      nodePort: 30080
```

This Service is denied because the explicit node port is outside the allowed range:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: public-api
spec:
  type: LoadBalancer
  loadBalancerIP: 10.0.0.2
  ports:
    - name: http
      port: 80
      targetPort: 8080
      nodePort: 32080
```

When `nodePorts.ports` is configured and LoadBalancer node port allocation is enabled, Capsule requires explicit `spec.ports[].nodePort` values:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: public-api
spec:
  type: LoadBalancer
  loadBalancerIP: 10.0.0.2
  ports:
    - name: http
      port: 80
      targetPort: 8080
```

Example rejection:

```bash
Error from server (Forbidden): error when creating "svc.yaml": admission webhook "services.validating.projectcapsule.dev" denied the request: service requires explicit spec.ports[*].nodePort because nodePort ranges are enforced by namespace rule
```

To avoid node port enforcement for a LoadBalancer Service, disable node port allocation explicitly:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: public-api
spec:
  type: LoadBalancer
  allocateLoadBalancerNodePorts: false
  loadBalancerIP: 10.0.0.2
  ports:
    - name: http
      port: 80
      targetPort: 8080
```

With `allocateLoadBalancerNodePorts: false`, Capsule does not require or validate `spec.ports[].nodePort` for that LoadBalancer Service. The Service must still satisfy any configured LoadBalancer CIDR rules.

### Advanced

#### Auditing Services

Use `action: audit` to observe Service usage without directly blocking the request. Audit rules emit Kubernetes events and return admission warnings, but they do not allow or deny the request.

Audit ExternalName usage:

```yaml
rules:
  - enforce:
      action: audit
      services:
        types:
          - ExternalName
        externalNames:
          hostnames:
            - exp: "audit\\..*"
```

A matching Service is admitted in this audit-only example because no Service type or hostname allow-list is configured:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: audited-external
spec:
  type: ExternalName
  externalName: audit.internal
  ports:
    - name: https
      port: 443
      targetPort: 443
```

If an allow-list is also configured, audit does not satisfy it:

```yaml
rules:
  - enforce:
      action: audit
      services:
        externalNames:
          hostnames:
            - exp: "audit\\..*"

  - enforce:
      action: allow
      services:
        types:
          - ExternalName
        externalNames:
          hostnames:
            - exp: "allowed\\..*"
```

With these rules, `audit.internal` emits an audit event but is still denied because it does not match the allowed hostname rule.

#### Combining Service Rules

Service rules can be split across multiple rule blocks. This is useful when type permissions, LoadBalancer CIDR rules, hostname rules, and NodePort ranges should be managed independently.

For example:

```yaml
rules:
  - enforce:
      action: allow
      services:
        types:
          - ClusterIP
          - ExternalName

  - enforce:
      action: allow
      services:
        externalNames:
          hostnames:
            - exp: ".*\\.example\\.com"
```

This configuration:

* allows `ClusterIP` Services;
* allows `ExternalName` Services as a type;
* allows only ExternalName hostnames matching `.*\\.example\\.com`.

A Service of type `ExternalName` with `externalName: api.example.com` is admitted. A Service of type `ExternalName` with `externalName: api.bad.com` is denied by the hostname allow-list.

A later deny rule can override an earlier allow rule:

```yaml
rules:
  - enforce:
      action: allow
      services:
        types:
          - ExternalName
        externalNames:
          hostnames:
            - exp: ".*\\.example\\.com"

  - enforce:
      action: deny
      services:
        externalNames:
          hostnames:
            - exact:
                - blocked.example.com
```

Here, `api.example.com` is allowed, but `blocked.example.com` is denied because the later deny rule matches.

A later allow rule can override an earlier deny rule:

```yaml
rules:
  - enforce:
      action: deny
      services:
        nodePorts:
          ports:
            - from: 30080
              to: 30080

  - namespaceSelector:
      matchLabels:
        allow-special-nodeport: "true"
    enforce:
      action: allow
      services:
        types:
          - NodePort
        nodePorts:
          ports:
            - from: 30080
              to: 30080
```

In namespaces labeled `allow-special-nodeport=true`, a `NodePort` Service using `30080` is admitted because the namespace-specific allow rule matches later.

#### Service Rule Caveats

Service enforcement is intentionally explicit. Keep the following behavior in mind:

| Behavior                                                      | Explanation                                                                                                                                                      |
| ------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `services.types` is the type gate                             | Type-specific sections do not automatically grant the Service type. Include the Service type in `services.types` when an allow-list for Service types is active. |
| Type-specific constraints create allow-lists for their values | If `loadBalancers.cidrs`, `externalNames.hostnames`, or `nodePorts.ports` is configured with `action: allow`, non-matching values are denied.                    |
| `loadBalancers.cidrs` requires explicit values                | When CIDR constraints are configured, `LoadBalancer` Services must set `spec.loadBalancerIP` or `spec.loadBalancerSourceRanges`.                                 |
| `nodePorts.ports` requires explicit node ports                | When port constraints are configured, `NodePort` Services and LoadBalancer Services with node port allocation enabled must set `spec.ports[].nodePort`.          |
| LoadBalancer node port allocation matters                     | `LoadBalancer` Services are subject to NodePort range checks unless `spec.allocateLoadBalancerNodePorts: false` is set.                                          |
| Audit does not allow                                          | A matching `audit` rule emits events and warnings but does not satisfy an allow-list.                                                                            |
| Last matching allow or deny wins                              | Later matching `allow` or `deny` rules override earlier matching allow or deny rules.                                                                            |
| Negation applies to the whole matcher                         | `negate: true` inverts the result of both `exact` and `exp`.                                                                                                     |
| Namespace selectors affect projected rules                    | Rules with `namespaceSelector` only apply to namespaces matching the selector.                                                                                   |

#### Complete Service Enforcement Example

The following example combines type enforcement, LoadBalancer CIDR restrictions, ExternalName hostname restrictions, NodePort range restrictions, audit rules, and namespace-specific exceptions:

```yaml
---
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: solar
spec:
  ...
  rules:
    - enforce:
        action: allow
        services:
          types:
            - ClusterIP
            - NodePort
            - LoadBalancer
            - ExternalName

    - enforce:
        action: allow
        services:
          loadBalancers:
            cidrs:
              - 10.0.0.2/32
              - 10.0.1.0/24

    - enforce:
        action: allow
        services:
          externalNames:
            hostnames:
              - exact:
                  - internal.git.com
              - exp: ".*\\.example\\.com"

    - enforce:
        action: allow
        services:
          nodePorts:
            ports:
              - from: 30000
                to: 30100
              - from: 30500
                to: 30500

    - enforce:
        action: deny
        services:
          nodePorts:
            ports:
              - from: 30090
                to: 30090

    - enforce:
        action: deny
        services:
          loadBalancers:
            cidrs:
              - 10.0.66.0/24

    - enforce:
        action: audit
        services:
          externalNames:
            hostnames:
              - exp: "audit\\..*"

    - namespaceSelector:
        matchLabels:
          environment: prod
      enforce:
        action: allow
        services:
          loadBalancers:
            cidrs:
              - 10.0.171.0/24
```

With this configuration:

* `ClusterIP`, `NodePort`, `LoadBalancer`, and `ExternalName` Services are valid Service types.
* LoadBalancer IPs must be contained in `10.0.0.2/32` or `10.0.1.0/24`.
* Namespaces labeled `environment=prod` can also use LoadBalancer IPs in `10.0.171.0/24`.
* ExternalName hostnames must be `internal.git.com` or match `.*\\.example\\.com`.
* Explicit node ports must be in `30000-30100` or equal to `30500`.
* Node port `30090` is denied even though it is inside the broader allowed range.
* ExternalName hostnames matching `audit\\..*` emit audit events and warnings.
* Audit matches do not allow values that fail the allow-list.

## Ingress

Ingress enforcement allows administrators to allow, deny, or audit hostnames on
Kubernetes Ingresses, OpenShift Routes, and Gateway API resources in Tenant
namespaces.

Ingress rules are configured under `spec.rules[].enforce.ingress`. Each rule
selects one or more resource `types` and defines hostname match expressions:

```yaml
rules:
  - enforce:
      action: allow
      ingress:
        types:
          - Ingress
          - HTTPRoute
        hostnames:
          - exact:
              - internal.example.com
          - exp: "^[a-z0-9-]+\\.example\\.com$"
```

| Field | Description |
|---|---|
| `types` | Resource kinds to which the rule applies. At least one type is required. |
| `hostnames` | One or more common match expressions using `exact`, `exp`, and optional `negate`. |

Capsule supports the following resource types and hostname fields:

| Type | API | Evaluated fields |
|---|---|---|
| `Ingress` | `networking.k8s.io/v1` | `spec.rules[].host` and `spec.tls[].hosts[]` |
| `Route` | `route.openshift.io/v1` | `spec.host` |
| `Gateway` | `gateway.networking.k8s.io/v1` | `spec.listeners[].hostname` |
| `ListenerSet` | `gateway.networking.k8s.io/v1` | `spec.listeners[].hostname` |
| `HTTPRoute` | `gateway.networking.k8s.io/v1` | `spec.hostnames[]` |
| `TLSRoute` | `gateway.networking.k8s.io/v1` | `spec.hostnames[]` |
| `GRPCRoute` | `gateway.networking.k8s.io/v1` | `spec.hostnames[]` |

Ingress rules are evaluated during create and update admission. A rule only
participates when its `types` list contains the incoming resource kind. Other
resource types are unaffected.

Each hostname on a targeted resource is evaluated independently. The entire
request is denied if any hostname is denied or does not satisfy an active
allow-list. For an `Ingress`, this includes both routing hosts and TLS hosts, so
all values in `spec.rules[].host` and `spec.tls[].hosts[]` must satisfy the
policy.

Ingress hostname enforcement follows the same action and precedence model as
other namespace rules:

* `allow` creates an allow-list for hostnames of the selected resource types.
* `deny` denies matching hostnames.
* `audit` emits Kubernetes events for matching hostnames and missing hostname
  fields but does not allow or deny them.
* If multiple `allow` or `deny` rules match the same hostname, the last matching
  allow or deny rule wins.
* An audit match does not satisfy an allow-list.

### Allow selected hostnames

The following rule allows one exact hostname and any single-label hostname
under `example.com` for Kubernetes Ingress and Gateway API HTTPRoute resources:

```yaml
---
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: solar
spec:
  ...
  rules:
    - enforce:
        action: allow
        ingress:
          types:
            - Ingress
            - HTTPRoute
          hostnames:
            - exact:
                - internal.example.com
            - exp: "^[a-z0-9-]+\\.example\\.com$"
```

This Ingress is admitted because both its routing hostname and TLS hostname
match the allow-list:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: tenant-api
spec:
  rules:
    - host: api.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: tenant-api
                port:
                  number: 8080
  tls:
    - hosts:
        - api.example.com
      secretName: tenant-api-tls
```

Changing either occurrence to `api.example.net` denies the request. A rejection
for the routing hostname includes the object path and configured allow-list:

```text
ingress hostname "api.example.net" at spec.rules[0].host is not allowed by namespace rule: value did not match any allowed rule. Allowed hostnames: exact: internal.example.com, exp: ^[a-z0-9-]+\.example\.com$
```

An `Ingress` TLS entry is not exempt from enforcement. For example, the
following object is denied even though its routing hostname is allowed, because
`legacy.example.net` in `spec.tls[0].hosts[0]` is not allowed:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: mixed-hostnames
spec:
  rules:
    - host: api.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: tenant-api
                port:
                  number: 8080
  tls:
    - hosts:
        - legacy.example.net
      secretName: tenant-api-tls
```

### Gateway API and OpenShift Route examples

A single rule can target several supported resource shapes:

```yaml
rules:
  - enforce:
      action: allow
      ingress:
        types:
          - Route
          - Gateway
          - ListenerSet
          - HTTPRoute
          - TLSRoute
          - GRPCRoute
        hostnames:
          - exp: "^([a-z0-9-]+\\.)*apps\\.example\\.com$"
```

For an `HTTPRoute`, Capsule evaluates every entry in `spec.hostnames`:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: store
spec:
  hostnames:
    - store.apps.example.com
    - checkout.apps.example.com
  rules:
    - backendRefs:
        - name: store
          port: 8080
```

Both values match the expression, so the request is admitted. If one hostname
does not match, Capsule denies the entire `HTTPRoute`.

For a `Gateway` or `ListenerSet`, Capsule evaluates the hostname of every
listener:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: tenant-gateway
spec:
  gatewayClassName: shared
  listeners:
    - name: https
      protocol: HTTPS
      port: 443
      hostname: gateway.apps.example.com
      tls:
        mode: Terminate
        certificateRefs:
          - name: gateway-tls
```

For an OpenShift `Route`, the same rule evaluates `spec.host`:

```yaml
apiVersion: route.openshift.io/v1
kind: Route
metadata:
  name: tenant-api
spec:
  host: api.apps.example.com
  to:
    kind: Service
    name: tenant-api
```

### Deny hostnames and add exceptions

Allow a hostname family, then deny a sensitive hostname with a later rule:

```yaml
rules:
  - enforce:
      action: allow
      ingress:
        types:
          - Ingress
          - HTTPRoute
        hostnames:
          - exp: "^[a-z0-9-]+\\.example\\.com$"

  - enforce:
      action: deny
      ingress:
        types:
          - Ingress
          - HTTPRoute
        hostnames:
          - exact:
              - admin.example.com
```

`api.example.com` is admitted, while `admin.example.com` is denied because the
later matching deny rule wins.

A still later namespace-specific rule can allow that hostname as an exception:

```yaml
  - namespaceSelector:
      matchLabels:
        ingress-admin: "true"
    enforce:
      action: allow
      ingress:
        types:
          - Ingress
          - HTTPRoute
        hostnames:
          - exact:
              - admin.example.com
```

Namespaces labeled `ingress-admin=true` can use `admin.example.com`; the earlier
deny rule still applies in other namespaces.

You can also use negation to deny every hostname outside a trusted suffix:

```yaml
rules:
  - enforce:
      action: deny
      ingress:
        types:
          - Ingress
        hostnames:
          - exp: "^([a-z0-9-]+\\.)*example\\.com$"
            negate: true
```

This deny rule matches hostnames that do not match the expression. Because it
does not create an allow-list, matching `example.com` hostnames pass unless
another rule denies them.

### Audit hostname usage

Use `action: audit` to observe selected hostnames without blocking them:

```yaml
rules:
  - enforce:
      action: audit
      ingress:
        types:
          - Ingress
          - HTTPRoute
        hostnames:
          - exp: "^preview-.*\\.example\\.com$"
```

A matching hostname is admitted in this audit-only example, and Capsule emits a
Kubernetes event for it. If an allow-list is also configured and the hostname
does not match an allow rule, the request is still denied; the audit rule does
not grant access.

### Missing hostnames

As soon as at least one `allow` or `deny` hostname rule targets a resource type,
every expected hostname field on that resource must contain a non-empty value.
Omitted, empty, and whitespace-only values are treated as missing.

Examples of missing values include:

* an `Ingress` with no routing or TLS hostname, an Ingress rule without `host`,
  or a TLS entry without `hosts`;
* a `Route` without `spec.host`;
* a `Gateway` or `ListenerSet` listener without `hostname`;
* an `HTTPRoute`, `TLSRoute`, or `GRPCRoute` without `spec.hostnames`.

For example, this Gateway is denied when an `allow` or `deny` hostname rule
targets `Gateway`:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: hostless
spec:
  gatewayClassName: shared
  listeners:
    - name: http
      protocol: HTTP
      port: 80
```

The rejection identifies the missing field:

```text
hostname is required at spec.listeners[0].hostname because hostname rules target Gateway
```

If no ingress rule targets the resource type, Capsule does not apply this
hostname requirement.

Audit-only rules do not make hostnames mandatory. When an expected hostname is
missing, Capsule admits the request and emits an audit event that identifies the
empty field, for example:

```text
empty hostname detected at spec.listeners[0].hostname for Gateway by audit namespace rule
```

If audit and `allow` or `deny` rules target the same resource type, Capsule emits
the empty-hostname audit event and enforces the non-audit rule, which denies the
request.

## Metadata

Metadata enforcement allows administrators to allow, deny, or audit Kubernetes object labels and annotations for namespaced resources.

Metadata rules are configured under `spec.rules[].enforce.metadata`. They are evaluated by a generic validating webhook and can target one or more Kubernetes kinds. This makes metadata enforcement useful for objects such as `ConfigMap`, `Secret`, `Service`, `Deployment`, custom resources, and other namespaced resources.

```yaml
rules:
  - enforce:
      action: allow
      metadata:
        - apiGroups:
            - "*"
          kinds:
            - ConfigMap
            - Service
          labels:
            corp.com/tenant:
              required: true
              values:
                - exact:
                    - prod
                    - test
          annotations:
            example.corp/cost-center:
              required: false
              values:
                - exp: "^INV-[0-9]{4}$"
                  exact:
                    - prod
                    - test
```

Metadata enforcement follows the same action and precedence model as other namespace rules:

* `allow` creates an allow-list for the evaluated metadata key.
* `deny` denies matching metadata values.
* `audit` emits Kubernetes events and admission warnings but does not allow or deny the request.
* If multiple `allow` or `deny` rules match the same metadata key and value, the last matching allow or deny rule wins.
* If at least one `allow` rule exists for a metadata key and the object contains that key with a value that does not match any allow or deny rule, Capsule denies the request.
* Audit rules never satisfy allow-list behavior.
* Missing optional metadata keys are ignored.

Metadata rules are evaluated during create and update admission. Metadata enforcement is intentionally generic and conservative. Keep the following behavior in mind:

| Behavior | Explanation |
|---|---|
| Namespaced resources and explicitly selected Namespaces are evaluated | Metadata rules normally target resources inside Tenant namespaces. `Namespace` is the only supported cluster-scoped kind, and it must be selected explicitly with `kinds: ["Namespace"]`. |
| Controller-managed objects can be skipped | Objects labeled `managed-by=controller` are ignored by generic metadata validation. This prevents controllers from being blocked when reconciling managed objects. The skip check is exact and case-sensitive. |
| Capsule-managed metadata is ignored | Built-in Capsule labels and annotations are treated as managed metadata and are ignored by metadata validation. Do not rely on metadata rules to validate Capsule-owned keys. |
| Managed annotation prefixes are ignored | Capsule-managed annotation prefixes such as resource quota and resource usage annotations are ignored. |
| Missing optional metadata is ignored | If `required: false`, the key is only evaluated when it is present. |
| `required` applies to allow rules | `required: true` enforces presence for `action: allow`. `deny` and `audit` rules match values; they do not require missing keys to exist. |
| Empty metadata values are valid values | A label or annotation with an empty string value is still present and can be matched with `exact: [""]`. |
| Labels and annotations are independent | A matching annotation does not satisfy a required label with the same key, and a matching label does not satisfy a required annotation. |
| Empty `apiGroups` means core `v1` | Omitted `apiGroups`, an empty list, or an empty entry selects the core Kubernetes `v1` API. Use `apiGroups: ["*"]` to match every API group and version. |
| `kinds` must be set | Use `kinds: ["*"]` to match all namespaced kinds. A wildcard does not implicitly include `Namespace`. |

Capsule-managed labels include labels used to track Tenant ownership, resource pools, freeze and cordon state, promotion state, Capsule ownership, and generated namespace resources. Capsule-managed annotations include release and reconciliation annotations, available class and registry annotations, forbidden namespace metadata annotations, protected Tenant annotations, and resource quota or resource usage annotation prefixes.

Because these keys are owned by Capsule, metadata rules that reference them are ignored by default. Use application-specific labels and annotations for Tenant policy enforcement.


### Target resources

Each metadata rule defines which resource kinds it applies to:

| Field | Description |
|---|---|
| `apiGroups` | List of API group or group/version selectors. Empty or omitted means core `v1`; `apps` matches every version in that group; `apps/v1` matches that exact group/version; and `"*"` matches all groups and versions. |
| `kinds` | List of Kubernetes kind selectors. `"*"` and partial wildcards match namespaced kinds, but `Namespace` must always appear as a separate literal entry to include it. |

Examples:

```yaml
metadata:
  - kinds:
      - ConfigMap
      - Service
```

This targets core `v1` `ConfigMap` and `Service` resources because `apiGroups`
is omitted.

```yaml
metadata:
  - apiGroups:
      - apps/v1
    kinds:
      - Deployment
      - StatefulSet
```

This targets only `apps/v1` `Deployment` and `StatefulSet` resources.

```yaml
metadata:
  - apiGroups:
      - "*"
    kinds:
      - "*"
```

This targets all namespaced resources handled by the generic metadata webhook.
It does **not** target `Namespace`, despite both selectors being wildcards.

Partial wildcards are also supported:

```yaml
metadata:
  - apiGroups:
      - "apps/*"
    kinds:
      - "*Set"
```

This can match resources such as `apps/v1` `ReplicaSet` and `apps/v1` `StatefulSet`.


#### Namespace

`Namespace` is the only cluster-scoped resource supported by metadata rules. It
is deliberately opt-in: the `kinds` list must contain the literal,
case-sensitive value `Namespace`. This prevents a broad rule intended for
resources inside Tenant namespaces from accidentally changing or rejecting the
Namespace object itself.

The Namespace GVK is core `v1`, `Kind=Namespace`. The `apiGroups` selector must
therefore match core `v1`. The clearest form is:

```yaml
metadata:
  - apiGroups:
      - "v1"
    kinds:
      - "Namespace"
```

Because omitted `apiGroups` defaults to core `v1`, this shorter form is
equivalent:

```yaml
metadata:
  - kinds:
      - Namespace
```

An API-group wildcard may also match core `v1`, but it still does not remove the
explicit-kind requirement. For example, this targets all namespaced kinds **and**
Namespace:

```yaml
metadata:
  - apiGroups:
      - "*"
    kinds:
      - "*"
      - Namespace
```

The following selectors do **not** target Namespace:

```yaml
# A full kind wildcard is not an explicit Namespace opt-in.
metadata:
  - apiGroups:
      - "*"
    kinds:
      - "*"

# A partial kind wildcard is not an explicit Namespace opt-in either,
# even when its pattern would otherwise match the word "Namespace".
  - apiGroups:
      - "v1"
    kinds:
      - "Name*"
```

In short, both conditions must be true: `apiGroups` must match core `v1`, and
`kinds` must contain a dedicated `Namespace` entry.

#### Important `apiGroups` behavior

Omitted or empty `apiGroups` does **not** mean all API groups and versions. It
means the core Kubernetes API version `v1`.

For example:

```yaml
metadata:
  - kinds:
      - Deployment
```

This does **not** match `apps/v1` `Deployment`, because omitted `apiGroups` is
interpreted as core `v1`.

To match `apps/v1` deployments, set the API group/version selector explicitly:

```yaml
metadata:
  - apiGroups:
      - apps/v1
    kinds:
      - Deployment
```

To match deployments across all API groups and versions, use `"*"`:

```yaml
metadata:
  - apiGroups:
      - "*"
    kinds:
      - Deployment
```

### Label rules

Label rules are configured under `metadata[].labels`. Each map key is the label key to validate.

```yaml
rules:
  - enforce:
      action: allow
      metadata:
        - kinds:
            - ConfigMap
          labels:
            env:
              required: true
              values:
                - exact:
                    - prod
                    - test
```

With this rule, a matching `ConfigMap` must contain `metadata.labels["env"]`, and its value must be either `prod` or `test`.

This object is admitted:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  labels:
    env: prod
data:
  key: value
```

This object is denied because the required label is missing:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
data:
  key: value
```

This object is denied because the label value does not match the allow-list:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  labels:
    env: stage
data:
  key: value
```

Example rejection:

```bash
Error from server (Forbidden): error when creating "configmap.yaml": admission webhook "rules.generic.projectcapsule.dev" denied the request: metadata label "env" is required at metadata.labels["env"]
```

### Annotation rules

Annotation rules are configured under `metadata[].annotations`. Each map key is the annotation key to validate.

```yaml
rules:
  - enforce:
      action: allow
      audience:
        - kind: "Group"
          name: "system:authenticated"
        - kind: "Custom"
          name: "CapsuleUser|Administrator|TenantOwner"
      metadata:
        - apiGroups:
            - "v1"
          kinds:
            - Namespace
          annotations:
            example.corp/cost-center:
              required: false
              values:
                - exp: "^INV-[0-9]{4}$"
              # If user / annotation is missing, use this as default value, only at admission mutation
              default: "II-1"
              # Overwrites anything, even if the user has set a value, Should be applied using SSA by the rulestatus controller, if removed also removes (one fieldmanager per rulestatus which controlles all managed metadata). Also enforce at admission
              managed: "II-10"
            example.corp/cost-center-2:
              values:
                - exp: "II-10"
              default: "{{$.tenant.spec.data.costCenter}}"

# Controller - APPLY
serviceOptions:
  labels:
    test: 2e2e



```

With this rule, the annotation is optional. If the object does not contain `metadata.annotations["example.corp/cost-center"]`, Capsule ignores the rule. If the annotation is present, its value must match the configured expression.

This object is admitted because the annotation is absent and `required` is `false`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
data:
  key: value
```

This object is admitted because the annotation value matches:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  annotations:
    example.corp/cost-center: INV-1234
data:
  key: value
```

This object is denied because the annotation is present but does not match:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  annotations:
    example.corp/cost-center: BAD-1234
data:
  key: value
```

### Required metadata

The `required` field controls whether the metadata key must be present.

| `required` | Behavior |
|---|---|
| `true` | For `action: allow`, the key must be present on matching objects. |
| `false` | The key is optional. If it is missing, Capsule ignores it. If it is present, configured values are evaluated. |

`required` is meaningful for `action: allow`. `deny` and `audit` rules are value matchers; they do not require missing metadata to exist.

Presence-only enforcement is possible by setting `required: true` and omitting `values`:

```yaml
rules:
  - enforce:
      action: allow
      metadata:
        - kinds:
            - ConfigMap
          labels:
            tenant-approved:
              required: true
```

With this rule, matching `ConfigMap` resources must contain the `tenant-approved` label, but any value is accepted.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  labels:
    tenant-approved: "true"
data:
  key: value
```

If the label is missing, the request is denied.

### Metadata values

The `values` field uses the common match expression structure with `exact`, `exp`, and optional `negate`.

```yaml
values:
  - exact:
      - prod
      - test
  - exp: "^sandbox-[0-9]+$"
```

A metadata value matches if any configured value matcher matches.

`exact` and `exp` can be combined in the same matcher:

```yaml
values:
  - exact:
      - prod
      - test
    exp: "^dev-[0-9]+$"
```

This matcher allows `prod`, `test`, and values matching `^dev-[0-9]+$`.

`negate: true` inverts the final matcher result:

```yaml
rules:
  - enforce:
      action: deny
      metadata:
        - apiVersion: "*"
          kinds:
            - ConfigMap
          labels:
            team:
              values:
                - exp: "^trusted-.*"
                  negate: true
```

With this rule:

* `team=trusted-platform` is not denied by this deny-only rule.
* `team=untrusted` is denied.

If an allow-list also exists for the same metadata key, values excluded from a negated deny rule still need a matching allow rule.

### Advanced

#### Allow-list behavior for metadata

An `allow` rule creates an allow-list for the specific metadata key it controls.

```yaml
rules:
  - enforce:
      action: allow
      metadata:
        - kinds:
            - ConfigMap
          labels:
            env:
              required: true
              values:
                - exact:
                    - prod
                    - test
```

With this rule:

| Object label | Result |
|---|---|
| `env=prod` | Allowed |
| `env=test` | Allowed |
| `env=stage` | Denied |
| missing `env` | Denied because `required: true` |

If `required` is `false`, missing metadata is ignored:

```yaml
rules:
  - enforce:
      action: allow
      metadata:
        - kinds:
            - ConfigMap
          labels:
            env:
              required: false
              values:
                - exact:
                    - prod
                    - test
```

With this rule:

| Object label | Result |
|---|---|
| `env=prod` | Allowed |
| `env=test` | Allowed |
| `env=stage` | Denied |
| missing `env` | Allowed |

Allow-list behavior is evaluated per metadata key. A matching value for one key does not satisfy another required key.

For example:

```yaml
rules:
  - enforce:
      action: allow
      metadata:
        - kinds:
            - ConfigMap
          labels:
            env:
              required: true
              values:
                - exact:
                    - prod
            team:
              required: true
              values:
                - exact:
                    - platform
```

The object must contain both `env=prod` and `team=platform`.

#### Deny metadata values

Use `action: deny` to reject specific metadata values.

```yaml
rules:
  - enforce:
      action: deny
      metadata:
        - kinds:
            - ConfigMap
          labels:
            environment:
              values:
                - exact:
                    - deprecated
```

This `ConfigMap` is denied:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  labels:
    environment: deprecated
data:
  key: value
```

A later matching `allow` rule can override an earlier `deny` rule:

```yaml
rules:
  - enforce:
      action: deny
      metadata:
        - kinds:
            - ConfigMap
          labels:
            environment:
              values:
                - exact:
                    - deprecated

  - namespaceSelector:
      matchLabels:
        allow-deprecated: "true"
    enforce:
      action: allow
      metadata:
        - kinds:
            - ConfigMap
          labels:
            environment:
              required: true
              values:
                - exact:
                    - deprecated
```

In namespaces labeled `allow-deprecated=true`, `environment=deprecated` is admitted because the later namespace-specific allow rule matches.

#### Audit metadata values

Use `action: audit` to observe metadata usage without blocking the request.

```yaml
rules:
  - enforce:
      action: audit
      metadata:
        - apiGroups:
            - "*"
          kinds:
            - ConfigMap
            - Service
          labels:
            example.corp/audit:
              values:
                - exp: "^audit-.*"
```

A matching object is admitted in this audit-only example, but Capsule emits an audit event and returns an admission warning.

If an allow-list also exists for the same metadata key, audit does not satisfy that allow-list. The metadata value must still match an `allow` rule.

#### Multiple resource kinds

A single metadata rule can target multiple kinds:

```yaml
rules:
  - enforce:
      action: allow
      metadata:
        - apiGroups:
            - "*"
          kinds:
            - ConfigMap
            - Service
          labels:
            corp.com/tenant:
              required: true
              values:
                - exact:
                    - prod
                    - test
```

With this rule, both matching `ConfigMap` and `Service` objects must contain `corp.com/tenant=prod` or `corp.com/tenant=test`.

#### Namespace-specific metadata rules

Metadata enforcement supports `namespaceSelector` like other namespace rules.

```yaml
rules:
  - namespaceSelector:
      matchLabels:
        environment: prod
    enforce:
      action: allow
      metadata:
        - kinds:
            - ConfigMap
          annotations:
            example.corp/approval:
              required: true
              values:
                - exact:
                    - approved
```

This rule only applies to namespaces labeled `environment=prod`. In those namespaces, matching `ConfigMap` objects must contain `example.corp/approval=approved`.

#### Complete metadata enforcement example

The following example combines required labels, optional annotations, multiple kinds, audit rules, deny rules, and namespace-specific exceptions:

```yaml
---
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: solar
spec:
  ...
  rules:
    - enforce:
        action: allow
        metadata:
          - apiGroups:
            - "*"
            kinds:
              - ConfigMap
              - Service
            labels:
              corp.com/tenant:
                required: true
                values:
                  - exact:
                      - prod
                      - test
            annotations:
              example.corp/cost-center:
                required: false
                values:
                  - exp: "^INV-[0-9]{4}$"
                  - exact:
                      - prod
                      - test

    - enforce:
        action: deny
        metadata:
          - kinds:
              - ConfigMap
            labels:
              environment:
                values:
                  - exact:
                      - deprecated

    - enforce:
        action: audit
        metadata:
          - apiGroups:
            - "*"
            kinds:
              - ConfigMap
              - Service
            labels:
              example.corp/audit:
                values:
                  - exp: "^audit-.*"

    - namespaceSelector:
        matchLabels:
          environment: prod
      enforce:
        action: allow
        metadata:
          - kinds:
              - ConfigMap
            annotations:
              example.corp/approval:
                required: true
                values:
                  - exact:
                      - approved
```

With this configuration:

* `ConfigMap` and `Service` objects must contain `projectcapsule.dev/tenant=prod` or `projectcapsule.dev/tenant=test`.
* `example.corp/cost-center` is optional, but if present it must match `^INV-[0-9]{4}$`, `prod`, or `test`.
* `ConfigMap` objects with `environment=deprecated` are denied unless a later matching allow rule overrides the decision.
* Objects with `example.corp/audit` values matching `^audit-.*` emit audit events.
* In namespaces labeled `environment=prod`, `ConfigMap` objects must also contain `example.corp/approval=approved`.
