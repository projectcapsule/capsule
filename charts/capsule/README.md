# Deploying the Capsule Operator

Use the Capsule Operator for easily implementing, managing, and maintaining multitenancy and access control in Kubernetes.

## Requirements

* [Helm 3](https://github.com/helm/helm/releases) is required when installing the Capsule Operator chart. Follow Helm’s official [steps](https://helm.sh/docs/intro/install/) for installing helm on your particular operating system.

* A Kubernetes cluster 1.16+ with following [Admission Controllers](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/) enabled:

    * PodNodeSelector
    * LimitRanger
    * ResourceQuota
    * MutatingAdmissionWebhook
    * ValidatingAdmissionWebhook

* A [`kubeconfig`](https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/) file accessing the Kubernetes cluster with cluster admin permissions.

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

## Installation

The Capsule Operator requires it's CRDs to be installed before the operator itself. Since the Helm CRD lifecycle has limitations, we recommend to install the CRDs separately. Our chart supports the installation of crds via a dedicated Release.
The Capsule Operator Chart can be used to instantly deploy the Capsule Operator on your Kubernetes cluster.

1. Add this repository:

        $ helm repo add projectcapsule https://projectcapsule.github.io/charts

2. Install Capsule:

        $ helm install capsule projectcapsule/capsule --version 0.7.0 -n capsule-system --create-namespace

        or

        $ helm install capsule oci://ghcr.io/projectcapsule/charts/capsule --version 0.7.0  -n capsule-system --create-namespace

3. Show the status:

        $ helm status capsule -n capsule-system

4. Upgrade the Chart

        $ helm upgrade capsule projectcapsule/capsule -n capsule-system

        or

        $ helm upgrade capsule oci://ghcr.io/projectcapsule/charts/capsule --version 0.4.7

5. Uninstall the Chart

        $ helm uninstall capsule -n capsule-system

## Customize the installation

There are two methods for specifying overrides of values during chart installation: `--values` and `--set`.

The `--values` option is the preferred method because it allows you to keep your overrides in a YAML file, rather than specifying them all on the command line. Create a copy of the YAML file `values.yaml` and add your overrides to it.

Specify your overrides file when you install the chart:

        $ helm install capsule capsule-helm-chart --values myvalues.yaml -n capsule-system

The values in your overrides file `myvalues.yaml` will override their counterparts in the chart's values.yaml file. Any values in `values.yaml` that weren’t overridden will keep their defaults.

If you only need to make minor customizations, you can specify them on the command line by using the `--set` option. For example:

        $ helm install capsule capsule-helm-chart --set manager.options.forceTenantPrefix=false -n capsule-system

Here the values you can override:

### CustomResourceDefinition Lifecycle

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| crds.annnotations | object | `{}` | Extra Annotations for CRDs |
| crds.exclusive | bool | `false` | Only install the CRDs, no other primitives |
| crds.install | bool | `true` | Install the CustomResourceDefinitions (This also manages the lifecycle of the CRDs for update operations) |
| crds.labels | object | `{}` | Extra Labels for CRDs |

### Global Parameters

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| global.jobs.kubectl.affinity | object | `{}` | Set affinity rules |
| global.jobs.kubectl.annotations | object | `{}` | Annotations to add to the certgen job. |
| global.jobs.kubectl.backoffLimit | int | `4` | Backofflimit for jobs |
| global.jobs.kubectl.image.pullPolicy | string | `"IfNotPresent"` | Set the image pull policy of the helm chart job |
| global.jobs.kubectl.image.registry | string | `"docker.io"` | Set the image repository of the helm chart job |
| global.jobs.kubectl.image.repository | string | `"clastix/kubectl"` | Set the image repository of the helm chart job |
| global.jobs.kubectl.image.tag | string | `""` | Set the image tag of the helm chart job |
| global.jobs.kubectl.imagePullSecrets | list | `[]` | ImagePullSecrets |
| global.jobs.kubectl.nodeSelector | object | `{}` | Set the node selector |
| global.jobs.kubectl.podSecurityContext | object | `{"seccompProfile":{"type":"RuntimeDefault"}}` | Security context for the job pods. |
| global.jobs.kubectl.priorityClassName | string | `""` | Set a pod priorityClassName |
| global.jobs.kubectl.resources | object | `{}` | Job resources |
| global.jobs.kubectl.restartPolicy | string | `"Never"` | Set the restartPolicy |
| global.jobs.kubectl.securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"readOnlyRootFilesystem":true,"runAsGroup":1002,"runAsNonRoot":true,"runAsUser":1002}` | Security context for the job containers. |
| global.jobs.kubectl.tolerations | list | `[]` | Set list of tolerations |
| global.jobs.kubectl.topologySpreadConstraints | list | `[]` | Set Topology Spread Constraints |
| global.jobs.kubectl.ttlSecondsAfterFinished | int | `60` | Sets the ttl in seconds after a finished certgen job is deleted. Set to -1 to never delete. |

### General Parameters

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` | Set affinity rules for the Capsule pod |
| certManager.additionalSANS | list | `[]` | Specify additional SANS to add to the certificate |
| certManager.generateCertificates | bool | `false` | Specifies whether capsule webhooks certificates should be generated using cert-manager |
| customAnnotations | object | `{}` | Additional annotations which will be added to all resources created by Capsule helm chart |
| customLabels | object | `{}` | Additional labels which will be added to all resources created by Capsule helm chart |
| imagePullSecrets | list | `[]` | Configuration for `imagePullSecrets` so that you can use a private images registry. |
| jobs | object | `{}` | Deprecated, use .global.jobs.kubectl instead |
| nodeSelector | object | `{}` | Set the node selector for the Capsule pod |
| podAnnotations | object | `{}` | Annotations to add to the capsule pod. |
| podSecurityContext | object | `{"runAsGroup":1002,"runAsNonRoot":true,"runAsUser":1002,"seccompProfile":{"type":"RuntimeDefault"}}` | Set the securityContext for the Capsule pod |
| priorityClassName | string | `""` | Set the priority class name of the Capsule pod |
| proxy.enabled | bool | `false` | Enable Installation of Capsule Proxy |
| replicaCount | int | `1` | Set the replica count for capsule pod |
| securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"readOnlyRootFilesystem":true}` | Set the securityContext for the Capsule container |
| serviceAccount.annotations | object | `{}` | Annotations to add to the service account. |
| serviceAccount.create | bool | `true` | Specifies whether a service account should be created. |
| serviceAccount.name | string | `""` | The name of the service account to use. If not set and `serviceAccount.create=true`, a name is generated using the fullname template |
| tls.create | bool | `true` | When cert-manager is disabled, Capsule will generate the TLS certificate for webhook and CRDs conversion. |
| tls.enableController | bool | `true` | Start the Capsule controller that injects the CA into mutating and validating webhooks, and CRD as well. |
| tls.name | string | `""` | Override name of the Capsule TLS Secret name when externally managed. |
| tolerations | list | `[]` | Set list of tolerations for the Capsule pod |
| topologySpreadConstraints | list | `[]` | Set topology spread constraints for the Capsule pod |

### Manager Parameters

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| manager.hostNetwork | bool | `false` | Specifies if the container should be started in hostNetwork mode.  Required for use in some managed kubernetes clusters (such as AWS EKS) with custom CNI (such as calico), because control-plane managed by AWS cannot communicate with pods' IP CIDR and admission webhooks are not working |
| manager.image.pullPolicy | string | `"IfNotPresent"` | Set the image pull policy. |
| manager.image.registry | string | `"ghcr.io"` | Set the image registry of capsule. |
| manager.image.repository | string | `"projectcapsule/capsule"` | Set the image repository of capsule. |
| manager.image.tag | string | `""` | Overrides the image tag whose default is the chart appVersion. |
| manager.kind | string | `"Deployment"` | Set the controller deployment mode as `Deployment` or `DaemonSet`. |
| manager.livenessProbe | object | `{"httpGet":{"path":"/healthz","port":10080}}` | Configure the liveness probe using Deployment probe spec |
| manager.options.capsuleConfiguration | string | `"default"` | Change the default name of the capsule configuration name |
| manager.options.capsuleUserGroups | list | `["projectcapsule.dev"]` | Override the Capsule user groups |
| manager.options.forceTenantPrefix | bool | `false` | Boolean, enforces the Tenant owner, during Namespace creation, to name it using the selected Tenant name as prefix, separated by a dash |
| manager.options.generateCertificates | bool | `true` | Specifies whether capsule webhooks certificates should be generated by capsule operator |
| manager.options.logLevel | string | `"4"` | Set the log verbosity of the capsule with a value from 1 to 10 |
| manager.options.nodeMetadata | object | `{"forbiddenAnnotations":{"denied":[],"deniedRegex":""},"forbiddenLabels":{"denied":[],"deniedRegex":""}}` | Allows to set the forbidden metadata for the worker nodes that could be patched by a Tenant |
| manager.options.protectedNamespaceRegex | string | `""` | If specified, disallows creation of namespaces matching the passed regexp |
| manager.rbac.create | bool | `true` | Specifies whether RBAC resources should be created. |
| manager.rbac.existingClusterRoles | list | `[]` | Specifies further cluster roles to be added to the Capsule manager service account. |
| manager.rbac.existingRoles | list | `[]` | Specifies further cluster roles to be added to the Capsule manager service account. |
| manager.readinessProbe | object | `{"httpGet":{"path":"/readyz","port":10080}}` | Configure the readiness probe using Deployment probe spec |
| manager.resources | object | `{}` | Set the resource requests/limits for the Capsule manager container |
| manager.webhookPort | int | `9443` | Set an alternative to the default container port.  Useful for use in some kubernetes clusters (such as GKE Private) with aggregator routing turned on, because pod ports have to be opened manually on the firewall side |

### ServiceMonitor Parameters

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| serviceMonitor.annotations | object | `{}` | Assign additional Annotations |
| serviceMonitor.enabled | bool | `false` | Enable ServiceMonitor |
| serviceMonitor.endpoint.interval | string | `"15s"` | Set the scrape interval for the endpoint of the serviceMonitor |
| serviceMonitor.endpoint.metricRelabelings | list | `[]` | Set metricRelabelings for the endpoint of the serviceMonitor |
| serviceMonitor.endpoint.relabelings | list | `[]` | Set relabelings for the endpoint of the serviceMonitor |
| serviceMonitor.endpoint.scrapeTimeout | string | `""` | Set the scrape timeout for the endpoint of the serviceMonitor |
| serviceMonitor.labels | object | `{}` | Assign additional labels according to Prometheus' serviceMonitorSelector matching labels |
| serviceMonitor.matchLabels | object | `{}` | Change matching labels |
| serviceMonitor.namespace | string | `""` | Install the ServiceMonitor into a different Namespace, as the monitoring stack one (default: the release one) |
| serviceMonitor.targetLabels | list | `[]` | Set targetLabels for the serviceMonitor |

### Webhooks Parameters

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| webhooks.exclusive | bool | `false` | When `crds.exclusive` is `true` the webhooks will be installed |
| webhooks.hooks.cordoning.failurePolicy | string | `"Fail"` |  |
| webhooks.hooks.cordoning.namespaceSelector.matchExpressions[0].key | string | `"capsule.clastix.io/tenant"` |  |
| webhooks.hooks.cordoning.namespaceSelector.matchExpressions[0].operator | string | `"Exists"` |  |
| webhooks.hooks.defaults.ingress.failurePolicy | string | `"Fail"` |  |
| webhooks.hooks.defaults.ingress.namespaceSelector.matchExpressions[0].key | string | `"capsule.clastix.io/tenant"` |  |
| webhooks.hooks.defaults.ingress.namespaceSelector.matchExpressions[0].operator | string | `"Exists"` |  |
| webhooks.hooks.defaults.pods.failurePolicy | string | `"Fail"` |  |
| webhooks.hooks.defaults.pods.namespaceSelector.matchExpressions[0].key | string | `"capsule.clastix.io/tenant"` |  |
| webhooks.hooks.defaults.pods.namespaceSelector.matchExpressions[0].operator | string | `"Exists"` |  |
| webhooks.hooks.defaults.pvc.failurePolicy | string | `"Fail"` |  |
| webhooks.hooks.defaults.pvc.namespaceSelector.matchExpressions[0].key | string | `"capsule.clastix.io/tenant"` |  |
| webhooks.hooks.defaults.pvc.namespaceSelector.matchExpressions[0].operator | string | `"Exists"` |  |
| webhooks.hooks.ingresses.failurePolicy | string | `"Fail"` |  |
| webhooks.hooks.ingresses.namespaceSelector.matchExpressions[0].key | string | `"capsule.clastix.io/tenant"` |  |
| webhooks.hooks.ingresses.namespaceSelector.matchExpressions[0].operator | string | `"Exists"` |  |
| webhooks.hooks.namespaceOwnerReference.failurePolicy | string | `"Fail"` |  |
| webhooks.hooks.namespaces.failurePolicy | string | `"Fail"` |  |
| webhooks.hooks.networkpolicies.failurePolicy | string | `"Fail"` |  |
| webhooks.hooks.networkpolicies.namespaceSelector.matchExpressions[0].key | string | `"capsule.clastix.io/tenant"` |  |
| webhooks.hooks.networkpolicies.namespaceSelector.matchExpressions[0].operator | string | `"Exists"` |  |
| webhooks.hooks.nodes.failurePolicy | string | `"Fail"` |  |
| webhooks.hooks.persistentvolumeclaims.failurePolicy | string | `"Fail"` |  |
| webhooks.hooks.persistentvolumeclaims.namespaceSelector.matchExpressions[0].key | string | `"capsule.clastix.io/tenant"` |  |
| webhooks.hooks.persistentvolumeclaims.namespaceSelector.matchExpressions[0].operator | string | `"Exists"` |  |
| webhooks.hooks.pods.failurePolicy | string | `"Fail"` |  |
| webhooks.hooks.pods.namespaceSelector.matchExpressions[0].key | string | `"capsule.clastix.io/tenant"` |  |
| webhooks.hooks.pods.namespaceSelector.matchExpressions[0].operator | string | `"Exists"` |  |
| webhooks.hooks.services.failurePolicy | string | `"Fail"` |  |
| webhooks.hooks.services.namespaceSelector.matchExpressions[0].key | string | `"capsule.clastix.io/tenant"` |  |
| webhooks.hooks.services.namespaceSelector.matchExpressions[0].operator | string | `"Exists"` |  |
| webhooks.hooks.tenantResourceObjects.failurePolicy | string | `"Fail"` |  |
| webhooks.hooks.tenants.failurePolicy | string | `"Fail"` |  |
| webhooks.mutatingWebhooksTimeoutSeconds | int | `30` | Timeout in seconds for mutating webhooks |
| webhooks.service.caBundle | string | `""` | CABundle for the webhook service |
| webhooks.service.name | string | `""` | Custom service name for the webhook service |
| webhooks.service.namespace | string | `""` | Custom service namespace for the webhook service |
| webhooks.service.port | string | `nil` | Custom service port for the webhook service |
| webhooks.service.url | string | `""` | The URL where the capsule webhook services are running (Overwrites cluster scoped service definition) |
| webhooks.validatingWebhooksTimeoutSeconds | int | `30` | Timeout in seconds for validating webhooks |

## Created resources

This Helm Chart creates the following Kubernetes resources in the release namespace:

* Capsule Namespace
* Capsule Operator Deployment
* Capsule Service
* CA Secret
* Certificate Secret
* Tenant Custom Resource Definition
* CapsuleConfiguration Custom Resource Definition
* MutatingWebHookConfiguration
* ValidatingWebHookConfiguration
* RBAC Cluster Roles
* Metrics Service

And optionally, depending on the values set:

* Capsule ServiceAccount
* Capsule Service Monitor
* PodSecurityPolicy
* RBAC ClusterRole and RoleBinding for pod security policy
* RBAC Role and Rolebinding for metrics scrape

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
