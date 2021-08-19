# Block access to cluster resources

**Profile Applicability:** L1

**Type:** Configuration Check

**Category:** Control Plane Isolation

**Description:** Tenants should not be able to view, edit, create or delete cluster (non-namespaced) resources such Node, ClusterRole, ClusterRoleBinding, etc.

**Rationale:** Access controls should be configured for tenants so that a tenant cannot list, create, modify or delete cluster resources

**Audit:**

As cluster admin, create a tenant

```yaml
kubectl create -f - <<EOF
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - kind: User
    name: alice
EOF

./create-user.sh alice oil
```

As cluster admin, run the following command to retrieve the list of non-namespaced resources
```bash 
kubectl --kubeconfig cluster-admin api-resources --namespaced=false
```
For all non-namespaced resources, and each verb (get, list, create, update, patch, watch, delete, and deletecollection) issue the following command:

```bash 
kubectl --kubeconfig alice auth can-i <verb> <resource>
```
Each command must return `no`

**Exception:**

It should, but it does not:

```bash 
kubectl --kubeconfig alice auth can-i create selfsubjectaccessreviews
yes
kubectl --kubeconfig alice auth can-i create selfsubjectrulesreviews
yes
kubectl --kubeconfig alice auth can-i create namespaces
yes
```

Any kubernetes user can create `SelfSubjectAccessReview` and `SelfSubjectRulesReviews` to checks whether he/she can act. First, two exceptions are not an issue.

```bash 
kubectl --anyuser auth can-i --list
Resources                                       Non-Resource URLs   Resource Names   Verbs
selfsubjectaccessreviews.authorization.k8s.io   []                  []               [create]
selfsubjectrulesreviews.authorization.k8s.io    []                  []               [create]
                                                [/api/*]            []               [get]
                                                [/api]              []               [get]
                                                [/apis/*]           []               [get]
                                                [/apis]             []               [get]
                                                [/healthz]          []               [get]
                                                [/healthz]          []               [get]
                                                [/livez]            []               [get]
                                                [/livez]            []               [get]
                                                [/openapi/*]        []               [get]
                                                [/openapi]          []               [get]
                                                [/readyz]           []               [get]
                                                [/readyz]           []               [get]
                                                [/version/]         []               [get]
                                                [/version/]         []               [get]
                                                [/version]          []               [get]
                                                [/version]          []               [get]
```

To enable namespace self-service provisioning, Capsule intentionally gives permissions to create namespaces to all users belonging to the Capsule group:

```bash
kubectl describe clusterrolebindings capsule-namespace-provisioner
Name:         capsule-namespace-provisioner
Labels:       <none>
Annotations:  <none>
Role:
  Kind:  ClusterRole
  Name:  capsule-namespace-provisioner
Subjects:
  Kind   Name                Namespace
  ----   ----                ---------
  Group  capsule.clastix.io  

kubectl describe clusterrole capsule-namespace-provisioner
Name:         capsule-namespace-provisioner
Labels:       <none>
Annotations:  <none>
PolicyRule:
  Resources   Non-Resource URLs  Resource Names  Verbs
  ---------   -----------------  --------------  -----
  namespaces  []                 []              [create]
```

Capsule controls self-service namespace creation by limiting the number of namespaces the user can create by the `tenant.spec.namespaceQuota option`. 

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
```