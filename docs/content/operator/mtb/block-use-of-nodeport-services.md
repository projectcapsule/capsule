# Block use of NodePort services

**Profile Applicability:** L1

**Type:** Behavioral Check

**Category:** Host Isolation

**Description:** Tenants should not be able to create services of type NodePort.

**Rationale:** the service type `NodePorts` configures host ports that cannot be secured using Kubernetes network policies and require upstream firewalls. Also, multiple tenants cannot use the same host port numbers.

**Audit:**

As cluster admin, create a tenant

```yaml
kubectl create -f - << EOF
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
spec:
  enableNodePorts: false
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

As tenant owner, creates a service in the tenant namespace having service type of `NodePort` 

```yaml
kubectl --kubeconfig alice apply -f - << EOF
apiVersion: v1
kind: Service
metadata:
  name: nginx
  labels:
  namespace: oil-production
spec:
  ports:
  - protocol: TCP
    port: 8080
    targetPort: 80
  selector:
    run: nginx
  type: NodePort
EOF
```

You must receive an error message denying the request:

```
Error from server
Error from server (NodePort service types are forbidden for the tenant:
error when creating "STDIN": admission webhook "services.capsule.clastix.io" denied the request:
NodePort service types are forbidden for the tenant: please, reach out to the system administrators
```

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
```