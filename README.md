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

# Kubernetes multi-tenancy made simple
This project provides a custom operator to implement multi-tenancy and policy control in your Kubernetes cluster. **Capsule** is not intended to be yet another _PaaS_, instead, it has been designed as a lightweight tool with a minimalist approach leveraging only the standard features of upstream Kubernetes. 

# Which is the problem to solve?
Kubernetes introduced the _Namespace_ resource to create logical partitions of the
cluster as isolated *slices*. Namespace isolation shines when Kubernetes is used to isolate different environments or the different types of applications. Also, it works well to isolate applications serving different users when implementing the SaaS model. 

However, implementing advanced multi-tenancy scenarios, it becomes soon complicated because of the flat structure of Kubernetes namespaces. To overcome this, different groups of users or teams get assigned a dedicated cluster. As your organization grows, the number of clusters to manage and to keep aligned becomes a pain, leading to the well know phenomena of the _clusters sprawl_.

**Capsule** takes a different approach. In a single cluster, it aggregates multiple namespaces assigned to a team or group of users in a lightweight abstraction called _Tenant_. Within each tenant, users are free to create their namespaces and share all the resources in the tenant. The _Network and Security Policies_, _Resource Quota_, _Limit Ranges_, _RBAC_, and other constraints defined at the tenant level are automatically inherited by all the namespaces in the tenant. And users are free to admin their tenants in authonomy, without the intervention of the cluster administrator.

# Use cases for Capsule
Please, refer to the corresponding [section](documentation.md) in the documentation for a more detailed list of use cases that Capsule can address.

# Installation
Make sure you have access to a Kubernetes cluster as an administrator.

There are two ways to install Capsule:

* Use the Helm Chart available [here](https://github.com/clastix/capsule-helm-chart)
* Use [`kustomize`](https://github.com/kubernetes-sigs/kustomize)

## Install with kustomize
Ensure you have `kubectl` and `kustomize` installed in your `PATH`. 

Clone this repository and move to the repo folder:

```
$ git clone https://github.com/clastix/capsule
$ cd capsule
$ make deploy
```

It will install the Capsule controller in a dedicated namespace `capsule-system`.

## Admission Controllers
Capsule implements Kubernetes multi-tenancy capabilities using a minimum set of standard [Admission Controllers](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/) enabled on the Kubernetes APIs server. See the corresponding [section](documentation.md) in the documentation for a detailed list of required Admission Controllers.

In addition to the required controllers, Capsule implements its own set through the [Dynamic Admission Controller](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/) mechanism, providing callbacks to add further validation or resource patching.

## How to create a Tenant
Use the [scaffold Tenant](config/samples/capsule_v1alpha1_tenant.yaml)
and simply apply as cluster admin.

```
$ kubectl apply -f config/samples/capsule_v1alpha1_tenant.yaml
tenant.capsule.clastix.io/oil created
```

You can check the tenant just created as

```
$ kubectl get tenants
NAME      NAMESPACE QUOTA   NAMESPACE COUNT   OWNER NAME   OWNER KIND   NODE SELECTOR    AGE
oil       3                 0                 alice        User                          1m
```

## Tenant users
Each tenant comes with a delegated user or group of users acting as the tenant admin. In the Capsule jargon, this is called the _Tenant Owner_. Other users can operate inside a tenant with different levels of permissions and authorizations assigned directly by the Tenant Owner.

Capsule does not care about the authentication strategy used in the cluster and all the Kubernetes methods of [authentication](https://kubernetes.io/docs/reference/access-authn-authz/authentication/) are supported. The only requirement to use Capsule is to assign tenant users to the the group defined by `--capsule-user-group` option, which defaults to `capsule.clastix.io`.

Assignment to a group depends on the authentication strategy in your cluster.

For example, if you are using `capsule.clastix.io`, users authenticated through a _X.509_ certificate must have `capsule.clastix.io` as _Organization_: `-subj "/CN=${USER}/O=capsule.clastix.io"`

Users authenticated through an _OIDC token_ must have

```json
...
"users_groups": [
    "capsule.clastix.io",
    "other_group"
  ]
```

in their token.

The [hack/create-user.sh](hack/create-user.sh) can help you set up a dummy `kubeconfig` for the `alice` user acting as owner of a tenant called `oil`

```bash
./hack/create-user.sh alice oil
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

Log as tenant owner

```
$ export KUBECONFIG=alice-oil.kubeconfig
```

and create a couple of new namespaces

```
$ kubectl create namespace oil-production
$ kubectl create namespace oil-development
```

As user `alice` you can operate with fully admin permissions:

```
$ kubectl -n oil-development run nginx --image=docker.io/nginx 
$ kubectl -n oil-development get pods
```

but limited to only your own namespaces:

```
$ kubectl -n kube-system get pods
Error from server (Forbidden): pods is forbidden: User "alice" cannot list resource "pods" in API group "" in the namespace "kube-system"
```

Please, check the [section](documentation.md) in the documentation for a list of more cool things you can do with Capsule.

# Removal
Similar to `deploy`, you can get rid of Capsule using the `remove` target.

```
$ make remove
```

# FAQ
- Q. How to pronunce Capsule?

  A. It should be pronounced as `/ˈkæpsjuːl/` with a bit of french accent.

- Q. Can I contribute?

  A. Absolutely! Capsule is Open Source with Apache 2 license and any contribution is welcome. Please refer to the corresponding [section](documentation.md) in the documentation.

- Q. Is it production grade?

  A. Although under frequent development and improvements, Capsule is ready to be used in production environments as currently, people are using it in public and private deployments. Check out the **Release** page for a detailed list of available versions.

- Q. Does it work with my Kuberentes XYZ distribution?

  A. We tested Capsule with vanilla Kubernetes 1.16+ on private envirnments and public clouds. We expect it works smootly on any other distribution. Please, let us know if you find it doesn't.

- Q. Do you provide commercial support?

  A. Yes, we're available to help and provide commercial support. [Clastix](https://clastix.io) is the company behind Capsule. Please, contact us for a quote. 
