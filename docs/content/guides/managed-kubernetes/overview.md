# Capsule on Managed Kubernetes
Capsule Operator can be easily installed on a Managed Kubernetes Service. Since you do not have access to the Kubernetes APIs Server, you should check with the provider of the service:

- the default `cluster-admin` ClusterRole is accessible 
- the following Admission Webhooks are enabled on the APIs Server:
    - PodNodeSelector
    - LimitRanger
    - ResourceQuota
    - MutatingAdmissionWebhook
    - ValidatingAdmissionWebhook
