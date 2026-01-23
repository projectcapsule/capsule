// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package events

const (
	// Generic.
	ReasonTenantResourceWriteOp string = "TenantResourceWriteOp"
	ReasonOverprovision         string = "Overprovisioned"
	ReasonCordoning             string = "Cordoned"
	// ForbiddenLabelReason used as reason string to deny forbidden labels.
	ReasonForbiddenLabel string = "ForbiddenLabel"
	// ForbiddenAnnotationReason used as reason string to deny forbidden annotations.
	ReasonForbiddenAnnotation string = "ForbiddenAnnotation"

	// Namespace.
	ReasonNamespaceHijack string = "ReasonNamespacePatch"

	// Tenant.
	ReasonTenantDefaulted     string = "TenantDefaulted"
	ReasonTenantAssigned      string = "TenantAssigned"
	ReasonInvalidTenantPrefix string = "InvalidTenantPrefix"

	// Classes.
	ReasonMissingStorageClass    string = "MissingStorageClass"
	ReasonForbiddenStorageClass  string = "ForbiddenStorageClass"
	ReasonForbiddenPriorityClass string = "ForbiddenPriorityClass"
	ReasonForbiddenRuntimeClass  string = "ForbiddenRuntimeClass"
	ReasonForbiddenIngressClass  string = "ForbiddenIngressClass"
	ReasonMissingIngressClass    string = "MissingIngressClass"
	ReasonForbiddenGatewayClass  string = "ForbiddenGatewayClass"
	ReasonMissingGatewayClass    string = "MissingGatewayClass"
	ReasonMissingDeviceClass     string = "MissingDeviceClass"
	ReasonForbiddenDeviceClass   string = "ForbiddenDeviceClass"

	// Pods.
	ReasonMissingFQCI                string = "MissingFQCI"
	ReasonForbiddenContainerRegistry string = "ForbiddenContainerRegistry"
	ReasonForbiddenPullPolicy        string = "ForbiddenPullPolicy"

	// Ingress.
	ReasonWildcardDenied           string = "WildcardDenied"
	ReasonIngressHostnameNotValid  string = "IngressHostnameNotValid"
	ReasonIngressHostnameEmpty     string = "IngressHostnameEmpty"
	ReasonIngressHostnameCollision string = "IngressHostnameCollision"

	// Services.
	ReasonForbiddenExternalServiceIP string = "ForbiddenExternalServiceIP"
	ReasonForbiddenLoadBalancer      string = "ForbiddenLoadBalancer"
	ReasonForbiddenExternalName      string = "ForbiddenExternalName"
	ReasonForbiddenNodePort          string = "ForbiddenNodePort"

	// ResourcePools.
	ReasonDisassociated string = "Disassociated"
)
