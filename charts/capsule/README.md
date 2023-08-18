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

## Quick Start

The Capsule Operator Chart can be used to instantly deploy the Capsule Operator on your Kubernetes cluster.

1. Add this repository:

        $ helm repo add clastix https://clastix.github.io/charts

2. Install the Chart:

        $ helm install capsule clastix/capsule -n capsule-system --create-namespace

3. Show the status:

        $ helm status capsule -n capsule-system

4. Upgrade the Chart

        $ helm upgrade capsule clastix/capsule -n capsule-system

5. Uninstall the Chart

        $ helm uninstall capsule -n capsule-system

## Customize the installation

There are two methods for specifying overrides of values during chart installation: `--values` and `--set`.

The `--values` option is the preferred method because it allows you to keep your overrides in a YAML file, rather than specifying them all on the command line. Create a copy of the YAML file `values.yaml` and add your overrides to it.

Specify your overrides file when you install the chart:

        $ helm install capsule capsule-helm-chart --values myvalues.yaml -n capsule-system

The values in your overrides file `myvalues.yaml` will override their counterparts in the chart’s values.yaml file. Any values in `values.yaml` that weren’t overridden will keep their defaults.

If you only need to make minor customizations, you can specify them on the command line by using the `--set` option. For example:

        $ helm install capsule capsule-helm-chart --set manager.options.forceTenantPrefix=false -n capsule-system

Here the values you can override:

### General Parameters

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` | Set affinity rules for the Capsule pod |
| certManager.generateCertificates | bool | `false` | Specifies whether capsule webhooks certificates should be generated using cert-manager |
| customAnnotations | object | `{}` | Additional annotations which will be added to all resources created by Capsule helm chart |
| customLabels | object | `{}` | Additional labels which will be added to all resources created by Capsule helm chart |
| imagePullSecrets | list | `[]` | Configuration for `imagePullSecrets` so that you can use a private images registry. |
| jobs.image.pullPolicy | string | `"IfNotPresent"` | Set the image pull policy of the helm chart job |
| jobs.image.repository | string | `"clastix/kubectl"` | Set the image repository of the helm chart job |
| jobs.image.tag | string | `""` | Set the image tag of the helm chart job |
| mutatingWebhooksTimeoutSeconds | int | `30` | Timeout in seconds for mutating webhooks |
| nodeSelector | object | `{}` | Set the node selector for the Capsule pod |
| podAnnotations | object | `{}` | Annotations to add to the capsule pod. |
| podSecurityContext | object | `{"runAsGroup":1002,"runAsNonRoot":true,"runAsUser":1002,"seccompProfile":{"type":"RuntimeDefault"}}` | Set the securityContext for the Capsule pod |
| podSecurityPolicy.enabled | bool | `false` | Specify if a Pod Security Policy must be created |
| priorityClassName | string | `""` | Set the priority class name of the Capsule pod |
| replicaCount | int | `1` | Set the replica count for capsule pod |
| securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"readOnlyRootFilesystem":true}` | Set the securityContext for the Capsule container |
| serviceAccount.annotations | object | `{}` | Annotations to add to the service account. |
| serviceAccount.create | bool | `true` | Specifies whether a service account should be created. |
| serviceAccount.name | string | `"capsule"` | The name of the service account to use. If not set and `serviceAccount.create=true`, a name is generated using the fullname template |
| tls.create | bool | `true` | When cert-manager is disabled, Capsule will generate the TLS certificate for webhook and CRDs conversion. |
| tls.enableController | bool | `true` | Start the Capsule controller that injects the CA into mutating and validating webhooks, and CRD as well. |
| tls.name | string | `""` | Override name of the Capsule TLS Secret name when externally managed. |
| tolerations | list | `[]` | Set list of tolerations for the Capsule pod |
| topologySpreadConstraints | list | `[]` | Set topology spread constraints for the Capsule pod |
| validatingWebhooksTimeoutSeconds | int | `30` | Timeout in seconds for validating webhooks |

### Manager Parameters

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| manager.hostNetwork | bool | `false` | Specifies if the container should be started in hostNetwork mode.  Required for use in some managed kubernetes clusters (such as AWS EKS) with custom CNI (such as calico), because control-plane managed by AWS cannot communicate with pods' IP CIDR and admission webhooks are not working |
| manager.image.pullPolicy | string | `"IfNotPresent"` | Set the image pull policy. |
| manager.image.repository | string | `"clastix/capsule"` | Set the image repository of the capsule. |
| manager.image.tag | string | `""` | Overrides the image tag whose default is the chart appVersion. |
| manager.kind | string | `"Deployment"` | Set the controller deployment mode as `Deployment` or `DaemonSet`. |
| manager.livenessProbe | object | `{"httpGet":{"path":"/healthz","port":10080}}` | Configure the liveness probe using Deployment probe spec |
| manager.options.capsuleUserGroups | list | `["capsule.clastix.io"]` | Override the Capsule user groups |
| manager.options.forceTenantPrefix | bool | `false` | Boolean, enforces the Tenant owner, during Namespace creation, to name it using the selected Tenant name as prefix, separated by a dash |
| manager.options.generateCertificates | bool | `true` | Specifies whether capsule webhooks certificates should be generated by capsule operator |
| manager.options.logLevel | string | `"4"` | Set the log verbosity of the capsule with a value from 1 to 10 |
| manager.options.nodeMetadata | object | `{"forbiddenAnnotations":{"denied":[],"deniedRegex":""},"forbiddenLabels":{"denied":[],"deniedRegex":""}}` | Allows to set the forbidden metadata for the worker nodes that could be patched by a Tenant |
| manager.options.protectedNamespaceRegex | string | `""` | If specified, disallows creation of namespaces matching the passed regexp |
| manager.readinessProbe | object | `{"httpGet":{"path":"/readyz","port":10080}}` | Configure the readiness probe using Deployment probe spec |
| manager.resources.limits.cpu | string | `"200m"` |  |
| manager.resources.limits.memory | string | `"128Mi"` |  |
| manager.resources.requests.cpu | string | `"200m"` |  |
| manager.resources.requests.memory | string | `"128Mi"` |  |
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

### Webhook Parameters

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| webhooks.cordoning.failurePolicy | string | `"Fail"` |  |
| webhooks.cordoning.namespaceSelector.matchExpressions[0].key | string | `"capsule.clastix.io/tenant"` |  |
| webhooks.cordoning.namespaceSelector.matchExpressions[0].operator | string | `"Exists"` |  |
| webhooks.defaults.ingress.failurePolicy | string | `"Fail"` |  |
| webhooks.defaults.ingress.namespaceSelector.matchExpressions[0].key | string | `"capsule.clastix.io/tenant"` |  |
| webhooks.defaults.ingress.namespaceSelector.matchExpressions[0].operator | string | `"Exists"` |  |
| webhooks.defaults.pods.failurePolicy | string | `"Fail"` |  |
| webhooks.defaults.pods.namespaceSelector.matchExpressions[0].key | string | `"capsule.clastix.io/tenant"` |  |
| webhooks.defaults.pods.namespaceSelector.matchExpressions[0].operator | string | `"Exists"` |  |
| webhooks.defaults.pvc.failurePolicy | string | `"Fail"` |  |
| webhooks.defaults.pvc.namespaceSelector.matchExpressions[0].key | string | `"capsule.clastix.io/tenant"` |  |
| webhooks.defaults.pvc.namespaceSelector.matchExpressions[0].operator | string | `"Exists"` |  |
| webhooks.ingresses.failurePolicy | string | `"Fail"` |  |
| webhooks.ingresses.namespaceSelector.matchExpressions[0].key | string | `"capsule.clastix.io/tenant"` |  |
| webhooks.ingresses.namespaceSelector.matchExpressions[0].operator | string | `"Exists"` |  |
| webhooks.namespaceOwnerReference.failurePolicy | string | `"Fail"` |  |
| webhooks.namespaces.failurePolicy | string | `"Fail"` |  |
| webhooks.networkpolicies.failurePolicy | string | `"Fail"` |  |
| webhooks.networkpolicies.namespaceSelector.matchExpressions[0].key | string | `"capsule.clastix.io/tenant"` |  |
| webhooks.networkpolicies.namespaceSelector.matchExpressions[0].operator | string | `"Exists"` |  |
| webhooks.nodes.failurePolicy | string | `"Fail"` |  |
| webhooks.persistentvolumeclaims.failurePolicy | string | `"Fail"` |  |
| webhooks.persistentvolumeclaims.namespaceSelector.matchExpressions[0].key | string | `"capsule.clastix.io/tenant"` |  |
| webhooks.persistentvolumeclaims.namespaceSelector.matchExpressions[0].operator | string | `"Exists"` |  |
| webhooks.pods.failurePolicy | string | `"Fail"` |  |
| webhooks.pods.namespaceSelector.matchExpressions[0].key | string | `"capsule.clastix.io/tenant"` |  |
| webhooks.pods.namespaceSelector.matchExpressions[0].operator | string | `"Exists"` |  |
| webhooks.services.failurePolicy | string | `"Fail"` |  |
| webhooks.services.namespaceSelector.matchExpressions[0].key | string | `"capsule.clastix.io/tenant"` |  |
| webhooks.services.namespaceSelector.matchExpressions[0].operator | string | `"Exists"` |  |
| webhooks.tenantResourceObjects.failurePolicy | string | `"Fail"` |  |
| webhooks.tenants.failurePolicy | string | `"Fail"` |  |

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
helm upgrade --install capsule clastix/capsule --namespace capsule-system --create-namespace \
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

See Capsule [tutorial](https://github.com/clastix/capsule/blob/master/docs/content/general/tutorial.md) for more information about how to use Capsule.
