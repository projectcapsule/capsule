# Installation
Make sure you have access to a Kubernetes cluster as administrator.

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

# Admission Controllers
Capsule implements Kubernetes multi-tenancy capabilities using a minimum set of standard [Admission Controllers](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/) enabled on the Kubernetes APIs server.

Here the list of required Admission Controllers you have to enable to get full support from Capsule:

* PodNodeSelector
* LimitRanger
* ResourceQuota
* MutatingAdmissionWebhook
* ValidatingAdmissionWebhook

In addition to the required controllers above, Capsule implements its own set through the [Dynamic Admission Controller](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/) mechanism, providing callbacks to add further validation or resource patching:

* capsule-mutating-webhook-configuration
* capsule-validating-webhook-configuration

# Create your first Tenant
In Capsule, a _Tenant_ is an abstraction to group togheter multiple namespaces in a single entity within a set of bundaries defined by the Cluster Administrator. The tenant is then assigned to a user or group of users who are called _Tenant Owners_.

Capsule defines a Tenant as Custom Resource with cluster scope:

```yaml
cat <<EOF > oil_tenant.yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: oil
spec:
  owner:
    name: alice
    kind: User
  namespaceQuota: 3
EOF
```

Apply as cluster admin:

```
$ kubectl apply -f oil_tenant.yaml
tenant.capsule.clastix.io/oil created
```

You can check the tenant just created as cluster admin

```
$ kubectl get tenants
NAME      NAMESPACE QUOTA   NAMESPACE COUNT   OWNER NAME   OWNER KIND   NODE SELECTOR    AGE
oil       3                 0                 alice        User                          1m
```

## Tenant owners
Each tenant comes with a delegated user or group of users acting as the tenant admin. In the Capsule jargon, this is called the _Tenant Owner_. Other users can operate inside a tenant with different levels of permissions and authorizations assigned directly by the Tenant Owner.

Capsule does not care about the authentication strategy used in the cluster and all the Kubernetes methods of [authentication](https://kubernetes.io/docs/reference/access-authn-authz/authentication/) are supported. The only requirement to use Capsule is to assign tenant users to the group defined by `--capsule-user-group` option, which defaults to `capsule.clastix.io`.

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

# What’s next
The Tenant Owners have full administrative permissions limited to only the namespaces in the assigned tenant. However, their permissions can be controlled by the Cluster Admin by setting rules and policies on the assigned tenant. See [use cases]() for more cool things you can do with Capsule.