# Capsule over Managed Kubernetes
Capsule Operator can be easly installed on a Managed Kubernetes Service. Since in these services, you do not have access to the Kubernetes APIs Server, you should check with your service provider following pre-requisites:

- the default `cluster-admin` ClusterRole is accessible 
- the following Admission Webhooks are enabled on the APIs Server:
    - PodNodeSelector
    - LimitRanger
    - ResourceQuota
    - MutatingAdmissionWebhook
    - ValidatingAdmissionWebhook
    - PodSecurityPolicy (optional)
- a CNI plugin with support for Network Policies can be used.

* [AWS EKS](./aws-eks.md)
* CoAKS - Capsule over Azure Kubernetes Service
* Google Cloud GKE
* IBM Cloud
* OVH