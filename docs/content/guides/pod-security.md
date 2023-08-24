# Pod Security
In Kubernetes, by default, workloads run with administrative access, which might be acceptable if there is only a single application running in the cluster or a single user accessing it. This is seldom required and you’ll consequently suffer a noisy neighbour effect along with large security blast radiuses.

Many of these concerns were addressed initially by [PodSecurityPolicies](https://kubernetes.io/docs/concepts/security/pod-security-policy) which have been present in the Kubernetes APIs since the very early days.

The Pod Security Policies are deprecated in Kubernetes 1.21 and removed entirely in 1.25. As replacement, the [Pod Security Standards](https://kubernetes.io/docs/concepts/security/pod-security-standards/) and [Pod Security Admission](https://kubernetes.io/docs/concepts/security/pod-security-admission/) has been introduced. Capsule support the new standard for tenants under its control as well as the oldest approach.

## Pod Security Policies
As stated in the documentation, *"PodSecurityPolicies enable fine-grained authorization of pod creation and updates. A Pod Security Policy is a cluster-level resource that controls security sensitive aspects of the pod specification. The `PodSecurityPolicy` objects define a set of conditions that a pod must run with in order to be accepted into the system, as well as defaults for the related fields."*

Using the [Pod Security Policies](https://kubernetes.io/docs/concepts/security/pod-security-policy), the cluster admin can impose limits on pod creation, for example the types of volume that can be consumed, the linux user that the process runs as in order to avoid running things as root, and more. From multi-tenancy point of view, the cluster admin has to control how users run pods in their tenants with a different level of permission on tenant basis.

Assume the Kubernetes cluster has been configured with [Pod Security Policy Admission Controller](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#podsecuritypolicy) enabled in the APIs server: `--enable-admission-plugins=PodSecurityPolicy`

The cluster admin creates a `PodSecurityPolicy`:

```yaml
kubectl apply -f - << EOF
apiVersion: policy/v1beta1
kind: PodSecurityPolicy
metadata:
  name: psp:restricted
spec:
  privileged: false
  # Required to prevent escalations to root.
  allowPrivilegeEscalation: false
EOF
```

Then create a _ClusterRole_ using or granting the said item

```yaml
kubectl apply -f - << EOF
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: psp:restricted
rules:
- apiGroups: ['policy']
  resources: ['podsecuritypolicies']
  resourceNames: ['psp:restricted']
  verbs: ['use']
EOF
```

He can assign this role to all namespaces in a tenant by setting the tenant manifest:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  additionalRoleBindings:
  - clusterRoleName: psp:privileged
    subjects:
    - kind: "Group"
      apiGroup: "rbac.authorization.k8s.io"
      name: "system:authenticated"
EOF
```

With the given specification, Capsule will ensure that all tenant namespaces will contain a _RoleBinding_ for the specified _Cluster Role_:

```yaml
kind: RoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: 'capsule-oil-psp:privileged'
  namespace: oil-production
  labels:
    capsule.clastix.io/tenant: oil
subjects:
  - kind: Group
    apiGroup: rbac.authorization.k8s.io
    name: 'system:authenticated'
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: 'psp:privileged'
```

Capsule admission controller forbids the tenant owner to run privileged pods in `oil-production` namespace and perform privilege escalation as declared by the above Cluster Role `psp:privileged`.

As tenant owner, creates a namespace:

```
kubectl --kubeconfig alice-oil.kubeconfig create ns oil-production
```

and create a pod with privileged permissions:

```yaml
kubectl --kubeconfig alice-oil.kubeconfig apply -f - << EOF
apiVersion: v1
kind: Pod
metadata:
  name: nginx
  namespace: oil-production
spec:
  containers:
  - image: nginx
    name: nginx
    ports:
    - containerPort: 80
    securityContext:
      privileged: true
EOF
```

Since the assigned `PodSecurityPolicy` explicitly disallows privileged containers, the tenant owner will see her request to be rejected by the Pod Security Policy Admission Controller.

## Pod Security Standards
One of the issues with Pod Security Policies is that it is difficult to apply restrictive permissions on a granular level, increasing security risk. Also the Pod Security Policies get applied when the request is submitted and there is no way of applying them to pods that are already running. For these, and other reasons, the Kubernetes community decided to deprecate the Pod Security Policies.

As the Pod Security Policies get deprecated and removed, the [Pod Security Standards](https://kubernetes.io/docs/concepts/security/pod-security-standards/) is used in place. It defines three different policies to broadly cover the security spectrum. These policies are cumulative and range from highly-permissive to highly-restrictive:

- **Privileged**: unrestricted policy, providing the widest possible level of permissions.
- **Baseline**: minimally restrictive policy which prevents known privilege escalations.
- **Restricted**: heavily restricted policy, following current Pod hardening best practices.

Kubernetes provides a built-in [Admission Controller](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#podsecurity) to enforce the Pod Security Standards at either:

1. cluster level which applies a standard configuration to all namespaces in a cluster
2. namespace level, one namespace at a time

For the first case, the cluster admin has to configure the Admission Controller and pass the configuration to the `kube-apiserver` by mean of the `--admission-control-config-file` extra argument, for example:

```yaml
apiVersion: apiserver.config.k8s.io/v1
kind: AdmissionConfiguration
plugins:
- name: PodSecurity
  configuration:
    apiVersion: pod-security.admission.config.k8s.io/v1beta1
    kind: PodSecurityConfiguration
    defaults:
      enforce: "baseline"
      enforce-version: "latest"
      warn: "restricted"
      warn-version: "latest"
      audit: "restricted"
      audit-version: "latest"
    exemptions:
      usernames: []
      runtimeClasses: []
      namespaces: [kube-system]
```

For the second case, he can just assign labels to the specific namespace he wants enforce the policy since the Pod Security Admission Controller is enabled by default starting from Kubernetes 1.23+:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  labels:
    pod-security.kubernetes.io/enforce: baseline
    pod-security.kubernetes.io/warn: restricted
    pod-security.kubernetes.io/audit: restricted
  name: development
```

## Pod Security Standards with Capsule
According to the regular Kubernetes segregation model, the cluster admin has to operate either at cluster level or at namespace level. Since Capsule introduces a further segregation level (the _Tenant_ abstraction), the cluster admin can implement Pod Security Standards at tenant level by simply forcing specific labels on all the namespaces created in the tenant.

As cluster admin, create a tenant with additional labels:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  namespaceOptions:
    additionalMetadata:
      labels:
        pod-security.kubernetes.io/enforce: baseline
        pod-security.kubernetes.io/audit: restricted
        pod-security.kubernetes.io/warn: restricted
  owners:
  - kind: User
    name: alice
EOF
```

All namespaces created by the tenant owner, will inherit the Pod Security labels: 

```yaml
apiVersion: v1
kind: Namespace
metadata:
  labels:
    capsule.clastix.io/tenant: oil
    kubernetes.io/metadata.name: oil-development
    name: oil-development
    pod-security.kubernetes.io/enforce: baseline
    pod-security.kubernetes.io/warn: restricted
    pod-security.kubernetes.io/audit: restricted
  name: oil-development
  ownerReferences:
  - apiVersion: capsule.clastix.io/v1beta2
    blockOwnerDeletion: true
    controller: true
    kind: Tenant
    name: oil
```

and the regular Pod Security Admission Controller does the magic:

```yaml
kubectl --kubeconfig alice-oil.kubeconfig apply -f - << EOF
apiVersion: v1
kind: Pod
metadata:
  name: nginx
  namespace: oil-production
spec:
  containers:
  - image: nginx
    name: nginx
    ports:
    - containerPort: 80
    securityContext:
      privileged: true
EOF
```

The request gets denied:

```
Error from server (Forbidden): error when creating "STDIN":
pods "nginx" is forbidden: violates PodSecurity "baseline:latest": privileged
(container "nginx" must not set securityContext.privileged=true)
```

If the tenant owner tries to change o delete the above labels, Capsule will reconcile them to the original tenant manifest set by the cluster admin.

As additional security measure, the cluster admin can also prevent the tenant owner to make an improper usage of the above labels:

```
kubectl annotate tenant oil \
  capsule.clastix.io/forbidden-namespace-labels-regexp="pod-security.kubernetes.io\/(enforce|warn|audit)"
```

In that case, the tenant owner gets denied if she tries to use the labels:

```
kubectl --kubeconfig alice-oil.kubeconfig label ns oil-production \
    pod-security.kubernetes.io/enforce=restricted \
    --overwrite

Error from server (Label pod-security.kubernetes.io/audit is forbidden for namespaces in the current Tenant ...
```