# Deploying the Capsule Operator

Use the Capsule Operator for easily implementing, managing, and maintaining multitenancy and access control in Kubernetes. Please read our installation guide:

* [https://projectcapsule.dev/docs/operating/setup/installation/](https://projectcapsule.dev/docs/operating/setup/installation/)

## Major Changes

In the following sections you see actions which are required when you are upgrading to a specific version.

### Upgrading to 0.7.x

Introduces a new methode to manage all capsule CRDs and their lifecycle. We are no longer relying on the [native CRD hook with the Helm Chart](https://helm.sh/docs/chart_best_practices/custom_resource_definitions/#some-caveats-and-explanations). The hook only allows to manage CRDs on install and uninstall but we can't deliver updates to the CRDs.
When you newly install the chart we recommend to set  `crds.install` to `true`. This will manage the CRDs with the Helm Chart. This behavior is the new default.

#### Changed Values

The following Values have changed key or Value:

  * All values from previous releases under `webhooks` have moved to `webhooks.hooks`.
  * `mutatingWebhooksTimeoutSeconds` has moved to `webhooks.mutatingWebhooksTimeoutSeconds`
  * `validatingWebhooksTimeoutSeconds` has moved to `webhooks.validatingWebhooksTimeoutSeconds`

## Values

### CustomResourceDefinition Lifecycle

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| crds.annnotations | object | `{}` | Extra Annotations for CRDs |
| crds.createConfig | bool | `false` | Create additionally CapsuleConfiguration even if CRDs are exclusive |
| crds.exclusive | bool | `false` | Only install the CRDs, no other primitives |
| crds.inline | bool | `false` |  |
| crds.install | bool | `true` | Install the CustomResourceDefinitions (This also manages the lifecycle of the CRDs for update operations) |
| crds.labels | object | `{}` | Extra Labels for CRDs |

### Global Parameters

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| global.jobs.kubectl.affinity | object | `{}` | Set affinity rules |
| global.jobs.kubectl.annotations | object | `{}` | Annotations to add to the job. |
| global.jobs.kubectl.backoffLimit | int | `4` | Backofflimit for jobs |
| global.jobs.kubectl.image.pullPolicy | string | `"IfNotPresent"` | Set the image pull policy of the helm chart job |
| global.jobs.kubectl.image.registry | string | `"docker.io"` | Set the image repository of the helm chart job |
| global.jobs.kubectl.image.repository | string | `"clastix/kubectl"` | Set the image repository of the helm chart job |
| global.jobs.kubectl.image.tag | string | `""` | Set the image tag of the helm chart job |
| global.jobs.kubectl.imagePullSecrets | list | `[]` | ImagePullSecrets |
| global.jobs.kubectl.labels | object | `{}` | Labels to add to the job. |
| global.jobs.kubectl.nodeSelector | object | `{}` | Set the node selector |
| global.jobs.kubectl.podAnnotations | object | `{}` | Annotations to add to the job pod |
| global.jobs.kubectl.podLabels | object | `{}` | Labels to add to the job pod |
| global.jobs.kubectl.podSecurityContext | object | `{"enabled":true,"seccompProfile":{"type":"RuntimeDefault"}}` | Security context for the job pods. |
| global.jobs.kubectl.priorityClassName | string | `""` | Set a pod priorityClassName |
| global.jobs.kubectl.resources | object | `{}` | Job resources |
| global.jobs.kubectl.restartPolicy | string | `"Never"` | Set the restartPolicy |
| global.jobs.kubectl.securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"enabled":true,"readOnlyRootFilesystem":true,"runAsGroup":1002,"runAsNonRoot":true,"runAsUser":1002}` | Security context for the job containers. |
| global.jobs.kubectl.tolerations | list | `[]` | Set list of tolerations |
| global.jobs.kubectl.topologySpreadConstraints | list | `[]` | Set Topology Spread Constraints |
| global.jobs.kubectl.ttlSecondsAfterFinished | int | `60` | Sets the ttl in seconds after a finished certgen job is deleted. Set to -1 to never delete. |
| global.jobs.postInstall.enabled | bool | `true` | Enable Post Install Job |
| global.jobs.preDelete.enabled | bool | `true` | Enable Pre Delete Job |

### General Parameters

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` | Set affinity rules for the Capsule pod |
| certManager.additionalSANS | list | `[]` | Specify additional SANS to add to the certificate |
| certManager.generateCertificates | bool | `true` | Specifies whether capsule webhooks certificates should be generated using cert-manager |
| customAnnotations | object | `{}` | Additional annotations which will be added to all resources created by Capsule helm chart |
| customLabels | object | `{}` | Additional labels which will be added to all resources created by Capsule helm chart |
| extraManifests | list | `[]` | Array of additional resources to be created alongside Capsule helm chart |
| imagePullSecrets | list | `[]` | Configuration for `imagePullSecrets` so that you can use a private images registry. |
| jobs | object | `{}` | Deprecated, use .global.jobs.kubectl instead |
| nodeSelector | object | `{}` | Set the node selector for the Capsule pod |
| podAnnotations | object | `{}` | Annotations to add to the capsule pod. |
| podLabels | object | `{}` | Labels to add to the capsule pod. |
| podSecurityContext | object | `{"enabled":true,"runAsGroup":1002,"runAsNonRoot":true,"runAsUser":1002,"seccompProfile":{"type":"RuntimeDefault"}}` | Set the securityContext for the Capsule pod |
| ports | list | `[]` | Set additional ports for the deployment |
| priorityClassName | string | `""` | Set the priority class name of the Capsule pod |
| proxy.enabled | bool | `false` | Enable Installation of Capsule Proxy |
| rbac.resourcepoolclaims.create | bool | `false` |  |
| rbac.resourcepoolclaims.labels."rbac.authorization.k8s.io/aggregate-to-admin" | string | `"true"` |  |
| rbac.resources.create | bool | `false` |  |
| rbac.resources.labels."rbac.authorization.k8s.io/aggregate-to-admin" | string | `"true"` |  |
| replicaCount | int | `1` | Set the replica count for capsule pod |
| securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"enabled":true,"readOnlyRootFilesystem":true}` | Set the securityContext for the Capsule container |
| serviceAccount.annotations | object | `{}` | Annotations to add to the service account. |
| serviceAccount.create | bool | `true` | Specifies whether a service account should be created. |
| serviceAccount.name | string | `""` | The name of the service account to use. If not set and `serviceAccount.create=true`, a name is generated using the fullname template |
| tls.create | bool | `false` | When cert-manager is disabled, Capsule will generate the TLS certificate for webhook and CRDs conversion. |
| tls.enableController | bool | `false` | Start the Capsule controller that injects the CA into mutating and validating webhooks, and CRD as well. |
| tls.name | string | `""` | Override name of the Capsule TLS Secret name when externally managed. |
| tolerations | list | `[]` | Set list of tolerations for the Capsule pod |
| topologySpreadConstraints | list | `[]` | Set topology spread constraints for the Capsule pod |

### Manager Parameters

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| manager.daemonsetStrategy | object | `{"type":"RollingUpdate"}` | [Daemonset Strategy](https://kubernetes.io/docs/tasks/manage-daemon/update-daemon-set/#creating-a-daemonset-with-rollingupdate-update-strategy) |
| manager.deploymentStrategy | object | `{"type":"RollingUpdate"}` | [Deployment Strategy](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#strategy) |
| manager.env | list | `[]` | Additional Environment Variables |
| manager.extraArgs | list | `["--enable-leader-election=true"]` | A list of extra arguments for the capsule controller |
| manager.hostNetwork | bool | `false` | Specifies if the container should be started in hostNetwork mode.  Required for use in some managed kubernetes clusters (such as AWS EKS) with custom CNI (such as calico), because control-plane managed by AWS cannot communicate with pods' IP CIDR and admission webhooks are not working |
| manager.hostPID | bool | `false` | Specifies if the container should be started in hostPID mode. |
| manager.hostUsers | bool | `true` | Don't use Host Users (User Namespaces) |
| manager.image.pullPolicy | string | `"IfNotPresent"` | Set the image pull policy. |
| manager.image.registry | string | `"ghcr.io"` | Set the image registry of capsule. |
| manager.image.repository | string | `"projectcapsule/capsule"` | Set the image repository of capsule. |
| manager.image.tag | string | `""` | Overrides the image tag whose default is the chart appVersion. |
| manager.kind | string | `"Deployment"` | Set the controller deployment mode as `Deployment` or `DaemonSet`. |
| manager.livenessProbe | object | `{"httpGet":{"path":"/healthz","port":10080}}` | Configure the liveness probe using Deployment probe spec |
| manager.options.administrators | list | `[]` | Define entities which can act as Administrators in the capsule construct These entities are automatically owners for all existing tenants. Meaning they can add namespaces to any tenant. However they must be specific by using the capsule label for interacting with namespaces. Because if that label is not defined, it's assumed that namespace interaction was not targeted towards a tenant and will therefor be ignored by capsule. May also be handy in GitOps scenarios where certain service accounts need to be able to manage namespaces for all tenants. |
| manager.options.allowServiceAccountPromotion | bool | `false` | ServiceAccounts within tenant namespaces can be promoted to owners of the given tenant this can be achieved by labeling the serviceaccount and then they are considered owners. This can only be done by other owners of the tenant. However ServiceAccounts which have been promoted to owner can not promote further serviceAccounts. |
| manager.options.annotations | object | `{}` | Additional annotations to add to the CapsuleConfiguration resource |
| manager.options.capsuleConfiguration | string | `"default"` | Change the default name of the capsule configuration name |
| manager.options.capsuleUserGroups | list | `[]` | DEPRECATED: use users properties. Names of the users considered as Capsule users. |
| manager.options.createConfiguration | bool | `true` | Create Configuration |
| manager.options.forceTenantPrefix | bool | `false` | Boolean, enforces the Tenant owner, during Namespace creation, to name it using the selected Tenant name as prefix, separated by a dash |
| manager.options.generateCertificates | bool | `true` | Specifies whether capsule webhooks certificates should be generated by capsule operator |
| manager.options.ignoreUserWithGroups | list | `[]` | Define groups which when found in the request of a user will be ignored by the Capsule this might be useful if you have one group where all the users are in, but you want to separate administrators from normal users with additional groups. |
| manager.options.labels | object | `{}` | Additional labels to add to the CapsuleConfiguration resource |
| manager.options.logLevel | string | `"info"` | Set the log verbosity of the capsule with a value from 1 to 5 |
| manager.options.nodeMetadata | object | `{"forbiddenAnnotations":{"denied":[],"deniedRegex":""},"forbiddenLabels":{"denied":[],"deniedRegex":""}}` | Allows to set the forbidden metadata for the worker nodes that could be patched by a Tenant |
| manager.options.protectedNamespaceRegex | string | `""` | If specified, disallows creation of namespaces matching the passed regexp |
| manager.options.userNames | list | `[]` | DEPRECATED: use users properties. Names of the users considered as Capsule users. |
| manager.options.users | list | `[{"kind":"Group","name":"projectcapsule.dev"}]` | Define entities which are considered part of the Capsule construct. Users not mentioned here will be ignored by Capsule |
| manager.options.workers | int | `1` | Workers (MaxConcurrentReconciles) is the maximum number of concurrent Reconciles which can be run (ALPHA). |
| manager.rbac.create | bool | `true` | Specifies whether RBAC resources should be created. |
| manager.rbac.existingClusterRoles | list | `[]` | Specifies further cluster roles to be added to the Capsule manager service account. |
| manager.rbac.existingRoles | list | `[]` | Specifies further cluster roles to be added to the Capsule manager service account. |
| manager.readinessProbe | object | `{"httpGet":{"path":"/readyz","port":10080}}` | Configure the readiness probe using Deployment probe spec |
| manager.resources | object | `{}` | Set the resource requests/limits for the Capsule manager container |
| manager.securityContext | object | `{}` | Set the securityContext for the Capsule container |
| manager.volumeMounts | list | `[]` | Set the additional volumeMounts needed for the Capsule manager container |
| manager.volumes | list | `[]` | Set the additional volumes needed for the Capsule manager container |
| manager.webhookPort | int | `9443` | Set an alternative to the default container port.  Useful for use in some kubernetes clusters (such as GKE Private) with aggregator routing turned on, because pod ports have to be opened manually on the firewall side |

### Monitoring Parameters

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| monitoring.dashboards.annotations | object | `{}` | Annotations for dashboard configmaps |
| monitoring.dashboards.enabled | bool | `false` | Enable Dashboards to be deployed |
| monitoring.dashboards.labels | object | `{}` | Labels for dashboard configmaps |
| monitoring.dashboards.namespace | string | `""` | Custom namespace for dashboard configmaps |
| monitoring.dashboards.operator.allowCrossNamespaceImport | bool | `true` | Allow the Operator to match this resource with Grafanas outside the current namespace |
| monitoring.dashboards.operator.enabled | bool | `false` | Enable Operator Resources (GrafanaDashboard) |
| monitoring.dashboards.operator.folder | string | `""` | folder assignment for dashboard |
| monitoring.dashboards.operator.instanceSelector | object | `{}` | Selects Grafana instances for import |
| monitoring.dashboards.operator.resyncPeriod | string | `"10m"` | How often the resource is synced, defaults to 10m0s if not set |
| monitoring.serviceMonitor.annotations | object | `{}` | Assign additional Annotations |
| monitoring.serviceMonitor.enabled | bool | `false` | Enable ServiceMonitor |
| monitoring.serviceMonitor.endpoint.interval | string | `"15s"` | Set the scrape interval for the endpoint of the serviceMonitor |
| monitoring.serviceMonitor.endpoint.metricRelabelings | list | `[]` | Set metricRelabelings for the endpoint of the serviceMonitor |
| monitoring.serviceMonitor.endpoint.relabelings | list | `[]` | Set relabelings for the endpoint of the serviceMonitor |
| monitoring.serviceMonitor.endpoint.scrapeTimeout | string | `""` | Set the scrape timeout for the endpoint of the serviceMonitor |
| monitoring.serviceMonitor.labels | object | `{}` | Assign additional labels according to Prometheus' serviceMonitorSelector matching labels |
| monitoring.serviceMonitor.matchLabels | object | `{}` | Change matching labels |
| monitoring.serviceMonitor.namespace | string | `""` | Install the ServiceMonitor into a different Namespace, as the monitoring stack one (default: the release one) |
| monitoring.serviceMonitor.targetLabels | list | `[]` | Set targetLabels for the serviceMonitor |

### Admission Webhook Parameters

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| webhooks.exclusive | bool | `false` | When `crds.exclusive` is `true` the webhooks will be installed |
| webhooks.hooks.config.enabled | bool | `true` | Enable the Hook |
| webhooks.hooks.config.failurePolicy | string | `"Ignore"` | [FailurePolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy) |
| webhooks.hooks.config.matchConditions | list | `[]` | [MatchConditions](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.config.matchPolicy | string | `"Exact"` | [MatchPolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.config.namespaceSelector | object | `{}` | [NamespaceSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-namespaceselector) |
| webhooks.hooks.config.objectSelector | object | `{}` | [ObjectSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-objectselector) |
| webhooks.hooks.config.reinvocationPolicy | string | `"Never"` | [ReinvocationPolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#reinvocation-policy) |
| webhooks.hooks.cordoning.enabled | bool | `true` | Enable the Hook |
| webhooks.hooks.cordoning.failurePolicy | string | `"Fail"` | [FailurePolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy) |
| webhooks.hooks.cordoning.matchConditions | list | `[]` | [MatchConditions](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.cordoning.matchPolicy | string | `"Equivalent"` | [MatchPolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.cordoning.namespaceSelector | object | `{"matchExpressions":[{"key":"capsule.clastix.io/tenant","operator":"Exists"},{"key":"projectcapsule.dev/cordoned","operator":"Exists"}]}` | [NamespaceSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-namespaceselector) |
| webhooks.hooks.cordoning.objectSelector | object | `{}` | [ObjectSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-objectselector) |
| webhooks.hooks.cordoning.rules | list | `[{"apiGroups":["*"],"apiVersions":["*"],"operations":["CREATE","UPDATE","DELETE"],"resources":["*"],"scope":"Namespaced"}]` | [Rules](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-rules) |
| webhooks.hooks.customresources.enabled | bool | `true` | Enable the Hook |
| webhooks.hooks.customresources.failurePolicy | string | `"Fail"` | [FailurePolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy) |
| webhooks.hooks.customresources.matchConditions | list | `[]` | [MatchConditions](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.customresources.matchPolicy | string | `"Equivalent"` | [MatchPolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.customresources.namespaceSelector | object | `{"matchExpressions":[{"key":"capsule.clastix.io/tenant","operator":"Exists"}]}` | [NamespaceSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-namespaceselector) |
| webhooks.hooks.customresources.objectSelector | object | `{}` | [ObjectSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-objectselector) |
| webhooks.hooks.defaults.ingress | object | `{}` | Deprecated, use webhooks.hooks.ingresses instead |
| webhooks.hooks.defaults.pods | object | `{}` | Deprecated, use webhooks.hooks.pods instead |
| webhooks.hooks.defaults.pvc | object | `{}` | Deprecated, use webhooks.hooks.persistentvolumeclaims instead |
| webhooks.hooks.devices.enabled | bool | `true` | Enable the Hook |
| webhooks.hooks.devices.failurePolicy | string | `"Fail"` | [FailurePolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy) |
| webhooks.hooks.devices.matchConditions | list | `[]` | [MatchConditions](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.devices.matchPolicy | string | `"Equivalent"` | [MatchPolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.devices.namespaceSelector | object | `{"matchExpressions":[{"key":"capsule.clastix.io/tenant","operator":"Exists"}]}` | [NamespaceSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-namespaceselector) |
| webhooks.hooks.devices.objectSelector | object | `{}` | [ObjectSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-objectselector) |
| webhooks.hooks.devices.reinvocationPolicy | string | `"Never"` | [ReinvocationPolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#reinvocation-policy) |
| webhooks.hooks.gateways.enabled | bool | `true` | Enable the Hook |
| webhooks.hooks.gateways.failurePolicy | string | `"Fail"` | [FailurePolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy) |
| webhooks.hooks.gateways.matchConditions | list | `[]` | [MatchConditions](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.gateways.matchPolicy | string | `"Equivalent"` | [MatchPolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.gateways.namespaceSelector | object | `{"matchExpressions":[{"key":"capsule.clastix.io/tenant","operator":"Exists"}]}` | [NamespaceSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-namespaceselector) |
| webhooks.hooks.gateways.objectSelector | object | `{}` | [ObjectSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-objectselector) |
| webhooks.hooks.ingresses.enabled | bool | `true` | Enable the Hook |
| webhooks.hooks.ingresses.failurePolicy | string | `"Fail"` | [FailurePolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy) |
| webhooks.hooks.ingresses.matchConditions | list | `[]` | [MatchConditions](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.ingresses.matchPolicy | string | `"Equivalent"` | [MatchPolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.ingresses.namespaceSelector | object | `{"matchExpressions":[{"key":"capsule.clastix.io/tenant","operator":"Exists"}]}` | [NamespaceSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-namespaceselector) |
| webhooks.hooks.ingresses.objectSelector | object | `{}` | [ObjectSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-objectselector) |
| webhooks.hooks.ingresses.reinvocationPolicy | string | `"Never"` | [ReinvocationPolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#reinvocation-policy) |
| webhooks.hooks.namespaceOwnerReference | object | `{}` | Deprecated, use webhooks.hooks.namespaces instead |
| webhooks.hooks.namespaces.enabled | bool | `true` | Enable the Hook |
| webhooks.hooks.namespaces.failurePolicy | string | `"Fail"` | [FailurePolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy) |
| webhooks.hooks.namespaces.matchConditions | list | `[]` | [MatchConditions](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.namespaces.matchPolicy | string | `"Equivalent"` | [MatchPolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.namespaces.namespaceSelector | object | `{}` | [NamespaceSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-namespaceselector) |
| webhooks.hooks.namespaces.objectSelector | object | `{}` | [ObjectSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-objectselector) |
| webhooks.hooks.namespaces.reinvocationPolicy | string | `"Never"` | [ReinvocationPolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#reinvocation-policy) |
| webhooks.hooks.networkpolicies.enabled | bool | `true` | Enable the Hook |
| webhooks.hooks.networkpolicies.failurePolicy | string | `"Fail"` | [FailurePolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy) |
| webhooks.hooks.networkpolicies.matchConditions | list | `[]` | [MatchConditions](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.networkpolicies.matchPolicy | string | `"Equivalent"` | [MatchPolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.networkpolicies.namespaceSelector | object | `{"matchExpressions":[{"key":"capsule.clastix.io/tenant","operator":"Exists"}]}` | [NamespaceSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-namespaceselector) |
| webhooks.hooks.networkpolicies.objectSelector | object | `{}` | [ObjectSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-objectselector) |
| webhooks.hooks.nodes.enabled | bool | `false` | Enable the Hook |
| webhooks.hooks.nodes.failurePolicy | string | `"Fail"` | [FailurePolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy) |
| webhooks.hooks.nodes.matchConditions | list | `[]` | [MatchConditions](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.nodes.matchPolicy | string | `"Exact"` | [MatchPolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.nodes.namespaceSelector | object | `{}` | [NamespaceSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-namespaceselector) |
| webhooks.hooks.nodes.objectSelector | object | `{}` | [ObjectSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-objectselector) |
| webhooks.hooks.persistentvolumeclaims.enabled | bool | `true` | Enable the Hook |
| webhooks.hooks.persistentvolumeclaims.failurePolicy | string | `"Fail"` | [FailurePolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy) |
| webhooks.hooks.persistentvolumeclaims.matchConditions | list | `[]` | [MatchConditions](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.persistentvolumeclaims.matchPolicy | string | `"Equivalent"` | [MatchPolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.persistentvolumeclaims.namespaceSelector | object | `{"matchExpressions":[{"key":"capsule.clastix.io/tenant","operator":"Exists"}]}` | [NamespaceSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-namespaceselector) |
| webhooks.hooks.persistentvolumeclaims.objectSelector | object | `{}` | [ObjectSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-objectselector) |
| webhooks.hooks.persistentvolumeclaims.reinvocationPolicy | string | `"Never"` | [ReinvocationPolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#reinvocation-policy) |
| webhooks.hooks.pods.enabled | bool | `true` | Enable the Hook |
| webhooks.hooks.pods.failurePolicy | string | `"Fail"` | [FailurePolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy) |
| webhooks.hooks.pods.matchConditions | list | `[]` | [MatchConditions](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.pods.matchPolicy | string | `"Exact"` | [MatchPolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.pods.namespaceSelector | object | `{"matchExpressions":[{"key":"capsule.clastix.io/tenant","operator":"Exists"}]}` | [NamespaceSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-namespaceselector) |
| webhooks.hooks.pods.objectSelector | object | `{}` | [ObjectSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-objectselector) |
| webhooks.hooks.pods.reinvocationPolicy | string | `"Never"` | [ReinvocationPolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#reinvocation-policy) |
| webhooks.hooks.resourcepools.claims.enabled | bool | `true` | Enable the Hook |
| webhooks.hooks.resourcepools.claims.failurePolicy | string | `"Fail"` | [FailurePolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy) |
| webhooks.hooks.resourcepools.claims.matchConditions | list | `[]` | [MatchConditions](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.resourcepools.claims.matchPolicy | string | `"Equivalent"` | [MatchPolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.resourcepools.claims.namespaceSelector | object | `{}` | [NamespaceSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-namespaceselector) |
| webhooks.hooks.resourcepools.claims.objectSelector | object | `{}` | [ObjectSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-objectselector) |
| webhooks.hooks.resourcepools.pools.enabled | bool | `true` | Enable the Hook |
| webhooks.hooks.resourcepools.pools.failurePolicy | string | `"Fail"` | [FailurePolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy) |
| webhooks.hooks.resourcepools.pools.matchConditions | list | `[]` | [MatchConditions](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.resourcepools.pools.matchPolicy | string | `"Equivalent"` | [MatchPolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.resourcepools.pools.namespaceSelector | object | `{}` | [NamespaceSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-namespaceselector) |
| webhooks.hooks.resourcepools.pools.objectSelector | object | `{}` | [ObjectSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-objectselector) |
| webhooks.hooks.serviceaccounts.enabled | bool | `true` | Enable the Hook |
| webhooks.hooks.serviceaccounts.failurePolicy | string | `"Fail"` | [FailurePolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy) |
| webhooks.hooks.serviceaccounts.matchConditions | list | `[]` | [MatchConditions](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.serviceaccounts.matchPolicy | string | `"Exact"` | [MatchPolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.serviceaccounts.namespaceSelector | object | `{"matchExpressions":[{"key":"capsule.clastix.io/tenant","operator":"Exists"}]}` | [NamespaceSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-namespaceselector) |
| webhooks.hooks.serviceaccounts.objectSelector | object | `{}` | [ObjectSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-objectselector) |
| webhooks.hooks.services.enabled | bool | `true` | Enable the Hook |
| webhooks.hooks.services.failurePolicy | string | `"Fail"` | [FailurePolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy) |
| webhooks.hooks.services.matchConditions | list | `[]` | [MatchConditions](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.services.matchPolicy | string | `"Exact"` | [MatchPolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.services.namespaceSelector | object | `{"matchExpressions":[{"key":"capsule.clastix.io/tenant","operator":"Exists"}]}` | [NamespaceSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-namespaceselector) |
| webhooks.hooks.services.objectSelector | object | `{}` | [ObjectSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-objectselector) |
| webhooks.hooks.tenantLabel.enabled | bool | `true` | Enable the Hook |
| webhooks.hooks.tenantLabel.failurePolicy | string | `"Fail"` | [FailurePolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy) |
| webhooks.hooks.tenantLabel.matchConditions | list | `[]` | [MatchConditions](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.tenantLabel.matchPolicy | string | `"Equivalent"` | [MatchPolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.tenantLabel.namespaceSelector | object | `{"matchExpressions":[{"key":"capsule.clastix.io/tenant","operator":"Exists"}]}` | [NamespaceSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-namespaceselector) |
| webhooks.hooks.tenantLabel.objectSelector | object | `{}` | [ObjectSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-objectselector) |
| webhooks.hooks.tenantLabel.reinvocationPolicy | string | `"Never"` | [ReinvocationPolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#reinvocation-policy) |
| webhooks.hooks.tenantLabel.rules | list | `[{"apiGroups":["*"],"apiVersions":["*"],"operations":["CREATE","UPDATE"],"resources":["*"],"scope":"Namespaced"}]` | [Rules](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-rules) |
| webhooks.hooks.tenantResourceObjects.enabled | bool | `true` | Enable the Hook |
| webhooks.hooks.tenantResourceObjects.failurePolicy | string | `"Fail"` | [FailurePolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy) |
| webhooks.hooks.tenantResourceObjects.matchConditions | list | `[]` | [MatchConditions](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.tenantResourceObjects.matchPolicy | string | `"Exact"` | [MatchPolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.tenantResourceObjects.namespaceSelector | object | `{"matchExpressions":[{"key":"capsule.clastix.io/tenant","operator":"Exists"}]}` | [NamespaceSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-namespaceselector) |
| webhooks.hooks.tenantResourceObjects.objectSelector | object | `{"matchExpressions":[{"key":"capsule.clastix.io/tenant","operator":"Exists"}]}` | [ObjectSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-objectselector) |
| webhooks.hooks.tenants.enabled | bool | `true` | Enable the Hook |
| webhooks.hooks.tenants.failurePolicy | string | `"Fail"` | [FailurePolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy) |
| webhooks.hooks.tenants.matchConditions | list | `[]` | [MatchConditions](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.tenants.matchPolicy | string | `"Exact"` | [MatchPolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.tenants.namespaceSelector | object | `{}` | [NamespaceSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-namespaceselector) |
| webhooks.hooks.tenants.objectSelector | object | `{}` | [ObjectSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-objectselector) |
| webhooks.hooks.tenants.reinvocationPolicy | string | `"Never"` | [ReinvocationPolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#reinvocation-policy) |
| webhooks.mutatingWebhooksTimeoutSeconds | int | `30` | Timeout in seconds for mutating webhooks |
| webhooks.service.caBundle | string | `""` | CABundle for the webhook service |
| webhooks.service.name | string | `""` | Custom service name for the webhook service |
| webhooks.service.namespace | string | `""` | Custom service namespace for the webhook service |
| webhooks.service.port | string | `nil` | Custom service port for the webhook service |
| webhooks.service.url | string | `""` | The URL where the capsule webhook services are running (Overwrites cluster scoped service definition) |
| webhooks.validatingWebhooksTimeoutSeconds | int | `30` | Timeout in seconds for validating webhooks |

## Notes on installing Custom Resource Definitions with Helm3

Capsule, as many other add-ons, defines its own set of Custom Resource Definitions (CRDs). Helm3 removed the old CRDs installation method for a more simple methodology. In the Helm Chart, there is now a special directory called `crds` to hold the CRDs. These CRDs are not templated, but will be installed by default when running a `helm install` for the chart. If the CRDs already exist (for example, you already executed `helm install`), it will be skipped with a warning. When you wish to skip the CRDs installation, and do not see the warning, you can pass the `--skip-crds` flag to the `helm install` command.

## Cert-Manager integration

You can enable the generation of certificates using `cert-manager` as follows.

```
helm upgrade --install capsule projectcapsule/capsule --namespace capsule-system --create-namespace \
  --set "certManager.generateCertificates=true" \
  --set "tls.create=false" \
  --set "tls.enableController=false"
```

With the usage of `tls.enableController=false` value, you're delegating the injection of the Validating and Mutating Webhooks' CA to `cert-manager`.
Since Helm3 doesn't allow to template _CRDs_, you have to patch manually the Custom Resource Definition `tenants.capsule.clastix.io` adding the proper annotation (YMMV).

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.5.0
    cert-manager.io/inject-ca-from: capsule-system/capsule-webhook-cert
  creationTimestamp: "2022-07-22T08:32:51Z"
  generation: 45
  name: tenants.capsule.clastix.io
  resourceVersion: "9832"
  uid: 61e287df-319b-476d-88d5-bdb8dc14d4a6
```

## More

See Capsule [tutorial](https://github.com/projectcapsule/capsule/blob/master/docs/content/general/tutorial.md) for more information about how to use Capsule.
