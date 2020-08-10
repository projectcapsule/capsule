# Capsule

<p align="center">
  <img src="assets/logo/space-capsule3.png" />
</p>

<p align="center">
  <img src="https://img.shields.io/github/license/clastix/capsule"/>
  <img src="https://img.shields.io/github/go-mod/go-version/clastix/capsule"/>
  <a href="https://github.com/clastix/capsule/releases">
    <img src="https://img.shields.io/github/v/release/clastix/capsule"/>
  </a>
</p>

---



# A multi-tenant operator for Kubernetes
This project provides a custom operator for implementing a strong
multi-tenant environment in _Kubernetes_. **Capsule** is not intended to be yet another _PaaS_, instead, it has been designed as a lightweight tool with a minimalist approach leveraging only the standard features of upstream Kubernetes. 

# Which is the problem to solve?
Kubernetes introduced the _namespace_ resource to create logical partitions of the
cluster. A Kubernetes namespace creates a sort of isolated *slice* in the
cluster: _Network and Security Policies_, _Resource Quota_, _Limit Ranges_, and
_RBAC_ can be used to enforce isolation among different namespaces. Namespace isolation shines when Kubernetes is used to isolate the different environments or the different types of applications. Also, it works well to isolate applications serving different users when implementing the SaaS delivery model. 

However, implementing advanced multi-tenancy scenarios, for example, a private or public _Container-as-a-Service_ platform, it becomes soon complicated because of the flat structure of Kubernetes namespaces. In such scenarios, different groups of users get assigned a pool of namespaces with a limited amount of resources (e.g.: _nodes_, _vCPU_, _RAM_, _ephemeral and persistent storage_). When users need more namespaces or move resources from one namespace to another, they always need the intervention of the cluster admin because each namespace still works as an isolated environment. To work around this, and not being overwhelmed by continuous users' requests, cluster admins often choose to create multiple smaller clusters and assign a dedicated cluster to each organization or group of users leading to the well know and painful phenomena of the _clusters sprawl_.

**Capsule** takes a different approach. It aggregates multiple namespaces assigned to an organization or group of users in a lightweight abstraction called _Tenant_. Within each tenant, users are free to create their namespaces and share all the assigned resources between the namespaces of the tenant. The _Network and Security Policies_, _Resource Quota_, _Limit Ranges_, _RBAC_, and other constraints defined at the tenant level are automatically inherited by all the namespaces in the tenant leaving the tenant's users to freely allocate resources without any intervention of the cluster administrator.

# Use cases for Capsule
Please, refer to the corresponding [section](use_cases.md) for a more detailed list of use cases that Capsule can address.

# Installation
Ensure you have `kubectl` and [`kustomize`](https://github.com/kubernetes-sigs/kustomize)
installed in your `PATH`. Also, make sure you have access to a Kubernetes cluster as an administrator.

Clone this repository and move to the repo folder:

```
~/capsule$ make deploy
# /home/prometherion/go/bin/controller-gen "crd:trivialVersions=true" rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases
# cd config/manager && /usr/local/bin/kustomize edit set image controller=quay.io/clastix/capsule:latest
# /usr/local/bin/kustomize build config/default | kubectl apply -f -
# namespace/capsule-system created
# customresourcedefinition.apiextensions.k8s.io/tenants.capsule.clastix.io created
# clusterrole.rbac.authorization.k8s.io/capsule-namespace:deleter created
# clusterrole.rbac.authorization.k8s.io/capsule-namespace:provisioner created
# clusterrole.rbac.authorization.k8s.io/capsule-proxy-role created
# clusterrole.rbac.authorization.k8s.io/capsule-metrics-reader created
# clusterrolebinding.rbac.authorization.k8s.io/capsule-manager-rolebinding created
# clusterrolebinding.rbac.authorization.k8s.io/capsule-namespace:provisioner created
# clusterrolebinding.rbac.authorization.k8s.io/capsule-proxy-rolebinding created
# secret/capsule-ca created
# secret/capsule-tls created
# service/capsule-controller-manager-metrics-service created
# service/capsule-webhook-service created
# deployment.apps/capsule-controller-manager created
# mutatingwebhookconfiguration.admissionregistration.k8s.io/capsule-mutating-webhook-configuration created
# validatingwebhookconfiguration.admissionregistration.k8s.io/capsule-validating-webhook-configuration created
```

Log verbosity of the Capsule controller can be increased by passing the `--zap-log-level` option with a value from `1` to `10` or the [basic keywords](https://godoc.org/go.uber.org/zap/zapcore#Level) although it is suggested to use the `--zap-devel` flag to get also stack traces.

## Admission Controllers
Capsule implements Kubernetes multi-tenancy capabilities using a minimum set of standard [Admission Controllers](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/) enabled on the Kubernetes APIs server: `--enable-admission-plugins=PodNodeSelector,LimitRanger,ResourceQuota`. In addition to these default controllers, Capsule implements its own set of Admission Controllers through the [Dynamic Admission Controller](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/), providing callbacks to add further validation or resource patching.

All these requests must be served via HTTPS and a CA must be provided to ensure that
the API Server is communicating with the right client. Capsule upon installation is setting its custom Certificate Authority as a client certificate as well, updating all the required resources to minimize the operational tasks.

## Tenant users
Each tenant comes with a delegated user acting as the tenant admin. In the Capsule jargon, this user is called the _Tenant Owner_. Other users can operate inside a tenant with different levels of permissions and authorizations assigned directly by the Tenant owner.

Capsule does not care about the authentication strategy used in the cluster and all the Kubernetes methods of [authentication](https://kubernetes.io/docs/reference/access-authn-authz/authentication/) are supported. The only requirement to use Capsule is to assign tenant users to the `capsule.clastix.io` group.

Assignment to a group depends on the authentication strategy in your cluster. For example, users authenticated through a _X.509_ certificate must have `capsule.clastix.io` as _Organization_: `-subj "/CN=${USER}/O=capsule.clastix.io"`

Users authenticated through an _OIDC token_ must have

```json
...
"users_groups": [
    "/capsule.clastix.io",
    "other_group"
  ]
```

in their token.

The [hack/create-user.sh](hack/create-user.sh) can help you set up a dummy `kubeconfig` for the `alice` user acting as owner of a tenant called `oil`

```bash
~/capsule$ ./hack/create-user.sh alice oil
creating certs in TMPDIR /tmp/tmp.4CLgpuime3 
Generating RSA private key, 2048 bit long modulus (2 primes)
............+++++
........................+++++
e is 65537 (0x010001)
certificatesigningrequest.certificates.k8s.io/alice-oil created
certificatesigningrequest.certificates.k8s.io/alice-oil approved
kubeconfig file is: alice-oil.kubeconfig
to use it as alice export KUBECONFIG=alice-oil.kubeconfig
```

## How to create a Tenant
Use the [scaffold Tenant](config/samples/capsule_v1alpha1_tenant.yaml)
and simply apply as Cluster Admin.

```
~/capsule$ kubectl apply -f config/samples/capsule_v1alpha1_tenant.yaml
tenant.capsule.clastix.io/oil created
```

The related Tenant owner `alice` can create Namespaces according to their assigned quota: happy Kubernetes cluster administration!

# Removal
Similar to `deploy`, you can get rid of Capsule using the `remove` target.

```
~/capsule$ make remove
# /home/prometherion/go/bin/controller-gen "crd:trivialVersions=true" rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases
# /usr/local/bin/kustomize build config/default | kubectl delete -f -
# namespace "capsule-system" deleted
# customresourcedefinition.apiextensions.k8s.io "tenants.capsule.clastix.io" deleted
# clusterrole.rbac.authorization.k8s.io "capsule-namespace:deleter" deleted
# clusterrole.rbac.authorization.k8s.io "capsule-namespace:provisioner" deleted
# clusterrole.rbac.authorization.k8s.io "capsule-proxy-role" deleted
# clusterrole.rbac.authorization.k8s.io "capsule-metrics-reader" deleted
# clusterrolebinding.rbac.authorization.k8s.io "capsule-manager-rolebinding" deleted
# clusterrolebinding.rbac.authorization.k8s.io "capsule-namespace:provisioner" deleted
# clusterrolebinding.rbac.authorization.k8s.io "capsule-proxy-rolebinding" deleted
# secret "capsule-ca" deleted
# secret "capsule-tls" deleted
# service "capsule-controller-manager-metrics-service" deleted
# service "capsule-webhook-service" deleted
# deployment.apps "capsule-controller-manager" deleted
# mutatingwebhookconfiguration.admissionregistration.k8s.io "capsule-mutating-webhook-configuration" deleted
# validatingwebhookconfiguration.admissionregistration.k8s.io "capsule-validating-webhook-configuration" deleted
```

# How to contribute
Any contribution is welcome! Please refer to the corresponding [section](contributing.md).

# Production Grade
Capsule is still in an _alpha_ stage, so **don't use it in production!**

# FAQ
tbd

# Changelog
tbd

# Roadmap
tbd