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

        $ helm install capsule capsule-helm-chart --set force_tenant_prefix=false -n capsule-system

Here the values you can override:

Parameter | Description                                                                                                                             | Default
--- |-----------------------------------------------------------------------------------------------------------------------------------------| ---
`manager.hostNetwork` | Specifies if the container should be started in `hostNetwork` mode.                                                                     | `false` 
`manager.options.logLevel` | Set the log verbosity of the controller with a value from 1 to 10.                                                                      | `4`
`manager.options.forceTenantPrefix` | Boolean, enforces the Tenant owner, during Namespace creation, to name it using the selected Tenant name as prefix, separated by a dash | `false`
`manager.options.capsuleUserGroups` | Override the Capsule user groups                                                                                                        | `[capsule.clastix.io]`
`manager.options.protectedNamespaceRegex` | If specified, disallows creation of namespaces matching the passed regexp                                                               | `null`
`manager.options.enableSecretController` | Boolean, enables apsule secret controller which reconciles TLS and CA secrets for capsule webhooks.                                     | `true`
`manager.image.repository` | Set the image repository of the controller.                                                                                             | `quay.io/clastix/capsule`
`manager.image.tag` | Overrides the image tag whose default is the chart. `appVersion`                                                                        | `null`
`manager.image.pullPolicy` | Set the image pull policy.                                                                                                              | `IfNotPresent`
`manager.livenessProbe` | Configure the liveness probe using Deployment probe spec                                                                                | `GET :10080/healthz`
`manager.readinessProbe` | Configure the readiness probe using Deployment probe spec                                                                               | `GET :10080/readyz`
`manager.resources.requests/cpu` | Set the CPU requests assigned to the controller.                                                                                        | `200m`
`manager.resources.requests/memory` | Set the memory requests assigned to the controller.                                                                                     | `128Mi`
`manager.resources.limits/cpu` | Set the CPU limits assigned to the controller.                                                                                          | `200m`
`manager.resources.limits/cpu` | Set the memory limits assigned to the controller.                                                                                       | `128Mi`
`mutatingWebhooksTimeoutSeconds` | Timeout in seconds for mutating webhooks.                                                                                               | `30`
`validatingWebhooksTimeoutSeconds` | Timeout in seconds for validating webhooks.                                                                                             | `30`
`webhooks` | Additional configuration for capsule webhooks.                                                                                          |
`imagePullSecrets` | Configuration for `imagePullSecrets` so that you can use a private images registry.                                                     | `[]`
`serviceAccount.create` | Specifies whether a service account should be created.                                                                                  | `true`
`serviceAccount.annotations` | Annotations to add to the service account.                                                                                              | `{}`
`serviceAccount.name` | The name of the service account to use. If not set and `serviceAccount.create=true`, a name is generated using the fullname template    | `capsule`
`podAnnotations` | Annotations to add to the Capsule pod.                                                                                                  | `{}`
`priorityClassName` | Set the priority class name of the Capsule pod.                                                                                         | `null`
`nodeSelector` | Set the node selector for the Capsule pod.                                                                                              | `{}`
`tolerations` | Set list of tolerations for the Capsule pod.                                                                                            | `[]`
`replicaCount` | Set the replica count for Capsule pod.                                                                                                  | `1`
`affinity` | Set affinity rules for the Capsule pod.                                                                                                 | `{}`
`podSecurityPolicy.enabled` | Specify if a Pod Security Policy must be created.                                                                                       | `false`
`serviceMonitor.enabled` | Specifies if a service monitor must be created.                                                                                         | `false`
`serviceMonitor.labels` | Additional labels which will be added to service monitor.                                                                               | `{}`
`serviceMonitor.annotations` | Additional annotations which will be added to service monitor.                                                                          | `{}`
`serviceMonitor.matchLabels` | Additional matchLabels which will be added to service monitor.                                                                          | `{}`
`serviceMonitor.serviceAccount.name` | Specifies service account name for metrics scrape.                                                                                      | `capsule`
`serviceMonitor.serviceAccount.namespace` | Specifies service account namespace for metrics scrape.                                                                                 | `capsule-system`
`customLabels` | Additional labels which will be added to all resources created by Capsule helm chart .                                                  | `{}`
`customAnnotations` | Additional annotations which will be added to all resources created by Capsule helm chart .                                             | `{}`
`certManager.generateCertificates` | Specifies whether capsule webhooks certificates should be generated using cert-manager.                                                 | `false`
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

## More

See Capsule [tutorial](https://github.com/clastix/capsule/blob/master/docs/content/general/tutorial.md) for more information about how to use Capsule.
