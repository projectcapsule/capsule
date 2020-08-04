# ![icon](assets/logo/space-capsule3.png) Capsule

# A Kubernetes multi-tenant operator

This project aims to provide a custom operator for implementing a strong
multi-tenant environment in _Kubernetes_, especially suited for public
_Container-as-a-Service_ (CaaS) platforms.

# tl;dr; How to install

Ensure you have [`kustomize`](https://github.com/kubernetes-sigs/kustomize)
installed in your `PATH`:

```
make deploy
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

## Webhooks and CA Bundle

Capsule is leveraging Kubernetes Multi-Tenant capabilities using the
[Dynamic Admission Controller](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/),
providing callbacks to add further validation or resource patching.

All this requests must be server via HTTPS and a CA must be provided to ensure that
the API Server is communicating with the right client.

Capsule upon installation is setting its custom Certificate Authority as
client certificate as well, updating all the required resources to minimize
the operational tasks.

## Tenant users

All Tenant owner needs to be granted with a X.509 certificate with
`capsule.clastix.io` as _Organization_.

> the [hack/create-user.sh](hack/create-user.sh) can help you setting up a
> dummy kubeconfig
>
> ```
> #. /create-user.sh alice oil
> creating certs in TMPDIR /tmp/tmp.4CLgpuime3 
> Generating RSA private key, 2048 bit long modulus (2 primes)
> ............+++++
> ........................+++++
> e is 65537 (0x010001)
> certificatesigningrequest.certificates.k8s.io/alice-oil created
> certificatesigningrequest.certificates.k8s.io/alice-oil approved
> kubeconfig file is: alice-oil.kubeconfig
> to use it as alice export KUBECONFIG=alice-oil.kubeconfig
> ```

## How to create a Tenant

Use the [scaffold Tenant](config/samples/capsule_v1alpha1_tenant.yaml)
and simply apply as Cluster Admin.

```
# kubectl apply -f config/samples/capsule_v1alpha1_tenant.yaml
tenant.capsule.clastix.io/oil created
```

The related Tenant owner can create Namespaces according to their quota:
happy Kubernetes cluster administration!

# Which is the problem to solve?

Kubernetes uses _Namespace_ resources to create logical partitions of the
cluster. A Kubernetes namespace provides the scope for some kind of resources
in the cluster. Users interacting with one namespace do not see the content in
another Namespace.

Kubernetes comes with few Namespace resources and leave the administrator to
create further namespaces in order to create sort of isolated *slices* of the
cluster: _Network and Security Policies_, _Resource Quota_, _Limit Ranges_, and
_RBAC_ are used to enforce isolation among namespaces.

Namespace isolation shines when Kubernetes is used as an enterprise container
platform, for example, to isolate the production environment from the
development and/or to isolate different types of applications.
Also it works well to isolate applications serving different users when
implementing the SaaS business model. 

When implementing a public _CaaS_ platform, the flat namespace structure in
Kubernetes shows its main limitations. In this model, each new user receives
their own namespace where to deploy workloads. The user buys a limited amount
of resources (e.g.: _vCPU_, _RAM_, _ephemeral and persistent storage_) and
cannot use more than that.
If the user needs for multiple namespaces, they can buy other namespaces.
However, resources cannot shared easily between namespaces which still work as
fully isolated environments.

_Capsule_ aggregates multiple namespaces belonging to the same user by leaving
the user to freely share resources among all their namespaces.
All the constraints, defined by _Network and Security Policies_,
_Resource Quota_, _Limit Ranges_, and RBAC can be freely shared between
namespaces in a fully self-provisioning fashion without any intervention of the
cluster admin.

# Use cases for Capsule

Please refer to the corresponding [section](use_cases.md)

# How to contribute

Please refer to the corresponding [section](contributing.md)

# Production Grade status

Capsule is still in an _alpha_ stage, so **don't use it in production**!
