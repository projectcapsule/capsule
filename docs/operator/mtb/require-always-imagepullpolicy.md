# Require always imagePullPolicy

**Profile Applicability:** L1

**Type:** Configuration Check

**Category:** Data Isolation

**Description:** Set the image pull policy to Always for tenant workloads.

**Rationale:** Tenants have to be assured that their private images can only be used by those who have the credentials to pull them.

**Audit:**

As cluster admin, create a tenant

```yaml
kubectl create -f - << EOF
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
spec:
  imagePullPolicies:
  - Always
  owners:
  - kind: User
    name: alice
EOF

./create-user.sh alice oil
```

As tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
```

As tenant owner, creates a pod in the tenant namespace having `imagePullPolicies=IfNotPresent` 

```yaml
kubectl --kubeconfig alice apply -f - << EOF
apiVersion: v1
kind: Pod
metadata:
  name: nginx
  namespace: oil-production
spec:
  containers:
  - name: nginx
    image: nginx:latest
    imagePullPolicy: IfNotPresent
EOF
```

You should receive an error message denying the request:

```
Error from server
(ImagePullPolicy IfNotPresent for container nginx is forbidden, use one of the followings: Always): error when creating "STDIN": admission webhook "pods.capsule.clastix.io" denied the request:
ImagePullPolicy IfNotPresent for container nginx is forbidden, use one of the followings: Always
```

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
```