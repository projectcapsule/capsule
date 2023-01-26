# Getting started

Thanks for giving Capsule a try.

## Installation

Make sure you have access to a Kubernetes cluster as administrator.

There are two ways to install Capsule:

* Use the [single YAML file installer](https://raw.githubusercontent.com/clastix/capsule/master/config/install.yaml)
* Use the [Capsule Helm Chart](https://github.com/clastix/capsule/blob/master/charts/capsule/README.md)

### Install with the single YAML file installer

Ensure you have `kubectl` installed in your `PATH`. Clone this repository and move to the repo folder:

```
$ kubectl apply -f https://raw.githubusercontent.com/clastix/capsule/master/config/install.yaml
```

It will install the Capsule controller in a dedicated namespace `capsule-system`.

### Install with Helm Chart

Please, refer to the instructions reported in the Capsule Helm Chart [README](https://github.com/clastix/capsule/blob/master/charts/capsule/README.md). 

## Create your first Tenant

In Capsule, a _Tenant_ is an abstraction to group multiple namespaces in a single entity within a set of boundaries defined by the Cluster Administrator. The tenant is then assigned to a user or group of users who is called _Tenant Owner_.

Capsule defines a Tenant as Custom Resource with cluster scope.

Create the tenant as cluster admin:

```yaml
kubectl create -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
EOF
```

You can check the tenant just created

```
$ kubectl get tenants
NAME   STATE    NAMESPACE QUOTA   NAMESPACE COUNT   NODE SELECTOR   AGE
oil    Active                     0                                 10s
```

## Login as Tenant Owner

Each tenant comes with a delegated user or group of users acting as the tenant admin. In the Capsule jargon, this is called the _Tenant Owner_. Other users can operate inside a tenant with different levels of permissions and authorizations assigned directly by the Tenant Owner.

Capsule does not care about the authentication strategy used in the cluster and all the Kubernetes methods of [authentication](https://kubernetes.io/docs/reference/access-authn-authz/authentication/) are supported. The only requirement to use Capsule is to assign tenant users to the group defined by `--capsule-user-group` option, which defaults to `capsule.clastix.io`.

Assignment to a group depends on the authentication strategy in your cluster.

For example, if you are using `capsule.clastix.io`, users authenticated through a _X.509_ certificate must have `capsule.clastix.io` as _Organization_: `-subj "/CN=${USER}/O=capsule.clastix.io"`

Users authenticated through an _OIDC token_ must have in their token:

```json
...
"users_groups": [
  "capsule.clastix.io",
  "other_group"
]
```

The [hack/create-user.sh](https://github.com/clastix/capsule/blob/master/hack/create-user.sh) can help you set up a dummy `kubeconfig` for the `alice` user acting as owner of a tenant called `oil`

```bash
./hack/create-user.sh alice oil
...
certificatesigningrequest.certificates.k8s.io/alice-oil created
certificatesigningrequest.certificates.k8s.io/alice-oil approved
kubeconfig file is: alice-oil.kubeconfig
to use it as alice export KUBECONFIG=alice-oil.kubeconfig
```

Login as tenant owner

```
$ export KUBECONFIG=alice-oil.kubeconfig
```

## Create namespaces

As tenant owner, you can create namespaces:

```
$ kubectl create namespace oil-production
$ kubectl create namespace oil-development
```

And operate with fully admin permissions:

```
$ kubectl -n oil-development run nginx --image=docker.io/nginx 
$ kubectl -n oil-development get pods
```

## Limiting access

Tenant Owners have full administrative permissions limited to only the namespaces in the assigned tenant. They can create any namespaced resource in their namespaces but they do not have access to cluster resources or resources belonging to other tenants they do not own:

```
$ kubectl -n kube-system get pods
Error from server (Forbidden): pods is forbidden:
User "alice" cannot list resource "pods" in API group "" in the namespace "kube-system"
```

See the [tutorial](/docs/general/tutorial) for getting more cool things you can do with Capsule.
