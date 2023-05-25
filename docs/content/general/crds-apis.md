<style>
  table {
    border: solid;
    padding: 15px;
    text-align: left;
  }
  th, td {
    border-bottom: 1px solid #ddd;
    padding: 10px;
    border: solid;
  }
  tr:hover {background-color: coral;}
  sup {
    font-size: 15px;
  }
</style>

# API Reference

Packages:

- [capsule.clastix.io/v1alpha1](#capsuleclastixiov1alpha1)
- [capsule.clastix.io/v1beta2](#capsuleclastixiov1beta2)
- [capsule.clastix.io/v1beta1](#capsuleclastixiov1beta1)

# capsule.clastix.io/v1alpha1

Resource Types:

- [CapsuleConfiguration](#capsuleconfiguration)

- [Tenant](#tenant)




## CapsuleConfiguration






CapsuleConfiguration is the Schema for the Capsule configuration API.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
      <td><b>apiVersion</b></td>
      <td>string</td>
      <td>capsule.clastix.io/v1alpha1</td>
      <td>true</td>
      </tr>
      <tr>
      <td><b>kind</b></td>
      <td>string</td>
      <td>CapsuleConfiguration</td>
      <td>true</td>
      </tr>
      <tr>
      <td><b><a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#objectmeta-v1-meta">metadata</a></b></td>
      <td>object</td>
      <td>Refer to the Kubernetes API documentation for the fields of the `metadata` field.</td>
      <td>true</td>
      </tr><tr>
        <td><b><a href="#capsuleconfigurationspec">spec</a></b></td>
        <td>object</td>
        <td>
          CapsuleConfigurationSpec defines the Capsule configuration.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### CapsuleConfiguration.spec



CapsuleConfigurationSpec defines the Capsule configuration.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>forceTenantPrefix</b></td>
        <td>boolean</td>
        <td>
          Enforces the Tenant owner, during Namespace creation, to name it using the selected Tenant name as prefix, separated by a dash. This is useful to avoid Namespace name collision in a public CaaS environment.<br/>
          <br/>
            <i>Default</i>: false<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>protectedNamespaceRegex</b></td>
        <td>string</td>
        <td>
          Disallow creation of namespaces, whose name matches this regexp<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>userGroups</b></td>
        <td>[]string</td>
        <td>
          Names of the groups for Capsule users.<br/>
          <br/>
            <i>Default</i>: [capsule.clastix.io]<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>

## Tenant






Tenant is the Schema for the tenants API.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
      <td><b>apiVersion</b></td>
      <td>string</td>
      <td>capsule.clastix.io/v1alpha1</td>
      <td>true</td>
      </tr>
      <tr>
      <td><b>kind</b></td>
      <td>string</td>
      <td>Tenant</td>
      <td>true</td>
      </tr>
      <tr>
      <td><b><a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#objectmeta-v1-meta">metadata</a></b></td>
      <td>object</td>
      <td>Refer to the Kubernetes API documentation for the fields of the `metadata` field.</td>
      <td>true</td>
      </tr><tr>
        <td><b><a href="#tenantspec">spec</a></b></td>
        <td>object</td>
        <td>
          TenantSpec defines the desired state of Tenant.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantstatus">status</a></b></td>
        <td>object</td>
        <td>
          TenantStatus defines the observed state of Tenant.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec



TenantSpec defines the desired state of Tenant.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecowner">owner</a></b></td>
        <td>object</td>
        <td>
          OwnerSpec defines tenant owner name and kind.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b><a href="#tenantspecadditionalrolebindingsindex">additionalRoleBindings</a></b></td>
        <td>[]object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspeccontainerregistries">containerRegistries</a></b></td>
        <td>object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecexternalserviceips">externalServiceIPs</a></b></td>
        <td>object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecingressclasses">ingressClasses</a></b></td>
        <td>object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecingresshostnames">ingressHostnames</a></b></td>
        <td>object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspeclimitrangesindex">limitRanges</a></b></td>
        <td>[]object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>namespaceQuota</b></td>
        <td>integer</td>
        <td>
          <br/>
          <br/>
            <i>Format</i>: int32<br/>
            <i>Minimum</i>: 1<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnamespacesmetadata">namespacesMetadata</a></b></td>
        <td>object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnetworkpoliciesindex">networkPolicies</a></b></td>
        <td>[]object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>nodeSelector</b></td>
        <td>map[string]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecresourcequotasindex">resourceQuotas</a></b></td>
        <td>[]object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecservicesmetadata">servicesMetadata</a></b></td>
        <td>object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecstorageclasses">storageClasses</a></b></td>
        <td>object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.owner



OwnerSpec defines tenant owner name and kind.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>kind</b></td>
        <td>enum</td>
        <td>
          <br/>
          <br/>
            <i>Enum</i>: User, Group<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>name</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr></tbody>
</table>


### Tenant.spec.additionalRoleBindings[index]





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>clusterRoleName</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b><a href="#tenantspecadditionalrolebindingsindexsubjectsindex">subjects</a></b></td>
        <td>[]object</td>
        <td>
          kubebuilder:validation:Minimum=1<br/>
        </td>
        <td>true</td>
      </tr></tbody>
</table>


### Tenant.spec.additionalRoleBindings[index].subjects[index]



Subject contains a reference to the object or user identities a role binding applies to.  This can either hold a direct API object reference, or a value for non-objects such as user and group names.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>kind</b></td>
        <td>string</td>
        <td>
          Kind of object being referenced. Values defined by this API group are "User", "Group", and "ServiceAccount". If the Authorizer does not recognized the kind value, the Authorizer should report an error.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>name</b></td>
        <td>string</td>
        <td>
          Name of the object being referenced.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>apiGroup</b></td>
        <td>string</td>
        <td>
          APIGroup holds the API group of the referenced subject. Defaults to "" for ServiceAccount subjects. Defaults to "rbac.authorization.k8s.io" for User and Group subjects.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>namespace</b></td>
        <td>string</td>
        <td>
          Namespace of the referenced object.  If the object kind is non-namespace, such as "User" or "Group", and this value is not empty the Authorizer should report an error.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.containerRegistries





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>allowed</b></td>
        <td>[]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>allowedRegex</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.externalServiceIPs





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>allowed</b></td>
        <td>[]string</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr></tbody>
</table>


### Tenant.spec.ingressClasses





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>allowed</b></td>
        <td>[]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>allowedRegex</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.ingressHostnames





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>allowed</b></td>
        <td>[]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>allowedRegex</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.limitRanges[index]



LimitRangeSpec defines a min/max usage limit for resources that match on kind.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspeclimitrangesindexlimitsindex">limits</a></b></td>
        <td>[]object</td>
        <td>
          Limits is the list of LimitRangeItem objects that are enforced.<br/>
        </td>
        <td>true</td>
      </tr></tbody>
</table>


### Tenant.spec.limitRanges[index].limits[index]



LimitRangeItem defines a min/max usage limit for any resource that matches on kind.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>type</b></td>
        <td>string</td>
        <td>
          Type of resource that this limit applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>default</b></td>
        <td>map[string]int or string</td>
        <td>
          Default resource requirement limit value by resource name if resource limit is omitted.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>defaultRequest</b></td>
        <td>map[string]int or string</td>
        <td>
          DefaultRequest is the default resource requirement request value by resource name if resource request is omitted.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>max</b></td>
        <td>map[string]int or string</td>
        <td>
          Max usage constraints on this kind by resource name.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>maxLimitRequestRatio</b></td>
        <td>map[string]int or string</td>
        <td>
          MaxLimitRequestRatio if specified, the named resource must have a request and limit that are both non-zero where limit divided by request is less than or equal to the enumerated value; this represents the max burst for the named resource.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>min</b></td>
        <td>map[string]int or string</td>
        <td>
          Min usage constraints on this kind by resource name.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.namespacesMetadata





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>additionalAnnotations</b></td>
        <td>map[string]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>additionalLabels</b></td>
        <td>map[string]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies[index]



NetworkPolicySpec provides the specification of a NetworkPolicy

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesindexpodselector">podSelector</a></b></td>
        <td>object</td>
        <td>
          podSelector selects the pods to which this NetworkPolicy object applies. The array of ingress rules is applied to any pods selected by this field. Multiple network policies can select the same set of pods. In this case, the ingress rules for each are combined additively. This field is NOT optional and follows standard label selector semantics. An empty podSelector matches all pods in this namespace.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b><a href="#tenantspecnetworkpoliciesindexegressindex">egress</a></b></td>
        <td>[]object</td>
        <td>
          egress is a list of egress rules to be applied to the selected pods. Outgoing traffic is allowed if there are no NetworkPolicies selecting the pod (and cluster policy otherwise allows the traffic), OR if the traffic matches at least one egress rule across all of the NetworkPolicy objects whose podSelector matches the pod. If this field is empty then this NetworkPolicy limits all outgoing traffic (and serves solely to ensure that the pods it selects are isolated by default). This field is beta-level in 1.8<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnetworkpoliciesindexingressindex">ingress</a></b></td>
        <td>[]object</td>
        <td>
          ingress is a list of ingress rules to be applied to the selected pods. Traffic is allowed to a pod if there are no NetworkPolicies selecting the pod (and cluster policy otherwise allows the traffic), OR if the traffic source is the pod's local node, OR if the traffic matches at least one ingress rule across all of the NetworkPolicy objects whose podSelector matches the pod. If this field is empty then this NetworkPolicy does not allow any traffic (and serves solely to ensure that the pods it selects are isolated by default)<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>policyTypes</b></td>
        <td>[]string</td>
        <td>
          policyTypes is a list of rule types that the NetworkPolicy relates to. Valid options are ["Ingress"], ["Egress"], or ["Ingress", "Egress"]. If this field is not specified, it will default based on the existence of ingress or egress rules; policies that contain an egress section are assumed to affect egress, and all policies (whether or not they contain an ingress section) are assumed to affect ingress. If you want to write an egress-only policy, you must explicitly specify policyTypes [ "Egress" ]. Likewise, if you want to write a policy that specifies that no egress is allowed, you must specify a policyTypes value that include "Egress" (since such a policy would not include an egress section and would otherwise default to just [ "Ingress" ]). This field is beta-level in 1.8<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies[index].podSelector



podSelector selects the pods to which this NetworkPolicy object applies. The array of ingress rules is applied to any pods selected by this field. Multiple network policies can select the same set of pods. In this case, the ingress rules for each are combined additively. This field is NOT optional and follows standard label selector semantics. An empty podSelector matches all pods in this namespace.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesindexpodselectormatchexpressionsindex">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          matchExpressions is a list of label selector requirements. The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>matchLabels</b></td>
        <td>map[string]string</td>
        <td>
          matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies[index].podSelector.matchExpressions[index]



A label selector requirement is a selector that contains values, a key, and an operator that relates the key and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>key</b></td>
        <td>string</td>
        <td>
          key is the label key that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          operator represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists and DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          values is an array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies[index].egress[index]



NetworkPolicyEgressRule describes a particular set of traffic that is allowed out of pods matched by a NetworkPolicySpec's podSelector. The traffic must match both ports and to. This type is beta-level in 1.8

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesindexegressindexportsindex">ports</a></b></td>
        <td>[]object</td>
        <td>
          ports is a list of destination ports for outgoing traffic. Each item in this list is combined using a logical OR. If this field is empty or missing, this rule matches all ports (traffic not restricted by port). If this field is present and contains at least one item, then this rule allows traffic only if the traffic matches at least one port in the list.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnetworkpoliciesindexegressindextoindex">to</a></b></td>
        <td>[]object</td>
        <td>
          to is a list of destinations for outgoing traffic of pods selected for this rule. Items in this list are combined using a logical OR operation. If this field is empty or missing, this rule matches all destinations (traffic not restricted by destination). If this field is present and contains at least one item, this rule allows traffic only if the traffic matches at least one item in the to list.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies[index].egress[index].ports[index]



NetworkPolicyPort describes a port to allow traffic on

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>endPort</b></td>
        <td>integer</td>
        <td>
          endPort indicates that the range of ports from port to endPort if set, inclusive, should be allowed by the policy. This field cannot be defined if the port field is not defined or if the port field is defined as a named (string) port. The endPort must be equal or greater than port.<br/>
          <br/>
            <i>Format</i>: int32<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>port</b></td>
        <td>int or string</td>
        <td>
          port represents the port on the given protocol. This can either be a numerical or named port on a pod. If this field is not provided, this matches all port names and numbers. If present, only traffic on the specified protocol AND port will be matched.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>protocol</b></td>
        <td>string</td>
        <td>
          protocol represents the protocol (TCP, UDP, or SCTP) which traffic must match. If not specified, this field defaults to TCP.<br/>
          <br/>
            <i>Default</i>: TCP<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies[index].egress[index].to[index]



NetworkPolicyPeer describes a peer to allow traffic to/from. Only certain combinations of fields are allowed

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesindexegressindextoindexipblock">ipBlock</a></b></td>
        <td>object</td>
        <td>
          ipBlock defines policy on a particular IPBlock. If this field is set then neither of the other fields can be.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnetworkpoliciesindexegressindextoindexnamespaceselector">namespaceSelector</a></b></td>
        <td>object</td>
        <td>
          namespaceSelector selects namespaces using cluster-scoped labels. This field follows standard label selector semantics; if present but empty, it selects all namespaces. 
 If podSelector is also set, then the NetworkPolicyPeer as a whole selects the pods matching podSelector in the namespaces selected by namespaceSelector. Otherwise it selects all pods in the namespaces selected by namespaceSelector.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnetworkpoliciesindexegressindextoindexpodselector">podSelector</a></b></td>
        <td>object</td>
        <td>
          podSelector is a label selector which selects pods. This field follows standard label selector semantics; if present but empty, it selects all pods. 
 If namespaceSelector is also set, then the NetworkPolicyPeer as a whole selects the pods matching podSelector in the Namespaces selected by NamespaceSelector. Otherwise it selects the pods matching podSelector in the policy's own namespace.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies[index].egress[index].to[index].ipBlock



ipBlock defines policy on a particular IPBlock. If this field is set then neither of the other fields can be.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>cidr</b></td>
        <td>string</td>
        <td>
          cidr is a string representing the IPBlock Valid examples are "192.168.1.0/24" or "2001:db8::/64"<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>except</b></td>
        <td>[]string</td>
        <td>
          except is a slice of CIDRs that should not be included within an IPBlock Valid examples are "192.168.1.0/24" or "2001:db8::/64" Except values will be rejected if they are outside the cidr range<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies[index].egress[index].to[index].namespaceSelector



namespaceSelector selects namespaces using cluster-scoped labels. This field follows standard label selector semantics; if present but empty, it selects all namespaces. 
 If podSelector is also set, then the NetworkPolicyPeer as a whole selects the pods matching podSelector in the namespaces selected by namespaceSelector. Otherwise it selects all pods in the namespaces selected by namespaceSelector.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesindexegressindextoindexnamespaceselectormatchexpressionsindex">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          matchExpressions is a list of label selector requirements. The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>matchLabels</b></td>
        <td>map[string]string</td>
        <td>
          matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies[index].egress[index].to[index].namespaceSelector.matchExpressions[index]



A label selector requirement is a selector that contains values, a key, and an operator that relates the key and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>key</b></td>
        <td>string</td>
        <td>
          key is the label key that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          operator represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists and DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          values is an array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies[index].egress[index].to[index].podSelector



podSelector is a label selector which selects pods. This field follows standard label selector semantics; if present but empty, it selects all pods. 
 If namespaceSelector is also set, then the NetworkPolicyPeer as a whole selects the pods matching podSelector in the Namespaces selected by NamespaceSelector. Otherwise it selects the pods matching podSelector in the policy's own namespace.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesindexegressindextoindexpodselectormatchexpressionsindex">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          matchExpressions is a list of label selector requirements. The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>matchLabels</b></td>
        <td>map[string]string</td>
        <td>
          matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies[index].egress[index].to[index].podSelector.matchExpressions[index]



A label selector requirement is a selector that contains values, a key, and an operator that relates the key and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>key</b></td>
        <td>string</td>
        <td>
          key is the label key that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          operator represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists and DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          values is an array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies[index].ingress[index]



NetworkPolicyIngressRule describes a particular set of traffic that is allowed to the pods matched by a NetworkPolicySpec's podSelector. The traffic must match both ports and from.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesindexingressindexfromindex">from</a></b></td>
        <td>[]object</td>
        <td>
          from is a list of sources which should be able to access the pods selected for this rule. Items in this list are combined using a logical OR operation. If this field is empty or missing, this rule matches all sources (traffic not restricted by source). If this field is present and contains at least one item, this rule allows traffic only if the traffic matches at least one item in the from list.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnetworkpoliciesindexingressindexportsindex">ports</a></b></td>
        <td>[]object</td>
        <td>
          ports is a list of ports which should be made accessible on the pods selected for this rule. Each item in this list is combined using a logical OR. If this field is empty or missing, this rule matches all ports (traffic not restricted by port). If this field is present and contains at least one item, then this rule allows traffic only if the traffic matches at least one port in the list.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies[index].ingress[index].from[index]



NetworkPolicyPeer describes a peer to allow traffic to/from. Only certain combinations of fields are allowed

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesindexingressindexfromindexipblock">ipBlock</a></b></td>
        <td>object</td>
        <td>
          ipBlock defines policy on a particular IPBlock. If this field is set then neither of the other fields can be.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnetworkpoliciesindexingressindexfromindexnamespaceselector">namespaceSelector</a></b></td>
        <td>object</td>
        <td>
          namespaceSelector selects namespaces using cluster-scoped labels. This field follows standard label selector semantics; if present but empty, it selects all namespaces. 
 If podSelector is also set, then the NetworkPolicyPeer as a whole selects the pods matching podSelector in the namespaces selected by namespaceSelector. Otherwise it selects all pods in the namespaces selected by namespaceSelector.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnetworkpoliciesindexingressindexfromindexpodselector">podSelector</a></b></td>
        <td>object</td>
        <td>
          podSelector is a label selector which selects pods. This field follows standard label selector semantics; if present but empty, it selects all pods. 
 If namespaceSelector is also set, then the NetworkPolicyPeer as a whole selects the pods matching podSelector in the Namespaces selected by NamespaceSelector. Otherwise it selects the pods matching podSelector in the policy's own namespace.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies[index].ingress[index].from[index].ipBlock



ipBlock defines policy on a particular IPBlock. If this field is set then neither of the other fields can be.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>cidr</b></td>
        <td>string</td>
        <td>
          cidr is a string representing the IPBlock Valid examples are "192.168.1.0/24" or "2001:db8::/64"<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>except</b></td>
        <td>[]string</td>
        <td>
          except is a slice of CIDRs that should not be included within an IPBlock Valid examples are "192.168.1.0/24" or "2001:db8::/64" Except values will be rejected if they are outside the cidr range<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies[index].ingress[index].from[index].namespaceSelector



namespaceSelector selects namespaces using cluster-scoped labels. This field follows standard label selector semantics; if present but empty, it selects all namespaces. 
 If podSelector is also set, then the NetworkPolicyPeer as a whole selects the pods matching podSelector in the namespaces selected by namespaceSelector. Otherwise it selects all pods in the namespaces selected by namespaceSelector.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesindexingressindexfromindexnamespaceselectormatchexpressionsindex">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          matchExpressions is a list of label selector requirements. The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>matchLabels</b></td>
        <td>map[string]string</td>
        <td>
          matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies[index].ingress[index].from[index].namespaceSelector.matchExpressions[index]



A label selector requirement is a selector that contains values, a key, and an operator that relates the key and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>key</b></td>
        <td>string</td>
        <td>
          key is the label key that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          operator represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists and DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          values is an array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies[index].ingress[index].from[index].podSelector



podSelector is a label selector which selects pods. This field follows standard label selector semantics; if present but empty, it selects all pods. 
 If namespaceSelector is also set, then the NetworkPolicyPeer as a whole selects the pods matching podSelector in the Namespaces selected by NamespaceSelector. Otherwise it selects the pods matching podSelector in the policy's own namespace.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesindexingressindexfromindexpodselectormatchexpressionsindex">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          matchExpressions is a list of label selector requirements. The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>matchLabels</b></td>
        <td>map[string]string</td>
        <td>
          matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies[index].ingress[index].from[index].podSelector.matchExpressions[index]



A label selector requirement is a selector that contains values, a key, and an operator that relates the key and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>key</b></td>
        <td>string</td>
        <td>
          key is the label key that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          operator represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists and DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          values is an array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies[index].ingress[index].ports[index]



NetworkPolicyPort describes a port to allow traffic on

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>endPort</b></td>
        <td>integer</td>
        <td>
          endPort indicates that the range of ports from port to endPort if set, inclusive, should be allowed by the policy. This field cannot be defined if the port field is not defined or if the port field is defined as a named (string) port. The endPort must be equal or greater than port.<br/>
          <br/>
            <i>Format</i>: int32<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>port</b></td>
        <td>int or string</td>
        <td>
          port represents the port on the given protocol. This can either be a numerical or named port on a pod. If this field is not provided, this matches all port names and numbers. If present, only traffic on the specified protocol AND port will be matched.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>protocol</b></td>
        <td>string</td>
        <td>
          protocol represents the protocol (TCP, UDP, or SCTP) which traffic must match. If not specified, this field defaults to TCP.<br/>
          <br/>
            <i>Default</i>: TCP<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.resourceQuotas[index]



ResourceQuotaSpec defines the desired hard limits to enforce for Quota.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>hard</b></td>
        <td>map[string]int or string</td>
        <td>
          hard is the set of desired hard limits for each named resource. More info: https://kubernetes.io/docs/concepts/policy/resource-quotas/<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecresourcequotasindexscopeselector">scopeSelector</a></b></td>
        <td>object</td>
        <td>
          scopeSelector is also a collection of filters like scopes that must match each object tracked by a quota but expressed using ScopeSelectorOperator in combination with possible values. For a resource to match, both scopes AND scopeSelector (if specified in spec), must be matched.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>scopes</b></td>
        <td>[]string</td>
        <td>
          A collection of filters that must match each object tracked by a quota. If not specified, the quota matches all objects.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.resourceQuotas[index].scopeSelector



scopeSelector is also a collection of filters like scopes that must match each object tracked by a quota but expressed using ScopeSelectorOperator in combination with possible values. For a resource to match, both scopes AND scopeSelector (if specified in spec), must be matched.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecresourcequotasindexscopeselectormatchexpressionsindex">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          A list of scope selector requirements by scope of the resources.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.resourceQuotas[index].scopeSelector.matchExpressions[index]



A scoped-resource selector requirement is a selector that contains values, a scope name, and an operator that relates the scope name and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          Represents a scope's relationship to a set of values. Valid operators are In, NotIn, Exists, DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>scopeName</b></td>
        <td>string</td>
        <td>
          The name of the scope that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          An array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.servicesMetadata





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>additionalAnnotations</b></td>
        <td>map[string]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>additionalLabels</b></td>
        <td>map[string]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.storageClasses





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>allowed</b></td>
        <td>[]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>allowedRegex</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.status



TenantStatus defines the observed state of Tenant.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>size</b></td>
        <td>integer</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>namespaces</b></td>
        <td>[]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>

# capsule.clastix.io/v1beta2

Resource Types:

- [CapsuleConfiguration](#capsuleconfiguration)

- [GlobalTenantResource](#globaltenantresource)

- [TenantResource](#tenantresource)

- [Tenant](#tenant)




## CapsuleConfiguration






CapsuleConfiguration is the Schema for the Capsule configuration API.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
      <td><b>apiVersion</b></td>
      <td>string</td>
      <td>capsule.clastix.io/v1beta2</td>
      <td>true</td>
      </tr>
      <tr>
      <td><b>kind</b></td>
      <td>string</td>
      <td>CapsuleConfiguration</td>
      <td>true</td>
      </tr>
      <tr>
      <td><b><a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#objectmeta-v1-meta">metadata</a></b></td>
      <td>object</td>
      <td>Refer to the Kubernetes API documentation for the fields of the `metadata` field.</td>
      <td>true</td>
      </tr><tr>
        <td><b><a href="#capsuleconfigurationspec-1">spec</a></b></td>
        <td>object</td>
        <td>
          CapsuleConfigurationSpec defines the Capsule configuration.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### CapsuleConfiguration.spec



CapsuleConfigurationSpec defines the Capsule configuration.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>enableTLSReconciler</b></td>
        <td>boolean</td>
        <td>
          Toggles the TLS reconciler, the controller that is able to generate CA and certificates for the webhooks when not using an already provided CA and certificate, or when these are managed externally with Vault, or cert-manager.<br/>
          <br/>
            <i>Default</i>: true<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>forceTenantPrefix</b></td>
        <td>boolean</td>
        <td>
          Enforces the Tenant owner, during Namespace creation, to name it using the selected Tenant name as prefix, separated by a dash. This is useful to avoid Namespace name collision in a public CaaS environment.<br/>
          <br/>
            <i>Default</i>: false<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#capsuleconfigurationspecnodemetadata">nodeMetadata</a></b></td>
        <td>object</td>
        <td>
          Allows to set the forbidden metadata for the worker nodes that could be patched by a Tenant. This applies only if the Tenant has an active NodeSelector, and the Owner have right to patch their nodes.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#capsuleconfigurationspecoverrides">overrides</a></b></td>
        <td>object</td>
        <td>
          Allows to set different name rather than the canonical one for the Capsule configuration objects, such as webhook secret or configurations.<br/>
          <br/>
            <i>Default</i>: map[TLSSecretName:capsule-tls mutatingWebhookConfigurationName:capsule-mutating-webhook-configuration validatingWebhookConfigurationName:capsule-validating-webhook-configuration]<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>protectedNamespaceRegex</b></td>
        <td>string</td>
        <td>
          Disallow creation of namespaces, whose name matches this regexp<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>userGroups</b></td>
        <td>[]string</td>
        <td>
          Names of the groups for Capsule users.<br/>
          <br/>
            <i>Default</i>: [capsule.clastix.io]<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### CapsuleConfiguration.spec.nodeMetadata



Allows to set the forbidden metadata for the worker nodes that could be patched by a Tenant. This applies only if the Tenant has an active NodeSelector, and the Owner have right to patch their nodes.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#capsuleconfigurationspecnodemetadataforbiddenannotations">forbiddenAnnotations</a></b></td>
        <td>object</td>
        <td>
          Define the annotations that a Tenant Owner cannot set for their nodes.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b><a href="#capsuleconfigurationspecnodemetadataforbiddenlabels">forbiddenLabels</a></b></td>
        <td>object</td>
        <td>
          Define the labels that a Tenant Owner cannot set for their nodes.<br/>
        </td>
        <td>true</td>
      </tr></tbody>
</table>


### CapsuleConfiguration.spec.nodeMetadata.forbiddenAnnotations



Define the annotations that a Tenant Owner cannot set for their nodes.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>denied</b></td>
        <td>[]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>deniedRegex</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### CapsuleConfiguration.spec.nodeMetadata.forbiddenLabels



Define the labels that a Tenant Owner cannot set for their nodes.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>denied</b></td>
        <td>[]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>deniedRegex</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### CapsuleConfiguration.spec.overrides



Allows to set different name rather than the canonical one for the Capsule configuration objects, such as webhook secret or configurations.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>TLSSecretName</b></td>
        <td>string</td>
        <td>
          Defines the Secret name used for the webhook server. Must be in the same Namespace where the Capsule Deployment is deployed.<br/>
          <br/>
            <i>Default</i>: capsule-tls<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>mutatingWebhookConfigurationName</b></td>
        <td>string</td>
        <td>
          Name of the MutatingWebhookConfiguration which contains the dynamic admission controller paths and resources.<br/>
          <br/>
            <i>Default</i>: capsule-mutating-webhook-configuration<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>validatingWebhookConfigurationName</b></td>
        <td>string</td>
        <td>
          Name of the ValidatingWebhookConfiguration which contains the dynamic admission controller paths and resources.<br/>
          <br/>
            <i>Default</i>: capsule-validating-webhook-configuration<br/>
        </td>
        <td>true</td>
      </tr></tbody>
</table>

## GlobalTenantResource






GlobalTenantResource allows to propagate resource replications to a specific subset of Tenant resources.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
      <td><b>apiVersion</b></td>
      <td>string</td>
      <td>capsule.clastix.io/v1beta2</td>
      <td>true</td>
      </tr>
      <tr>
      <td><b>kind</b></td>
      <td>string</td>
      <td>GlobalTenantResource</td>
      <td>true</td>
      </tr>
      <tr>
      <td><b><a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#objectmeta-v1-meta">metadata</a></b></td>
      <td>object</td>
      <td>Refer to the Kubernetes API documentation for the fields of the `metadata` field.</td>
      <td>true</td>
      </tr><tr>
        <td><b><a href="#globaltenantresourcespec">spec</a></b></td>
        <td>object</td>
        <td>
          GlobalTenantResourceSpec defines the desired state of GlobalTenantResource.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#globaltenantresourcestatus">status</a></b></td>
        <td>object</td>
        <td>
          GlobalTenantResourceStatus defines the observed state of GlobalTenantResource.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### GlobalTenantResource.spec



GlobalTenantResourceSpec defines the desired state of GlobalTenantResource.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#globaltenantresourcespecresourcesindex">resources</a></b></td>
        <td>[]object</td>
        <td>
          Defines the rules to select targeting Namespace, along with the objects that must be replicated.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>resyncPeriod</b></td>
        <td>string</td>
        <td>
          Define the period of time upon a second reconciliation must be invoked. Keep in mind that any change to the manifests will trigger a new reconciliation.<br/>
          <br/>
            <i>Default</i>: 60s<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>pruningOnDelete</b></td>
        <td>boolean</td>
        <td>
          When the replicated resource manifest is deleted, all the objects replicated so far will be automatically deleted. Disable this to keep replicated resources although the deletion of the replication manifest.<br/>
          <br/>
            <i>Default</i>: true<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#globaltenantresourcespectenantselector">tenantSelector</a></b></td>
        <td>object</td>
        <td>
          Defines the Tenant selector used target the tenants on which resources must be propagated.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### GlobalTenantResource.spec.resources[index]





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#globaltenantresourcespecresourcesindexadditionalmetadata">additionalMetadata</a></b></td>
        <td>object</td>
        <td>
          Besides the Capsule metadata required by TenantResource controller, defines additional metadata that must be added to the replicated resources.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#globaltenantresourcespecresourcesindexnamespaceselector">namespaceSelector</a></b></td>
        <td>object</td>
        <td>
          Defines the Namespace selector to select the Tenant Namespaces on which the resources must be propagated. In case of nil value, all the Tenant Namespaces are targeted.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#globaltenantresourcespecresourcesindexnamespaceditemsindex">namespacedItems</a></b></td>
        <td>[]object</td>
        <td>
          List of the resources already existing in other Namespaces that must be replicated.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>rawItems</b></td>
        <td>[]RawExtension</td>
        <td>
          List of raw resources that must be replicated.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### GlobalTenantResource.spec.resources[index].additionalMetadata



Besides the Capsule metadata required by TenantResource controller, defines additional metadata that must be added to the replicated resources.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>annotations</b></td>
        <td>map[string]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>labels</b></td>
        <td>map[string]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### GlobalTenantResource.spec.resources[index].namespaceSelector



Defines the Namespace selector to select the Tenant Namespaces on which the resources must be propagated. In case of nil value, all the Tenant Namespaces are targeted.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#globaltenantresourcespecresourcesindexnamespaceselectormatchexpressionsindex">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          matchExpressions is a list of label selector requirements. The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>matchLabels</b></td>
        <td>map[string]string</td>
        <td>
          matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### GlobalTenantResource.spec.resources[index].namespaceSelector.matchExpressions[index]



A label selector requirement is a selector that contains values, a key, and an operator that relates the key and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>key</b></td>
        <td>string</td>
        <td>
          key is the label key that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          operator represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists and DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          values is an array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### GlobalTenantResource.spec.resources[index].namespacedItems[index]





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>kind</b></td>
        <td>string</td>
        <td>
          Kind of the referent. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>namespace</b></td>
        <td>string</td>
        <td>
          Namespace of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b><a href="#globaltenantresourcespecresourcesindexnamespaceditemsindexselector">selector</a></b></td>
        <td>object</td>
        <td>
          Label selector used to select the given resources in the given Namespace.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>apiVersion</b></td>
        <td>string</td>
        <td>
          API version of the referent.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### GlobalTenantResource.spec.resources[index].namespacedItems[index].selector



Label selector used to select the given resources in the given Namespace.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#globaltenantresourcespecresourcesindexnamespaceditemsindexselectormatchexpressionsindex">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          matchExpressions is a list of label selector requirements. The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>matchLabels</b></td>
        <td>map[string]string</td>
        <td>
          matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### GlobalTenantResource.spec.resources[index].namespacedItems[index].selector.matchExpressions[index]



A label selector requirement is a selector that contains values, a key, and an operator that relates the key and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>key</b></td>
        <td>string</td>
        <td>
          key is the label key that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          operator represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists and DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          values is an array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### GlobalTenantResource.spec.tenantSelector



Defines the Tenant selector used target the tenants on which resources must be propagated.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#globaltenantresourcespectenantselectormatchexpressionsindex">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          matchExpressions is a list of label selector requirements. The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>matchLabels</b></td>
        <td>map[string]string</td>
        <td>
          matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### GlobalTenantResource.spec.tenantSelector.matchExpressions[index]



A label selector requirement is a selector that contains values, a key, and an operator that relates the key and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>key</b></td>
        <td>string</td>
        <td>
          key is the label key that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          operator represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists and DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          values is an array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### GlobalTenantResource.status



GlobalTenantResourceStatus defines the observed state of GlobalTenantResource.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#globaltenantresourcestatusprocesseditemsindex">processedItems</a></b></td>
        <td>[]object</td>
        <td>
          List of the replicated resources for the given TenantResource.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>selectedTenants</b></td>
        <td>[]string</td>
        <td>
          List of Tenants addressed by the GlobalTenantResource.<br/>
        </td>
        <td>true</td>
      </tr></tbody>
</table>


### GlobalTenantResource.status.processedItems[index]





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>kind</b></td>
        <td>string</td>
        <td>
          Kind of the referent. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>name</b></td>
        <td>string</td>
        <td>
          Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>namespace</b></td>
        <td>string</td>
        <td>
          Namespace of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>apiVersion</b></td>
        <td>string</td>
        <td>
          API version of the referent.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>

## TenantResource






TenantResource allows a Tenant Owner, if enabled with proper RBAC, to propagate resources in its Namespace. The object must be deployed in a Tenant Namespace, and cannot reference object living in non-Tenant namespaces. For such cases, the GlobalTenantResource must be used.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
      <td><b>apiVersion</b></td>
      <td>string</td>
      <td>capsule.clastix.io/v1beta2</td>
      <td>true</td>
      </tr>
      <tr>
      <td><b>kind</b></td>
      <td>string</td>
      <td>TenantResource</td>
      <td>true</td>
      </tr>
      <tr>
      <td><b><a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#objectmeta-v1-meta">metadata</a></b></td>
      <td>object</td>
      <td>Refer to the Kubernetes API documentation for the fields of the `metadata` field.</td>
      <td>true</td>
      </tr><tr>
        <td><b><a href="#tenantresourcespec">spec</a></b></td>
        <td>object</td>
        <td>
          TenantResourceSpec defines the desired state of TenantResource.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantresourcestatus">status</a></b></td>
        <td>object</td>
        <td>
          TenantResourceStatus defines the observed state of TenantResource.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### TenantResource.spec



TenantResourceSpec defines the desired state of TenantResource.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantresourcespecresourcesindex">resources</a></b></td>
        <td>[]object</td>
        <td>
          Defines the rules to select targeting Namespace, along with the objects that must be replicated.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>resyncPeriod</b></td>
        <td>string</td>
        <td>
          Define the period of time upon a second reconciliation must be invoked. Keep in mind that any change to the manifests will trigger a new reconciliation.<br/>
          <br/>
            <i>Default</i>: 60s<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>pruningOnDelete</b></td>
        <td>boolean</td>
        <td>
          When the replicated resource manifest is deleted, all the objects replicated so far will be automatically deleted. Disable this to keep replicated resources although the deletion of the replication manifest.<br/>
          <br/>
            <i>Default</i>: true<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### TenantResource.spec.resources[index]





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantresourcespecresourcesindexadditionalmetadata">additionalMetadata</a></b></td>
        <td>object</td>
        <td>
          Besides the Capsule metadata required by TenantResource controller, defines additional metadata that must be added to the replicated resources.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantresourcespecresourcesindexnamespaceselector">namespaceSelector</a></b></td>
        <td>object</td>
        <td>
          Defines the Namespace selector to select the Tenant Namespaces on which the resources must be propagated. In case of nil value, all the Tenant Namespaces are targeted.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantresourcespecresourcesindexnamespaceditemsindex">namespacedItems</a></b></td>
        <td>[]object</td>
        <td>
          List of the resources already existing in other Namespaces that must be replicated.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>rawItems</b></td>
        <td>[]RawExtension</td>
        <td>
          List of raw resources that must be replicated.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### TenantResource.spec.resources[index].additionalMetadata



Besides the Capsule metadata required by TenantResource controller, defines additional metadata that must be added to the replicated resources.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>annotations</b></td>
        <td>map[string]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>labels</b></td>
        <td>map[string]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### TenantResource.spec.resources[index].namespaceSelector



Defines the Namespace selector to select the Tenant Namespaces on which the resources must be propagated. In case of nil value, all the Tenant Namespaces are targeted.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantresourcespecresourcesindexnamespaceselectormatchexpressionsindex">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          matchExpressions is a list of label selector requirements. The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>matchLabels</b></td>
        <td>map[string]string</td>
        <td>
          matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### TenantResource.spec.resources[index].namespaceSelector.matchExpressions[index]



A label selector requirement is a selector that contains values, a key, and an operator that relates the key and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>key</b></td>
        <td>string</td>
        <td>
          key is the label key that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          operator represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists and DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          values is an array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### TenantResource.spec.resources[index].namespacedItems[index]





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>kind</b></td>
        <td>string</td>
        <td>
          Kind of the referent. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>namespace</b></td>
        <td>string</td>
        <td>
          Namespace of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b><a href="#tenantresourcespecresourcesindexnamespaceditemsindexselector">selector</a></b></td>
        <td>object</td>
        <td>
          Label selector used to select the given resources in the given Namespace.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>apiVersion</b></td>
        <td>string</td>
        <td>
          API version of the referent.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### TenantResource.spec.resources[index].namespacedItems[index].selector



Label selector used to select the given resources in the given Namespace.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantresourcespecresourcesindexnamespaceditemsindexselectormatchexpressionsindex">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          matchExpressions is a list of label selector requirements. The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>matchLabels</b></td>
        <td>map[string]string</td>
        <td>
          matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### TenantResource.spec.resources[index].namespacedItems[index].selector.matchExpressions[index]



A label selector requirement is a selector that contains values, a key, and an operator that relates the key and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>key</b></td>
        <td>string</td>
        <td>
          key is the label key that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          operator represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists and DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          values is an array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### TenantResource.status



TenantResourceStatus defines the observed state of TenantResource.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantresourcestatusprocesseditemsindex">processedItems</a></b></td>
        <td>[]object</td>
        <td>
          List of the replicated resources for the given TenantResource.<br/>
        </td>
        <td>true</td>
      </tr></tbody>
</table>


### TenantResource.status.processedItems[index]





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>kind</b></td>
        <td>string</td>
        <td>
          Kind of the referent. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>name</b></td>
        <td>string</td>
        <td>
          Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>namespace</b></td>
        <td>string</td>
        <td>
          Namespace of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>apiVersion</b></td>
        <td>string</td>
        <td>
          API version of the referent.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>

## Tenant






Tenant is the Schema for the tenants API.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
      <td><b>apiVersion</b></td>
      <td>string</td>
      <td>capsule.clastix.io/v1beta2</td>
      <td>true</td>
      </tr>
      <tr>
      <td><b>kind</b></td>
      <td>string</td>
      <td>Tenant</td>
      <td>true</td>
      </tr>
      <tr>
      <td><b><a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#objectmeta-v1-meta">metadata</a></b></td>
      <td>object</td>
      <td>Refer to the Kubernetes API documentation for the fields of the `metadata` field.</td>
      <td>true</td>
      </tr><tr>
        <td><b><a href="#tenantspec-1">spec</a></b></td>
        <td>object</td>
        <td>
          TenantSpec defines the desired state of Tenant.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantstatus-1">status</a></b></td>
        <td>object</td>
        <td>
          Returns the observed state of the Tenant.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec



TenantSpec defines the desired state of Tenant.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecownersindex-1">owners</a></b></td>
        <td>[]object</td>
        <td>
          Specifies the owners of the Tenant. Mandatory.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b><a href="#tenantspecadditionalrolebindingsindex-1">additionalRoleBindings</a></b></td>
        <td>[]object</td>
        <td>
          Specifies additional RoleBindings assigned to the Tenant. Capsule will ensure that all namespaces in the Tenant always contain the RoleBinding for the given ClusterRole. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspeccontainerregistries-1">containerRegistries</a></b></td>
        <td>object</td>
        <td>
          Specifies the trusted Image Registries assigned to the Tenant. Capsule assures that all Pods resources created in the Tenant can use only one of the allowed trusted registries. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>cordoned</b></td>
        <td>boolean</td>
        <td>
          Toggling the Tenant resources cordoning, when enable resources cannot be deleted.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>imagePullPolicies</b></td>
        <td>[]enum</td>
        <td>
          Specify the allowed values for the imagePullPolicies option in Pod resources. Capsule assures that all Pod resources created in the Tenant can use only one of the allowed policy. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecingressoptions-1">ingressOptions</a></b></td>
        <td>object</td>
        <td>
          Specifies options for the Ingress resources, such as allowed hostnames and IngressClass. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspeclimitranges-1">limitRanges</a></b></td>
        <td>object</td>
        <td>
          Specifies the resource min/max usage restrictions to the Tenant. The assigned values are inherited by any namespace created in the Tenant. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnamespaceoptions-1">namespaceOptions</a></b></td>
        <td>object</td>
        <td>
          Specifies options for the Namespaces, such as additional metadata or maximum number of namespaces allowed for that Tenant. Once the namespace quota assigned to the Tenant has been reached, the Tenant owner cannot create further namespaces. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnetworkpolicies-1">networkPolicies</a></b></td>
        <td>object</td>
        <td>
          Specifies the NetworkPolicies assigned to the Tenant. The assigned NetworkPolicies are inherited by any namespace created in the Tenant. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>nodeSelector</b></td>
        <td>map[string]string</td>
        <td>
          Specifies the label to control the placement of pods on a given pool of worker nodes. All namespaces created within the Tenant will have the node selector annotation. This annotation tells the Kubernetes scheduler to place pods on the nodes having the selector label. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>preventDeletion</b></td>
        <td>boolean</td>
        <td>
          Prevent accidental deletion of the Tenant. When enabled, the deletion request will be declined.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecpriorityclasses-1">priorityClasses</a></b></td>
        <td>object</td>
        <td>
          Specifies the allowed priorityClasses assigned to the Tenant. Capsule assures that all Pods resources created in the Tenant can use only one of the allowed PriorityClasses. A default value can be specified, and all the Pod resources created will inherit the declared class. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecresourcequotas-1">resourceQuotas</a></b></td>
        <td>object</td>
        <td>
          Specifies a list of ResourceQuota resources assigned to the Tenant. The assigned values are inherited by any namespace created in the Tenant. The Capsule operator aggregates ResourceQuota at Tenant level, so that the hard quota is never crossed for the given Tenant. This permits the Tenant owner to consume resources in the Tenant regardless of the namespace. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecruntimeclasses">runtimeClasses</a></b></td>
        <td>object</td>
        <td>
          Specifies the allowed RuntimeClasses assigned to the Tenant. Capsule assures that all Pods resources created in the Tenant can use only one of the allowed RuntimeClasses. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecserviceoptions-1">serviceOptions</a></b></td>
        <td>object</td>
        <td>
          Specifies options for the Service, such as additional metadata or block of certain type of Services. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecstorageclasses-1">storageClasses</a></b></td>
        <td>object</td>
        <td>
          Specifies the allowed StorageClasses assigned to the Tenant. Capsule assures that all PersistentVolumeClaim resources created in the Tenant can use only one of the allowed StorageClasses. A default value can be specified, and all the PersistentVolumeClaim resources created will inherit the declared class. Optional.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.owners[index]





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>kind</b></td>
        <td>enum</td>
        <td>
          Kind of tenant owner. Possible values are "User", "Group", and "ServiceAccount"<br/>
          <br/>
            <i>Enum</i>: User, Group, ServiceAccount<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>name</b></td>
        <td>string</td>
        <td>
          Name of tenant owner.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>clusterRoles</b></td>
        <td>[]string</td>
        <td>
          Defines additional cluster-roles for the specific Owner.<br/>
          <br/>
            <i>Default</i>: [admin capsule-namespace-deleter]<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecownersindexproxysettingsindex-1">proxySettings</a></b></td>
        <td>[]object</td>
        <td>
          Proxy settings for tenant owner.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.owners[index].proxySettings[index]





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>kind</b></td>
        <td>enum</td>
        <td>
          <br/>
          <br/>
            <i>Enum</i>: Nodes, StorageClasses, IngressClasses, PriorityClasses, RuntimeClasses, PersistentVolumes<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operations</b></td>
        <td>[]enum</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr></tbody>
</table>


### Tenant.spec.additionalRoleBindings[index]





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>clusterRoleName</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b><a href="#tenantspecadditionalrolebindingsindexsubjectsindex-1">subjects</a></b></td>
        <td>[]object</td>
        <td>
          kubebuilder:validation:Minimum=1<br/>
        </td>
        <td>true</td>
      </tr></tbody>
</table>


### Tenant.spec.additionalRoleBindings[index].subjects[index]



Subject contains a reference to the object or user identities a role binding applies to.  This can either hold a direct API object reference, or a value for non-objects such as user and group names.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>kind</b></td>
        <td>string</td>
        <td>
          Kind of object being referenced. Values defined by this API group are "User", "Group", and "ServiceAccount". If the Authorizer does not recognized the kind value, the Authorizer should report an error.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>name</b></td>
        <td>string</td>
        <td>
          Name of the object being referenced.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>apiGroup</b></td>
        <td>string</td>
        <td>
          APIGroup holds the API group of the referenced subject. Defaults to "" for ServiceAccount subjects. Defaults to "rbac.authorization.k8s.io" for User and Group subjects.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>namespace</b></td>
        <td>string</td>
        <td>
          Namespace of the referenced object.  If the object kind is non-namespace, such as "User" or "Group", and this value is not empty the Authorizer should report an error.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.containerRegistries



Specifies the trusted Image Registries assigned to the Tenant. Capsule assures that all Pods resources created in the Tenant can use only one of the allowed trusted registries. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>allowed</b></td>
        <td>[]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>allowedRegex</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.ingressOptions



Specifies options for the Ingress resources, such as allowed hostnames and IngressClass. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>allowWildcardHostnames</b></td>
        <td>boolean</td>
        <td>
          Toggles the ability for Ingress resources created in a Tenant to have a hostname wildcard.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecingressoptionsallowedclasses-1">allowedClasses</a></b></td>
        <td>object</td>
        <td>
          Specifies the allowed IngressClasses assigned to the Tenant. Capsule assures that all Ingress resources created in the Tenant can use only one of the allowed IngressClasses. A default value can be specified, and all the Ingress resources created will inherit the declared class. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecingressoptionsallowedhostnames-1">allowedHostnames</a></b></td>
        <td>object</td>
        <td>
          Specifies the allowed hostnames in Ingresses for the given Tenant. Capsule assures that all Ingress resources created in the Tenant can use only one of the allowed hostnames. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>hostnameCollisionScope</b></td>
        <td>enum</td>
        <td>
          Defines the scope of hostname collision check performed when Tenant Owners create Ingress with allowed hostnames. 
 - Cluster: disallow the creation of an Ingress if the pair hostname and path is already used across the Namespaces managed by Capsule. 
 - Tenant: disallow the creation of an Ingress if the pair hostname and path is already used across the Namespaces of the Tenant. 
 - Namespace: disallow the creation of an Ingress if the pair hostname and path is already used in the Ingress Namespace. 
 Optional.<br/>
          <br/>
            <i>Enum</i>: Cluster, Tenant, Namespace, Disabled<br/>
            <i>Default</i>: Disabled<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.ingressOptions.allowedClasses



Specifies the allowed IngressClasses assigned to the Tenant. Capsule assures that all Ingress resources created in the Tenant can use only one of the allowed IngressClasses. A default value can be specified, and all the Ingress resources created will inherit the declared class. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>allowed</b></td>
        <td>[]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>allowedRegex</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>default</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecingressoptionsallowedclassesmatchexpressionsindex">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          matchExpressions is a list of label selector requirements. The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>matchLabels</b></td>
        <td>map[string]string</td>
        <td>
          matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.ingressOptions.allowedClasses.matchExpressions[index]



A label selector requirement is a selector that contains values, a key, and an operator that relates the key and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>key</b></td>
        <td>string</td>
        <td>
          key is the label key that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          operator represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists and DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          values is an array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.ingressOptions.allowedHostnames



Specifies the allowed hostnames in Ingresses for the given Tenant. Capsule assures that all Ingress resources created in the Tenant can use only one of the allowed hostnames. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>allowed</b></td>
        <td>[]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>allowedRegex</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.limitRanges



Specifies the resource min/max usage restrictions to the Tenant. The assigned values are inherited by any namespace created in the Tenant. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspeclimitrangesitemsindex-1">items</a></b></td>
        <td>[]object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.limitRanges.items[index]



LimitRangeSpec defines a min/max usage limit for resources that match on kind.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspeclimitrangesitemsindexlimitsindex-1">limits</a></b></td>
        <td>[]object</td>
        <td>
          Limits is the list of LimitRangeItem objects that are enforced.<br/>
        </td>
        <td>true</td>
      </tr></tbody>
</table>


### Tenant.spec.limitRanges.items[index].limits[index]



LimitRangeItem defines a min/max usage limit for any resource that matches on kind.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>type</b></td>
        <td>string</td>
        <td>
          Type of resource that this limit applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>default</b></td>
        <td>map[string]int or string</td>
        <td>
          Default resource requirement limit value by resource name if resource limit is omitted.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>defaultRequest</b></td>
        <td>map[string]int or string</td>
        <td>
          DefaultRequest is the default resource requirement request value by resource name if resource request is omitted.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>max</b></td>
        <td>map[string]int or string</td>
        <td>
          Max usage constraints on this kind by resource name.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>maxLimitRequestRatio</b></td>
        <td>map[string]int or string</td>
        <td>
          MaxLimitRequestRatio if specified, the named resource must have a request and limit that are both non-zero where limit divided by request is less than or equal to the enumerated value; this represents the max burst for the named resource.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>min</b></td>
        <td>map[string]int or string</td>
        <td>
          Min usage constraints on this kind by resource name.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.namespaceOptions



Specifies options for the Namespaces, such as additional metadata or maximum number of namespaces allowed for that Tenant. Once the namespace quota assigned to the Tenant has been reached, the Tenant owner cannot create further namespaces. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnamespaceoptionsadditionalmetadata-1">additionalMetadata</a></b></td>
        <td>object</td>
        <td>
          Specifies additional labels and annotations the Capsule operator places on any Namespace resource in the Tenant. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnamespaceoptionsforbiddenannotations">forbiddenAnnotations</a></b></td>
        <td>object</td>
        <td>
          Define the annotations that a Tenant Owner cannot set for their Namespace resources.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnamespaceoptionsforbiddenlabels">forbiddenLabels</a></b></td>
        <td>object</td>
        <td>
          Define the labels that a Tenant Owner cannot set for their Namespace resources.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>quota</b></td>
        <td>integer</td>
        <td>
          Specifies the maximum number of namespaces allowed for that Tenant. Once the namespace quota assigned to the Tenant has been reached, the Tenant owner cannot create further namespaces. Optional.<br/>
          <br/>
            <i>Format</i>: int32<br/>
            <i>Minimum</i>: 1<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.namespaceOptions.additionalMetadata



Specifies additional labels and annotations the Capsule operator places on any Namespace resource in the Tenant. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>annotations</b></td>
        <td>map[string]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>labels</b></td>
        <td>map[string]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.namespaceOptions.forbiddenAnnotations



Define the annotations that a Tenant Owner cannot set for their Namespace resources.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>denied</b></td>
        <td>[]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>deniedRegex</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.namespaceOptions.forbiddenLabels



Define the labels that a Tenant Owner cannot set for their Namespace resources.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>denied</b></td>
        <td>[]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>deniedRegex</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies



Specifies the NetworkPolicies assigned to the Tenant. The assigned NetworkPolicies are inherited by any namespace created in the Tenant. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindex-1">items</a></b></td>
        <td>[]object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index]



NetworkPolicySpec provides the specification of a NetworkPolicy

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexpodselector-1">podSelector</a></b></td>
        <td>object</td>
        <td>
          podSelector selects the pods to which this NetworkPolicy object applies. The array of ingress rules is applied to any pods selected by this field. Multiple network policies can select the same set of pods. In this case, the ingress rules for each are combined additively. This field is NOT optional and follows standard label selector semantics. An empty podSelector matches all pods in this namespace.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexegressindex-1">egress</a></b></td>
        <td>[]object</td>
        <td>
          egress is a list of egress rules to be applied to the selected pods. Outgoing traffic is allowed if there are no NetworkPolicies selecting the pod (and cluster policy otherwise allows the traffic), OR if the traffic matches at least one egress rule across all of the NetworkPolicy objects whose podSelector matches the pod. If this field is empty then this NetworkPolicy limits all outgoing traffic (and serves solely to ensure that the pods it selects are isolated by default). This field is beta-level in 1.8<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexingressindex-1">ingress</a></b></td>
        <td>[]object</td>
        <td>
          ingress is a list of ingress rules to be applied to the selected pods. Traffic is allowed to a pod if there are no NetworkPolicies selecting the pod (and cluster policy otherwise allows the traffic), OR if the traffic source is the pod's local node, OR if the traffic matches at least one ingress rule across all of the NetworkPolicy objects whose podSelector matches the pod. If this field is empty then this NetworkPolicy does not allow any traffic (and serves solely to ensure that the pods it selects are isolated by default)<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>policyTypes</b></td>
        <td>[]string</td>
        <td>
          policyTypes is a list of rule types that the NetworkPolicy relates to. Valid options are ["Ingress"], ["Egress"], or ["Ingress", "Egress"]. If this field is not specified, it will default based on the existence of ingress or egress rules; policies that contain an egress section are assumed to affect egress, and all policies (whether or not they contain an ingress section) are assumed to affect ingress. If you want to write an egress-only policy, you must explicitly specify policyTypes [ "Egress" ]. Likewise, if you want to write a policy that specifies that no egress is allowed, you must specify a policyTypes value that include "Egress" (since such a policy would not include an egress section and would otherwise default to just [ "Ingress" ]). This field is beta-level in 1.8<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].podSelector



podSelector selects the pods to which this NetworkPolicy object applies. The array of ingress rules is applied to any pods selected by this field. Multiple network policies can select the same set of pods. In this case, the ingress rules for each are combined additively. This field is NOT optional and follows standard label selector semantics. An empty podSelector matches all pods in this namespace.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexpodselectormatchexpressionsindex-1">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          matchExpressions is a list of label selector requirements. The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>matchLabels</b></td>
        <td>map[string]string</td>
        <td>
          matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].podSelector.matchExpressions[index]



A label selector requirement is a selector that contains values, a key, and an operator that relates the key and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>key</b></td>
        <td>string</td>
        <td>
          key is the label key that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          operator represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists and DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          values is an array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].egress[index]



NetworkPolicyEgressRule describes a particular set of traffic that is allowed out of pods matched by a NetworkPolicySpec's podSelector. The traffic must match both ports and to. This type is beta-level in 1.8

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexegressindexportsindex-1">ports</a></b></td>
        <td>[]object</td>
        <td>
          ports is a list of destination ports for outgoing traffic. Each item in this list is combined using a logical OR. If this field is empty or missing, this rule matches all ports (traffic not restricted by port). If this field is present and contains at least one item, then this rule allows traffic only if the traffic matches at least one port in the list.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexegressindextoindex-1">to</a></b></td>
        <td>[]object</td>
        <td>
          to is a list of destinations for outgoing traffic of pods selected for this rule. Items in this list are combined using a logical OR operation. If this field is empty or missing, this rule matches all destinations (traffic not restricted by destination). If this field is present and contains at least one item, this rule allows traffic only if the traffic matches at least one item in the to list.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].egress[index].ports[index]



NetworkPolicyPort describes a port to allow traffic on

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>endPort</b></td>
        <td>integer</td>
        <td>
          endPort indicates that the range of ports from port to endPort if set, inclusive, should be allowed by the policy. This field cannot be defined if the port field is not defined or if the port field is defined as a named (string) port. The endPort must be equal or greater than port.<br/>
          <br/>
            <i>Format</i>: int32<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>port</b></td>
        <td>int or string</td>
        <td>
          port represents the port on the given protocol. This can either be a numerical or named port on a pod. If this field is not provided, this matches all port names and numbers. If present, only traffic on the specified protocol AND port will be matched.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>protocol</b></td>
        <td>string</td>
        <td>
          protocol represents the protocol (TCP, UDP, or SCTP) which traffic must match. If not specified, this field defaults to TCP.<br/>
          <br/>
            <i>Default</i>: TCP<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].egress[index].to[index]



NetworkPolicyPeer describes a peer to allow traffic to/from. Only certain combinations of fields are allowed

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexegressindextoindexipblock-1">ipBlock</a></b></td>
        <td>object</td>
        <td>
          ipBlock defines policy on a particular IPBlock. If this field is set then neither of the other fields can be.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexegressindextoindexnamespaceselector-1">namespaceSelector</a></b></td>
        <td>object</td>
        <td>
          namespaceSelector selects namespaces using cluster-scoped labels. This field follows standard label selector semantics; if present but empty, it selects all namespaces. 
 If podSelector is also set, then the NetworkPolicyPeer as a whole selects the pods matching podSelector in the namespaces selected by namespaceSelector. Otherwise it selects all pods in the namespaces selected by namespaceSelector.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexegressindextoindexpodselector-1">podSelector</a></b></td>
        <td>object</td>
        <td>
          podSelector is a label selector which selects pods. This field follows standard label selector semantics; if present but empty, it selects all pods. 
 If namespaceSelector is also set, then the NetworkPolicyPeer as a whole selects the pods matching podSelector in the Namespaces selected by NamespaceSelector. Otherwise it selects the pods matching podSelector in the policy's own namespace.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].egress[index].to[index].ipBlock



ipBlock defines policy on a particular IPBlock. If this field is set then neither of the other fields can be.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>cidr</b></td>
        <td>string</td>
        <td>
          cidr is a string representing the IPBlock Valid examples are "192.168.1.0/24" or "2001:db8::/64"<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>except</b></td>
        <td>[]string</td>
        <td>
          except is a slice of CIDRs that should not be included within an IPBlock Valid examples are "192.168.1.0/24" or "2001:db8::/64" Except values will be rejected if they are outside the cidr range<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].egress[index].to[index].namespaceSelector



namespaceSelector selects namespaces using cluster-scoped labels. This field follows standard label selector semantics; if present but empty, it selects all namespaces. 
 If podSelector is also set, then the NetworkPolicyPeer as a whole selects the pods matching podSelector in the namespaces selected by namespaceSelector. Otherwise it selects all pods in the namespaces selected by namespaceSelector.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexegressindextoindexnamespaceselectormatchexpressionsindex-1">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          matchExpressions is a list of label selector requirements. The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>matchLabels</b></td>
        <td>map[string]string</td>
        <td>
          matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].egress[index].to[index].namespaceSelector.matchExpressions[index]



A label selector requirement is a selector that contains values, a key, and an operator that relates the key and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>key</b></td>
        <td>string</td>
        <td>
          key is the label key that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          operator represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists and DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          values is an array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].egress[index].to[index].podSelector



podSelector is a label selector which selects pods. This field follows standard label selector semantics; if present but empty, it selects all pods. 
 If namespaceSelector is also set, then the NetworkPolicyPeer as a whole selects the pods matching podSelector in the Namespaces selected by NamespaceSelector. Otherwise it selects the pods matching podSelector in the policy's own namespace.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexegressindextoindexpodselectormatchexpressionsindex-1">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          matchExpressions is a list of label selector requirements. The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>matchLabels</b></td>
        <td>map[string]string</td>
        <td>
          matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].egress[index].to[index].podSelector.matchExpressions[index]



A label selector requirement is a selector that contains values, a key, and an operator that relates the key and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>key</b></td>
        <td>string</td>
        <td>
          key is the label key that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          operator represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists and DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          values is an array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].ingress[index]



NetworkPolicyIngressRule describes a particular set of traffic that is allowed to the pods matched by a NetworkPolicySpec's podSelector. The traffic must match both ports and from.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexingressindexfromindex-1">from</a></b></td>
        <td>[]object</td>
        <td>
          from is a list of sources which should be able to access the pods selected for this rule. Items in this list are combined using a logical OR operation. If this field is empty or missing, this rule matches all sources (traffic not restricted by source). If this field is present and contains at least one item, this rule allows traffic only if the traffic matches at least one item in the from list.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexingressindexportsindex-1">ports</a></b></td>
        <td>[]object</td>
        <td>
          ports is a list of ports which should be made accessible on the pods selected for this rule. Each item in this list is combined using a logical OR. If this field is empty or missing, this rule matches all ports (traffic not restricted by port). If this field is present and contains at least one item, then this rule allows traffic only if the traffic matches at least one port in the list.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].ingress[index].from[index]



NetworkPolicyPeer describes a peer to allow traffic to/from. Only certain combinations of fields are allowed

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexingressindexfromindexipblock-1">ipBlock</a></b></td>
        <td>object</td>
        <td>
          ipBlock defines policy on a particular IPBlock. If this field is set then neither of the other fields can be.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexingressindexfromindexnamespaceselector-1">namespaceSelector</a></b></td>
        <td>object</td>
        <td>
          namespaceSelector selects namespaces using cluster-scoped labels. This field follows standard label selector semantics; if present but empty, it selects all namespaces. 
 If podSelector is also set, then the NetworkPolicyPeer as a whole selects the pods matching podSelector in the namespaces selected by namespaceSelector. Otherwise it selects all pods in the namespaces selected by namespaceSelector.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexingressindexfromindexpodselector-1">podSelector</a></b></td>
        <td>object</td>
        <td>
          podSelector is a label selector which selects pods. This field follows standard label selector semantics; if present but empty, it selects all pods. 
 If namespaceSelector is also set, then the NetworkPolicyPeer as a whole selects the pods matching podSelector in the Namespaces selected by NamespaceSelector. Otherwise it selects the pods matching podSelector in the policy's own namespace.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].ingress[index].from[index].ipBlock



ipBlock defines policy on a particular IPBlock. If this field is set then neither of the other fields can be.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>cidr</b></td>
        <td>string</td>
        <td>
          cidr is a string representing the IPBlock Valid examples are "192.168.1.0/24" or "2001:db8::/64"<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>except</b></td>
        <td>[]string</td>
        <td>
          except is a slice of CIDRs that should not be included within an IPBlock Valid examples are "192.168.1.0/24" or "2001:db8::/64" Except values will be rejected if they are outside the cidr range<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].ingress[index].from[index].namespaceSelector



namespaceSelector selects namespaces using cluster-scoped labels. This field follows standard label selector semantics; if present but empty, it selects all namespaces. 
 If podSelector is also set, then the NetworkPolicyPeer as a whole selects the pods matching podSelector in the namespaces selected by namespaceSelector. Otherwise it selects all pods in the namespaces selected by namespaceSelector.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexingressindexfromindexnamespaceselectormatchexpressionsindex-1">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          matchExpressions is a list of label selector requirements. The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>matchLabels</b></td>
        <td>map[string]string</td>
        <td>
          matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].ingress[index].from[index].namespaceSelector.matchExpressions[index]



A label selector requirement is a selector that contains values, a key, and an operator that relates the key and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>key</b></td>
        <td>string</td>
        <td>
          key is the label key that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          operator represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists and DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          values is an array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].ingress[index].from[index].podSelector



podSelector is a label selector which selects pods. This field follows standard label selector semantics; if present but empty, it selects all pods. 
 If namespaceSelector is also set, then the NetworkPolicyPeer as a whole selects the pods matching podSelector in the Namespaces selected by NamespaceSelector. Otherwise it selects the pods matching podSelector in the policy's own namespace.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexingressindexfromindexpodselectormatchexpressionsindex-1">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          matchExpressions is a list of label selector requirements. The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>matchLabels</b></td>
        <td>map[string]string</td>
        <td>
          matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].ingress[index].from[index].podSelector.matchExpressions[index]



A label selector requirement is a selector that contains values, a key, and an operator that relates the key and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>key</b></td>
        <td>string</td>
        <td>
          key is the label key that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          operator represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists and DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          values is an array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].ingress[index].ports[index]



NetworkPolicyPort describes a port to allow traffic on

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>endPort</b></td>
        <td>integer</td>
        <td>
          endPort indicates that the range of ports from port to endPort if set, inclusive, should be allowed by the policy. This field cannot be defined if the port field is not defined or if the port field is defined as a named (string) port. The endPort must be equal or greater than port.<br/>
          <br/>
            <i>Format</i>: int32<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>port</b></td>
        <td>int or string</td>
        <td>
          port represents the port on the given protocol. This can either be a numerical or named port on a pod. If this field is not provided, this matches all port names and numbers. If present, only traffic on the specified protocol AND port will be matched.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>protocol</b></td>
        <td>string</td>
        <td>
          protocol represents the protocol (TCP, UDP, or SCTP) which traffic must match. If not specified, this field defaults to TCP.<br/>
          <br/>
            <i>Default</i>: TCP<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.priorityClasses



Specifies the allowed priorityClasses assigned to the Tenant. Capsule assures that all Pods resources created in the Tenant can use only one of the allowed PriorityClasses. A default value can be specified, and all the Pod resources created will inherit the declared class. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>allowed</b></td>
        <td>[]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>allowedRegex</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>default</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecpriorityclassesmatchexpressionsindex">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          matchExpressions is a list of label selector requirements. The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>matchLabels</b></td>
        <td>map[string]string</td>
        <td>
          matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.priorityClasses.matchExpressions[index]



A label selector requirement is a selector that contains values, a key, and an operator that relates the key and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>key</b></td>
        <td>string</td>
        <td>
          key is the label key that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          operator represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists and DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          values is an array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.resourceQuotas



Specifies a list of ResourceQuota resources assigned to the Tenant. The assigned values are inherited by any namespace created in the Tenant. The Capsule operator aggregates ResourceQuota at Tenant level, so that the hard quota is never crossed for the given Tenant. This permits the Tenant owner to consume resources in the Tenant regardless of the namespace. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecresourcequotasitemsindex-1">items</a></b></td>
        <td>[]object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>scope</b></td>
        <td>enum</td>
        <td>
          Define if the Resource Budget should compute resource across all Namespaces in the Tenant or individually per cluster. Default is Tenant<br/>
          <br/>
            <i>Enum</i>: Tenant, Namespace<br/>
            <i>Default</i>: Tenant<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.resourceQuotas.items[index]



ResourceQuotaSpec defines the desired hard limits to enforce for Quota.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>hard</b></td>
        <td>map[string]int or string</td>
        <td>
          hard is the set of desired hard limits for each named resource. More info: https://kubernetes.io/docs/concepts/policy/resource-quotas/<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecresourcequotasitemsindexscopeselector-1">scopeSelector</a></b></td>
        <td>object</td>
        <td>
          scopeSelector is also a collection of filters like scopes that must match each object tracked by a quota but expressed using ScopeSelectorOperator in combination with possible values. For a resource to match, both scopes AND scopeSelector (if specified in spec), must be matched.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>scopes</b></td>
        <td>[]string</td>
        <td>
          A collection of filters that must match each object tracked by a quota. If not specified, the quota matches all objects.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.resourceQuotas.items[index].scopeSelector



scopeSelector is also a collection of filters like scopes that must match each object tracked by a quota but expressed using ScopeSelectorOperator in combination with possible values. For a resource to match, both scopes AND scopeSelector (if specified in spec), must be matched.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecresourcequotasitemsindexscopeselectormatchexpressionsindex-1">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          A list of scope selector requirements by scope of the resources.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.resourceQuotas.items[index].scopeSelector.matchExpressions[index]



A scoped-resource selector requirement is a selector that contains values, a scope name, and an operator that relates the scope name and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          Represents a scope's relationship to a set of values. Valid operators are In, NotIn, Exists, DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>scopeName</b></td>
        <td>string</td>
        <td>
          The name of the scope that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          An array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.runtimeClasses



Specifies the allowed RuntimeClasses assigned to the Tenant. Capsule assures that all Pods resources created in the Tenant can use only one of the allowed RuntimeClasses. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>allowed</b></td>
        <td>[]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>allowedRegex</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecruntimeclassesmatchexpressionsindex">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          matchExpressions is a list of label selector requirements. The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>matchLabels</b></td>
        <td>map[string]string</td>
        <td>
          matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.runtimeClasses.matchExpressions[index]



A label selector requirement is a selector that contains values, a key, and an operator that relates the key and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>key</b></td>
        <td>string</td>
        <td>
          key is the label key that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          operator represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists and DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          values is an array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.serviceOptions



Specifies options for the Service, such as additional metadata or block of certain type of Services. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecserviceoptionsadditionalmetadata-1">additionalMetadata</a></b></td>
        <td>object</td>
        <td>
          Specifies additional labels and annotations the Capsule operator places on any Service resource in the Tenant. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecserviceoptionsallowedservices-1">allowedServices</a></b></td>
        <td>object</td>
        <td>
          Block or deny certain type of Services. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecserviceoptionsexternalips-1">externalIPs</a></b></td>
        <td>object</td>
        <td>
          Specifies the external IPs that can be used in Services with type ClusterIP. An empty list means no IPs are allowed. Optional.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.serviceOptions.additionalMetadata



Specifies additional labels and annotations the Capsule operator places on any Service resource in the Tenant. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>annotations</b></td>
        <td>map[string]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>labels</b></td>
        <td>map[string]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.serviceOptions.allowedServices



Block or deny certain type of Services. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>externalName</b></td>
        <td>boolean</td>
        <td>
          Specifies if ExternalName service type resources are allowed for the Tenant. Default is true. Optional.<br/>
          <br/>
            <i>Default</i>: true<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>loadBalancer</b></td>
        <td>boolean</td>
        <td>
          Specifies if LoadBalancer service type resources are allowed for the Tenant. Default is true. Optional.<br/>
          <br/>
            <i>Default</i>: true<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>nodePort</b></td>
        <td>boolean</td>
        <td>
          Specifies if NodePort service type resources are allowed for the Tenant. Default is true. Optional.<br/>
          <br/>
            <i>Default</i>: true<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.serviceOptions.externalIPs



Specifies the external IPs that can be used in Services with type ClusterIP. An empty list means no IPs are allowed. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>allowed</b></td>
        <td>[]string</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr></tbody>
</table>


### Tenant.spec.storageClasses



Specifies the allowed StorageClasses assigned to the Tenant. Capsule assures that all PersistentVolumeClaim resources created in the Tenant can use only one of the allowed StorageClasses. A default value can be specified, and all the PersistentVolumeClaim resources created will inherit the declared class. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>allowed</b></td>
        <td>[]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>allowedRegex</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>default</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecstorageclassesmatchexpressionsindex">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          matchExpressions is a list of label selector requirements. The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>matchLabels</b></td>
        <td>map[string]string</td>
        <td>
          matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.storageClasses.matchExpressions[index]



A label selector requirement is a selector that contains values, a key, and an operator that relates the key and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>key</b></td>
        <td>string</td>
        <td>
          key is the label key that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          operator represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists and DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          values is an array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.status



Returns the observed state of the Tenant.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>size</b></td>
        <td>integer</td>
        <td>
          How many namespaces are assigned to the Tenant.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>state</b></td>
        <td>enum</td>
        <td>
          The operational state of the Tenant. Possible values are "Active", "Cordoned".<br/>
          <br/>
            <i>Enum</i>: Cordoned, Active<br/>
            <i>Default</i>: Active<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>namespaces</b></td>
        <td>[]string</td>
        <td>
          List of namespaces assigned to the Tenant.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>

# capsule.clastix.io/v1beta1

Resource Types:

- [Tenant](#tenant)




## Tenant






Tenant is the Schema for the tenants API.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
      <td><b>apiVersion</b></td>
      <td>string</td>
      <td>capsule.clastix.io/v1beta1</td>
      <td>true</td>
      </tr>
      <tr>
      <td><b>kind</b></td>
      <td>string</td>
      <td>Tenant</td>
      <td>true</td>
      </tr>
      <tr>
      <td><b><a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#objectmeta-v1-meta">metadata</a></b></td>
      <td>object</td>
      <td>Refer to the Kubernetes API documentation for the fields of the `metadata` field.</td>
      <td>true</td>
      </tr><tr>
        <td><b><a href="#tenantspec-1">spec</a></b></td>
        <td>object</td>
        <td>
          TenantSpec defines the desired state of Tenant.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantstatus-1">status</a></b></td>
        <td>object</td>
        <td>
          Returns the observed state of the Tenant.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec



TenantSpec defines the desired state of Tenant.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecownersindex">owners</a></b></td>
        <td>[]object</td>
        <td>
          Specifies the owners of the Tenant. Mandatory.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b><a href="#tenantspecadditionalrolebindingsindex-1">additionalRoleBindings</a></b></td>
        <td>[]object</td>
        <td>
          Specifies additional RoleBindings assigned to the Tenant. Capsule will ensure that all namespaces in the Tenant always contain the RoleBinding for the given ClusterRole. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspeccontainerregistries-1">containerRegistries</a></b></td>
        <td>object</td>
        <td>
          Specifies the trusted Image Registries assigned to the Tenant. Capsule assures that all Pods resources created in the Tenant can use only one of the allowed trusted registries. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>imagePullPolicies</b></td>
        <td>[]enum</td>
        <td>
          Specify the allowed values for the imagePullPolicies option in Pod resources. Capsule assures that all Pod resources created in the Tenant can use only one of the allowed policy. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecingressoptions">ingressOptions</a></b></td>
        <td>object</td>
        <td>
          Specifies options for the Ingress resources, such as allowed hostnames and IngressClass. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspeclimitranges">limitRanges</a></b></td>
        <td>object</td>
        <td>
          Specifies the resource min/max usage restrictions to the Tenant. The assigned values are inherited by any namespace created in the Tenant. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnamespaceoptions">namespaceOptions</a></b></td>
        <td>object</td>
        <td>
          Specifies options for the Namespaces, such as additional metadata or maximum number of namespaces allowed for that Tenant. Once the namespace quota assigned to the Tenant has been reached, the Tenant owner cannot create further namespaces. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnetworkpolicies">networkPolicies</a></b></td>
        <td>object</td>
        <td>
          Specifies the NetworkPolicies assigned to the Tenant. The assigned NetworkPolicies are inherited by any namespace created in the Tenant. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>nodeSelector</b></td>
        <td>map[string]string</td>
        <td>
          Specifies the label to control the placement of pods on a given pool of worker nodes. All namespaces created within the Tenant will have the node selector annotation. This annotation tells the Kubernetes scheduler to place pods on the nodes having the selector label. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecpriorityclasses">priorityClasses</a></b></td>
        <td>object</td>
        <td>
          Specifies the allowed priorityClasses assigned to the Tenant. Capsule assures that all Pods resources created in the Tenant can use only one of the allowed PriorityClasses. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecresourcequotas">resourceQuotas</a></b></td>
        <td>object</td>
        <td>
          Specifies a list of ResourceQuota resources assigned to the Tenant. The assigned values are inherited by any namespace created in the Tenant. The Capsule operator aggregates ResourceQuota at Tenant level, so that the hard quota is never crossed for the given Tenant. This permits the Tenant owner to consume resources in the Tenant regardless of the namespace. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecserviceoptions">serviceOptions</a></b></td>
        <td>object</td>
        <td>
          Specifies options for the Service, such as additional metadata or block of certain type of Services. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecstorageclasses-1">storageClasses</a></b></td>
        <td>object</td>
        <td>
          Specifies the allowed StorageClasses assigned to the Tenant. Capsule assures that all PersistentVolumeClaim resources created in the Tenant can use only one of the allowed StorageClasses. Optional.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.owners[index]





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>kind</b></td>
        <td>enum</td>
        <td>
          Kind of tenant owner. Possible values are "User", "Group", and "ServiceAccount"<br/>
          <br/>
            <i>Enum</i>: User, Group, ServiceAccount<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>name</b></td>
        <td>string</td>
        <td>
          Name of tenant owner.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b><a href="#tenantspecownersindexproxysettingsindex">proxySettings</a></b></td>
        <td>[]object</td>
        <td>
          Proxy settings for tenant owner.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.owners[index].proxySettings[index]





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>kind</b></td>
        <td>enum</td>
        <td>
          <br/>
          <br/>
            <i>Enum</i>: Nodes, StorageClasses, IngressClasses, PriorityClasses<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operations</b></td>
        <td>[]enum</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr></tbody>
</table>


### Tenant.spec.additionalRoleBindings[index]





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>clusterRoleName</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b><a href="#tenantspecadditionalrolebindingsindexsubjectsindex-1">subjects</a></b></td>
        <td>[]object</td>
        <td>
          kubebuilder:validation:Minimum=1<br/>
        </td>
        <td>true</td>
      </tr></tbody>
</table>


### Tenant.spec.additionalRoleBindings[index].subjects[index]



Subject contains a reference to the object or user identities a role binding applies to.  This can either hold a direct API object reference, or a value for non-objects such as user and group names.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>kind</b></td>
        <td>string</td>
        <td>
          Kind of object being referenced. Values defined by this API group are "User", "Group", and "ServiceAccount". If the Authorizer does not recognized the kind value, the Authorizer should report an error.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>name</b></td>
        <td>string</td>
        <td>
          Name of the object being referenced.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>apiGroup</b></td>
        <td>string</td>
        <td>
          APIGroup holds the API group of the referenced subject. Defaults to "" for ServiceAccount subjects. Defaults to "rbac.authorization.k8s.io" for User and Group subjects.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>namespace</b></td>
        <td>string</td>
        <td>
          Namespace of the referenced object.  If the object kind is non-namespace, such as "User" or "Group", and this value is not empty the Authorizer should report an error.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.containerRegistries



Specifies the trusted Image Registries assigned to the Tenant. Capsule assures that all Pods resources created in the Tenant can use only one of the allowed trusted registries. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>allowed</b></td>
        <td>[]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>allowedRegex</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.ingressOptions



Specifies options for the Ingress resources, such as allowed hostnames and IngressClass. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecingressoptionsallowedclasses">allowedClasses</a></b></td>
        <td>object</td>
        <td>
          Specifies the allowed IngressClasses assigned to the Tenant. Capsule assures that all Ingress resources created in the Tenant can use only one of the allowed IngressClasses. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecingressoptionsallowedhostnames">allowedHostnames</a></b></td>
        <td>object</td>
        <td>
          Specifies the allowed hostnames in Ingresses for the given Tenant. Capsule assures that all Ingress resources created in the Tenant can use only one of the allowed hostnames. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>hostnameCollisionScope</b></td>
        <td>enum</td>
        <td>
          Defines the scope of hostname collision check performed when Tenant Owners create Ingress with allowed hostnames. 
 - Cluster: disallow the creation of an Ingress if the pair hostname and path is already used across the Namespaces managed by Capsule. 
 - Tenant: disallow the creation of an Ingress if the pair hostname and path is already used across the Namespaces of the Tenant. 
 - Namespace: disallow the creation of an Ingress if the pair hostname and path is already used in the Ingress Namespace. 
 Optional.<br/>
          <br/>
            <i>Enum</i>: Cluster, Tenant, Namespace, Disabled<br/>
            <i>Default</i>: Disabled<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.ingressOptions.allowedClasses



Specifies the allowed IngressClasses assigned to the Tenant. Capsule assures that all Ingress resources created in the Tenant can use only one of the allowed IngressClasses. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>allowed</b></td>
        <td>[]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>allowedRegex</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.ingressOptions.allowedHostnames



Specifies the allowed hostnames in Ingresses for the given Tenant. Capsule assures that all Ingress resources created in the Tenant can use only one of the allowed hostnames. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>allowed</b></td>
        <td>[]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>allowedRegex</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.limitRanges



Specifies the resource min/max usage restrictions to the Tenant. The assigned values are inherited by any namespace created in the Tenant. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspeclimitrangesitemsindex">items</a></b></td>
        <td>[]object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.limitRanges.items[index]



LimitRangeSpec defines a min/max usage limit for resources that match on kind.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspeclimitrangesitemsindexlimitsindex">limits</a></b></td>
        <td>[]object</td>
        <td>
          Limits is the list of LimitRangeItem objects that are enforced.<br/>
        </td>
        <td>true</td>
      </tr></tbody>
</table>


### Tenant.spec.limitRanges.items[index].limits[index]



LimitRangeItem defines a min/max usage limit for any resource that matches on kind.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>type</b></td>
        <td>string</td>
        <td>
          Type of resource that this limit applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>default</b></td>
        <td>map[string]int or string</td>
        <td>
          Default resource requirement limit value by resource name if resource limit is omitted.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>defaultRequest</b></td>
        <td>map[string]int or string</td>
        <td>
          DefaultRequest is the default resource requirement request value by resource name if resource request is omitted.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>max</b></td>
        <td>map[string]int or string</td>
        <td>
          Max usage constraints on this kind by resource name.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>maxLimitRequestRatio</b></td>
        <td>map[string]int or string</td>
        <td>
          MaxLimitRequestRatio if specified, the named resource must have a request and limit that are both non-zero where limit divided by request is less than or equal to the enumerated value; this represents the max burst for the named resource.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>min</b></td>
        <td>map[string]int or string</td>
        <td>
          Min usage constraints on this kind by resource name.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.namespaceOptions



Specifies options for the Namespaces, such as additional metadata or maximum number of namespaces allowed for that Tenant. Once the namespace quota assigned to the Tenant has been reached, the Tenant owner cannot create further namespaces. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnamespaceoptionsadditionalmetadata">additionalMetadata</a></b></td>
        <td>object</td>
        <td>
          Specifies additional labels and annotations the Capsule operator places on any Namespace resource in the Tenant. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>quota</b></td>
        <td>integer</td>
        <td>
          Specifies the maximum number of namespaces allowed for that Tenant. Once the namespace quota assigned to the Tenant has been reached, the Tenant owner cannot create further namespaces. Optional.<br/>
          <br/>
            <i>Format</i>: int32<br/>
            <i>Minimum</i>: 1<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.namespaceOptions.additionalMetadata



Specifies additional labels and annotations the Capsule operator places on any Namespace resource in the Tenant. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>annotations</b></td>
        <td>map[string]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>labels</b></td>
        <td>map[string]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies



Specifies the NetworkPolicies assigned to the Tenant. The assigned NetworkPolicies are inherited by any namespace created in the Tenant. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindex">items</a></b></td>
        <td>[]object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index]



NetworkPolicySpec provides the specification of a NetworkPolicy

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexpodselector">podSelector</a></b></td>
        <td>object</td>
        <td>
          podSelector selects the pods to which this NetworkPolicy object applies. The array of ingress rules is applied to any pods selected by this field. Multiple network policies can select the same set of pods. In this case, the ingress rules for each are combined additively. This field is NOT optional and follows standard label selector semantics. An empty podSelector matches all pods in this namespace.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexegressindex">egress</a></b></td>
        <td>[]object</td>
        <td>
          egress is a list of egress rules to be applied to the selected pods. Outgoing traffic is allowed if there are no NetworkPolicies selecting the pod (and cluster policy otherwise allows the traffic), OR if the traffic matches at least one egress rule across all of the NetworkPolicy objects whose podSelector matches the pod. If this field is empty then this NetworkPolicy limits all outgoing traffic (and serves solely to ensure that the pods it selects are isolated by default). This field is beta-level in 1.8<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexingressindex">ingress</a></b></td>
        <td>[]object</td>
        <td>
          ingress is a list of ingress rules to be applied to the selected pods. Traffic is allowed to a pod if there are no NetworkPolicies selecting the pod (and cluster policy otherwise allows the traffic), OR if the traffic source is the pod's local node, OR if the traffic matches at least one ingress rule across all of the NetworkPolicy objects whose podSelector matches the pod. If this field is empty then this NetworkPolicy does not allow any traffic (and serves solely to ensure that the pods it selects are isolated by default)<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>policyTypes</b></td>
        <td>[]string</td>
        <td>
          policyTypes is a list of rule types that the NetworkPolicy relates to. Valid options are ["Ingress"], ["Egress"], or ["Ingress", "Egress"]. If this field is not specified, it will default based on the existence of ingress or egress rules; policies that contain an egress section are assumed to affect egress, and all policies (whether or not they contain an ingress section) are assumed to affect ingress. If you want to write an egress-only policy, you must explicitly specify policyTypes [ "Egress" ]. Likewise, if you want to write a policy that specifies that no egress is allowed, you must specify a policyTypes value that include "Egress" (since such a policy would not include an egress section and would otherwise default to just [ "Ingress" ]). This field is beta-level in 1.8<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].podSelector



podSelector selects the pods to which this NetworkPolicy object applies. The array of ingress rules is applied to any pods selected by this field. Multiple network policies can select the same set of pods. In this case, the ingress rules for each are combined additively. This field is NOT optional and follows standard label selector semantics. An empty podSelector matches all pods in this namespace.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexpodselectormatchexpressionsindex">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          matchExpressions is a list of label selector requirements. The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>matchLabels</b></td>
        <td>map[string]string</td>
        <td>
          matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].podSelector.matchExpressions[index]



A label selector requirement is a selector that contains values, a key, and an operator that relates the key and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>key</b></td>
        <td>string</td>
        <td>
          key is the label key that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          operator represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists and DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          values is an array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].egress[index]



NetworkPolicyEgressRule describes a particular set of traffic that is allowed out of pods matched by a NetworkPolicySpec's podSelector. The traffic must match both ports and to. This type is beta-level in 1.8

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexegressindexportsindex">ports</a></b></td>
        <td>[]object</td>
        <td>
          ports is a list of destination ports for outgoing traffic. Each item in this list is combined using a logical OR. If this field is empty or missing, this rule matches all ports (traffic not restricted by port). If this field is present and contains at least one item, then this rule allows traffic only if the traffic matches at least one port in the list.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexegressindextoindex">to</a></b></td>
        <td>[]object</td>
        <td>
          to is a list of destinations for outgoing traffic of pods selected for this rule. Items in this list are combined using a logical OR operation. If this field is empty or missing, this rule matches all destinations (traffic not restricted by destination). If this field is present and contains at least one item, this rule allows traffic only if the traffic matches at least one item in the to list.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].egress[index].ports[index]



NetworkPolicyPort describes a port to allow traffic on

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>endPort</b></td>
        <td>integer</td>
        <td>
          endPort indicates that the range of ports from port to endPort if set, inclusive, should be allowed by the policy. This field cannot be defined if the port field is not defined or if the port field is defined as a named (string) port. The endPort must be equal or greater than port.<br/>
          <br/>
            <i>Format</i>: int32<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>port</b></td>
        <td>int or string</td>
        <td>
          port represents the port on the given protocol. This can either be a numerical or named port on a pod. If this field is not provided, this matches all port names and numbers. If present, only traffic on the specified protocol AND port will be matched.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>protocol</b></td>
        <td>string</td>
        <td>
          protocol represents the protocol (TCP, UDP, or SCTP) which traffic must match. If not specified, this field defaults to TCP.<br/>
          <br/>
            <i>Default</i>: TCP<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].egress[index].to[index]



NetworkPolicyPeer describes a peer to allow traffic to/from. Only certain combinations of fields are allowed

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexegressindextoindexipblock">ipBlock</a></b></td>
        <td>object</td>
        <td>
          ipBlock defines policy on a particular IPBlock. If this field is set then neither of the other fields can be.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexegressindextoindexnamespaceselector">namespaceSelector</a></b></td>
        <td>object</td>
        <td>
          namespaceSelector selects namespaces using cluster-scoped labels. This field follows standard label selector semantics; if present but empty, it selects all namespaces. 
 If podSelector is also set, then the NetworkPolicyPeer as a whole selects the pods matching podSelector in the namespaces selected by namespaceSelector. Otherwise it selects all pods in the namespaces selected by namespaceSelector.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexegressindextoindexpodselector">podSelector</a></b></td>
        <td>object</td>
        <td>
          podSelector is a label selector which selects pods. This field follows standard label selector semantics; if present but empty, it selects all pods. 
 If namespaceSelector is also set, then the NetworkPolicyPeer as a whole selects the pods matching podSelector in the Namespaces selected by NamespaceSelector. Otherwise it selects the pods matching podSelector in the policy's own namespace.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].egress[index].to[index].ipBlock



ipBlock defines policy on a particular IPBlock. If this field is set then neither of the other fields can be.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>cidr</b></td>
        <td>string</td>
        <td>
          cidr is a string representing the IPBlock Valid examples are "192.168.1.0/24" or "2001:db8::/64"<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>except</b></td>
        <td>[]string</td>
        <td>
          except is a slice of CIDRs that should not be included within an IPBlock Valid examples are "192.168.1.0/24" or "2001:db8::/64" Except values will be rejected if they are outside the cidr range<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].egress[index].to[index].namespaceSelector



namespaceSelector selects namespaces using cluster-scoped labels. This field follows standard label selector semantics; if present but empty, it selects all namespaces. 
 If podSelector is also set, then the NetworkPolicyPeer as a whole selects the pods matching podSelector in the namespaces selected by namespaceSelector. Otherwise it selects all pods in the namespaces selected by namespaceSelector.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexegressindextoindexnamespaceselectormatchexpressionsindex">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          matchExpressions is a list of label selector requirements. The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>matchLabels</b></td>
        <td>map[string]string</td>
        <td>
          matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].egress[index].to[index].namespaceSelector.matchExpressions[index]



A label selector requirement is a selector that contains values, a key, and an operator that relates the key and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>key</b></td>
        <td>string</td>
        <td>
          key is the label key that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          operator represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists and DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          values is an array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].egress[index].to[index].podSelector



podSelector is a label selector which selects pods. This field follows standard label selector semantics; if present but empty, it selects all pods. 
 If namespaceSelector is also set, then the NetworkPolicyPeer as a whole selects the pods matching podSelector in the Namespaces selected by NamespaceSelector. Otherwise it selects the pods matching podSelector in the policy's own namespace.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexegressindextoindexpodselectormatchexpressionsindex">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          matchExpressions is a list of label selector requirements. The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>matchLabels</b></td>
        <td>map[string]string</td>
        <td>
          matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].egress[index].to[index].podSelector.matchExpressions[index]



A label selector requirement is a selector that contains values, a key, and an operator that relates the key and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>key</b></td>
        <td>string</td>
        <td>
          key is the label key that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          operator represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists and DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          values is an array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].ingress[index]



NetworkPolicyIngressRule describes a particular set of traffic that is allowed to the pods matched by a NetworkPolicySpec's podSelector. The traffic must match both ports and from.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexingressindexfromindex">from</a></b></td>
        <td>[]object</td>
        <td>
          from is a list of sources which should be able to access the pods selected for this rule. Items in this list are combined using a logical OR operation. If this field is empty or missing, this rule matches all sources (traffic not restricted by source). If this field is present and contains at least one item, this rule allows traffic only if the traffic matches at least one item in the from list.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexingressindexportsindex">ports</a></b></td>
        <td>[]object</td>
        <td>
          ports is a list of ports which should be made accessible on the pods selected for this rule. Each item in this list is combined using a logical OR. If this field is empty or missing, this rule matches all ports (traffic not restricted by port). If this field is present and contains at least one item, then this rule allows traffic only if the traffic matches at least one port in the list.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].ingress[index].from[index]



NetworkPolicyPeer describes a peer to allow traffic to/from. Only certain combinations of fields are allowed

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexingressindexfromindexipblock">ipBlock</a></b></td>
        <td>object</td>
        <td>
          ipBlock defines policy on a particular IPBlock. If this field is set then neither of the other fields can be.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexingressindexfromindexnamespaceselector">namespaceSelector</a></b></td>
        <td>object</td>
        <td>
          namespaceSelector selects namespaces using cluster-scoped labels. This field follows standard label selector semantics; if present but empty, it selects all namespaces. 
 If podSelector is also set, then the NetworkPolicyPeer as a whole selects the pods matching podSelector in the namespaces selected by namespaceSelector. Otherwise it selects all pods in the namespaces selected by namespaceSelector.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexingressindexfromindexpodselector">podSelector</a></b></td>
        <td>object</td>
        <td>
          podSelector is a label selector which selects pods. This field follows standard label selector semantics; if present but empty, it selects all pods. 
 If namespaceSelector is also set, then the NetworkPolicyPeer as a whole selects the pods matching podSelector in the Namespaces selected by NamespaceSelector. Otherwise it selects the pods matching podSelector in the policy's own namespace.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].ingress[index].from[index].ipBlock



ipBlock defines policy on a particular IPBlock. If this field is set then neither of the other fields can be.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>cidr</b></td>
        <td>string</td>
        <td>
          cidr is a string representing the IPBlock Valid examples are "192.168.1.0/24" or "2001:db8::/64"<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>except</b></td>
        <td>[]string</td>
        <td>
          except is a slice of CIDRs that should not be included within an IPBlock Valid examples are "192.168.1.0/24" or "2001:db8::/64" Except values will be rejected if they are outside the cidr range<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].ingress[index].from[index].namespaceSelector



namespaceSelector selects namespaces using cluster-scoped labels. This field follows standard label selector semantics; if present but empty, it selects all namespaces. 
 If podSelector is also set, then the NetworkPolicyPeer as a whole selects the pods matching podSelector in the namespaces selected by namespaceSelector. Otherwise it selects all pods in the namespaces selected by namespaceSelector.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexingressindexfromindexnamespaceselectormatchexpressionsindex">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          matchExpressions is a list of label selector requirements. The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>matchLabels</b></td>
        <td>map[string]string</td>
        <td>
          matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].ingress[index].from[index].namespaceSelector.matchExpressions[index]



A label selector requirement is a selector that contains values, a key, and an operator that relates the key and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>key</b></td>
        <td>string</td>
        <td>
          key is the label key that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          operator represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists and DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          values is an array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].ingress[index].from[index].podSelector



podSelector is a label selector which selects pods. This field follows standard label selector semantics; if present but empty, it selects all pods. 
 If namespaceSelector is also set, then the NetworkPolicyPeer as a whole selects the pods matching podSelector in the Namespaces selected by NamespaceSelector. Otherwise it selects the pods matching podSelector in the policy's own namespace.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecnetworkpoliciesitemsindexingressindexfromindexpodselectormatchexpressionsindex">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          matchExpressions is a list of label selector requirements. The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>matchLabels</b></td>
        <td>map[string]string</td>
        <td>
          matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].ingress[index].from[index].podSelector.matchExpressions[index]



A label selector requirement is a selector that contains values, a key, and an operator that relates the key and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>key</b></td>
        <td>string</td>
        <td>
          key is the label key that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          operator represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists and DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          values is an array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.networkPolicies.items[index].ingress[index].ports[index]



NetworkPolicyPort describes a port to allow traffic on

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>endPort</b></td>
        <td>integer</td>
        <td>
          endPort indicates that the range of ports from port to endPort if set, inclusive, should be allowed by the policy. This field cannot be defined if the port field is not defined or if the port field is defined as a named (string) port. The endPort must be equal or greater than port.<br/>
          <br/>
            <i>Format</i>: int32<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>port</b></td>
        <td>int or string</td>
        <td>
          port represents the port on the given protocol. This can either be a numerical or named port on a pod. If this field is not provided, this matches all port names and numbers. If present, only traffic on the specified protocol AND port will be matched.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>protocol</b></td>
        <td>string</td>
        <td>
          protocol represents the protocol (TCP, UDP, or SCTP) which traffic must match. If not specified, this field defaults to TCP.<br/>
          <br/>
            <i>Default</i>: TCP<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.priorityClasses



Specifies the allowed priorityClasses assigned to the Tenant. Capsule assures that all Pods resources created in the Tenant can use only one of the allowed PriorityClasses. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>allowed</b></td>
        <td>[]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>allowedRegex</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.resourceQuotas



Specifies a list of ResourceQuota resources assigned to the Tenant. The assigned values are inherited by any namespace created in the Tenant. The Capsule operator aggregates ResourceQuota at Tenant level, so that the hard quota is never crossed for the given Tenant. This permits the Tenant owner to consume resources in the Tenant regardless of the namespace. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecresourcequotasitemsindex">items</a></b></td>
        <td>[]object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>scope</b></td>
        <td>enum</td>
        <td>
          Define if the Resource Budget should compute resource across all Namespaces in the Tenant or individually per cluster. Default is Tenant<br/>
          <br/>
            <i>Enum</i>: Tenant, Namespace<br/>
            <i>Default</i>: Tenant<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.resourceQuotas.items[index]



ResourceQuotaSpec defines the desired hard limits to enforce for Quota.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>hard</b></td>
        <td>map[string]int or string</td>
        <td>
          hard is the set of desired hard limits for each named resource. More info: https://kubernetes.io/docs/concepts/policy/resource-quotas/<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecresourcequotasitemsindexscopeselector">scopeSelector</a></b></td>
        <td>object</td>
        <td>
          scopeSelector is also a collection of filters like scopes that must match each object tracked by a quota but expressed using ScopeSelectorOperator in combination with possible values. For a resource to match, both scopes AND scopeSelector (if specified in spec), must be matched.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>scopes</b></td>
        <td>[]string</td>
        <td>
          A collection of filters that must match each object tracked by a quota. If not specified, the quota matches all objects.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.resourceQuotas.items[index].scopeSelector



scopeSelector is also a collection of filters like scopes that must match each object tracked by a quota but expressed using ScopeSelectorOperator in combination with possible values. For a resource to match, both scopes AND scopeSelector (if specified in spec), must be matched.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecresourcequotasitemsindexscopeselectormatchexpressionsindex">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          A list of scope selector requirements by scope of the resources.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.resourceQuotas.items[index].scopeSelector.matchExpressions[index]



A scoped-resource selector requirement is a selector that contains values, a scope name, and an operator that relates the scope name and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          Represents a scope's relationship to a set of values. Valid operators are In, NotIn, Exists, DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>scopeName</b></td>
        <td>string</td>
        <td>
          The name of the scope that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          An array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. This array is replaced during a strategic merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.serviceOptions



Specifies options for the Service, such as additional metadata or block of certain type of Services. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#tenantspecserviceoptionsadditionalmetadata">additionalMetadata</a></b></td>
        <td>object</td>
        <td>
          Specifies additional labels and annotations the Capsule operator places on any Service resource in the Tenant. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecserviceoptionsallowedservices">allowedServices</a></b></td>
        <td>object</td>
        <td>
          Block or deny certain type of Services. Optional.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#tenantspecserviceoptionsexternalips">externalIPs</a></b></td>
        <td>object</td>
        <td>
          Specifies the external IPs that can be used in Services with type ClusterIP. An empty list means no IPs are allowed. Optional.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.serviceOptions.additionalMetadata



Specifies additional labels and annotations the Capsule operator places on any Service resource in the Tenant. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>annotations</b></td>
        <td>map[string]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>labels</b></td>
        <td>map[string]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.serviceOptions.allowedServices



Block or deny certain type of Services. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>externalName</b></td>
        <td>boolean</td>
        <td>
          Specifies if ExternalName service type resources are allowed for the Tenant. Default is true. Optional.<br/>
          <br/>
            <i>Default</i>: true<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>loadBalancer</b></td>
        <td>boolean</td>
        <td>
          Specifies if LoadBalancer service type resources are allowed for the Tenant. Default is true. Optional.<br/>
          <br/>
            <i>Default</i>: true<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>nodePort</b></td>
        <td>boolean</td>
        <td>
          Specifies if NodePort service type resources are allowed for the Tenant. Default is true. Optional.<br/>
          <br/>
            <i>Default</i>: true<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.spec.serviceOptions.externalIPs



Specifies the external IPs that can be used in Services with type ClusterIP. An empty list means no IPs are allowed. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>allowed</b></td>
        <td>[]string</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr></tbody>
</table>


### Tenant.spec.storageClasses



Specifies the allowed StorageClasses assigned to the Tenant. Capsule assures that all PersistentVolumeClaim resources created in the Tenant can use only one of the allowed StorageClasses. Optional.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>allowed</b></td>
        <td>[]string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>allowedRegex</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### Tenant.status



Returns the observed state of the Tenant.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>size</b></td>
        <td>integer</td>
        <td>
          How many namespaces are assigned to the Tenant.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>state</b></td>
        <td>enum</td>
        <td>
          The operational state of the Tenant. Possible values are "Active", "Cordoned".<br/>
          <br/>
            <i>Enum</i>: Cordoned, Active<br/>
            <i>Default</i>: Active<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>namespaces</b></td>
        <td>[]string</td>
        <td>
          List of namespaces assigned to the Tenant.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>