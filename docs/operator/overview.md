# Kubernetes multi-tenancy made simple with Capsule
**Capsule** provides a custom operator to implement multi-tenancy and policy control in your Kubernetes cluster. Ii is not intended to be yet another PaaS, instead, it has been designed as a lightweight tool with a minimalist approach leveraging only the standard features of upstream Kubernetes. 

# Which is the problem to solve?
Kubernetes introduced the _Namespace_ resource to create logical partitions of the
cluster as isolated *slices*. Namespace isolation shines when Kubernetes is used to isolate different environments or the different types of applications. Also, it works well to isolate applications serving different users when implementing the SaaS model. 

However, implementing advanced multi-tenancy scenarios, it becomes soon complicated because of the flat structure of Kubernetes namespaces. To overcome this, different groups of users or teams get assigned a dedicated cluster. As your organization grows, the number of clusters to manage and to keep aligned becomes a pain, leading to the well know phenomena of the _clusters sprawl_.

**Capsule** takes a different approach. In a single cluster, it aggregates multiple namespaces assigned to a team or group of users in a lightweight abstraction called _Tenant_. Within each tenant, users are free to create their namespaces and share all the resources in the tenant. The _Network and Security Policies_, _Resource Quota_, _Limit Ranges_, _RBAC_, and other constraints defined at the tenant level are automatically inherited by all the namespaces in the tenant. And users are free to admin their tenants in authonomy, without the intervention of the cluster administrator.

## Main Features

### **Deliver Self-Service**
Leave developers and sysadmins the freedom to self-provision their cluster resources according to their assigned boundaries.

### **Prevents Clusters Sprawl**
Share a single cluster with multiple organizations or groups of users by saving operational and management efforts.

### **Governance**
Leverage Kubernetes Admission Controllers to enforce the industry security best practices and meet legal requirements.

### **Resources Control**
Get tenants assigned with a limited amount of compute, network, and storage resources while preventing them to overtake.

### **Native Experience**
Provide multi-tenancy with a native Kubernetes experience without introducing custom resources, plugins, or additional binaries.

### **Bring your own devices**
Assign to tenants a dedicated set of compute, storage, and network resources and avoid the noisy neighbors' effect.

# What’s next
See [getting stated]() guide and play with Capsule.