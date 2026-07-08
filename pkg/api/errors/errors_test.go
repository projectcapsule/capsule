// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package errors_test

import (
	stderrors "errors"
	"strings"
	"testing"

	"github.com/projectcapsule/capsule/pkg/api"
	apierrors "github.com/projectcapsule/capsule/pkg/api/errors"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	corev1 "k8s.io/api/core/v1"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestAllowedValuesErrorMessages(t *testing.T) {
	t.Parallel()

	allowed := api.SelectorAllowedListSpec{
		AllowedListSpec: api.AllowedListSpec{
			Exact: []string{"fast", "slow"},
			Regex: "premium-.*",
		},
		LabelSelector: metav1.LabelSelector{MatchLabels: map[string]string{"class": "gold"}},
	}

	got := apierrors.AllowedValuesErrorMessage(allowed, "prefix: ")
	for _, want := range []string{
		"prefix:",
		"use one from the following list (fast, slow)",
		"use one matching the following regex (premium-.*)",
		"matching the label selector defined in the Tenant",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("AllowedValuesErrorMessage() = %q, missing %q", got, want)
		}
	}

	defaultMsg := apierrors.DefaultAllowedValuesErrorMessage(api.DefaultAllowedListSpec{SelectorAllowedListSpec: allowed}, "default: ")
	if !strings.Contains(defaultMsg, "premium-.*") {
		t.Fatalf("DefaultAllowedValuesErrorMessage() = %q, want regex detail", defaultMsg)
	}

	selectionMsg := apierrors.SelectionListWithDefaultErrorMessage(api.SelectionListWithDefaultSpec{
		SelectionListWithSpec: api.SelectionListWithSpec{
			LabelSelector: metav1.LabelSelector{MatchLabels: map[string]string{"class": "gold"}},
		},
	}, "selection: ")
	if !strings.Contains(selectionMsg, "matching the label selector defined in the Tenant") {
		t.Fatalf("SelectionListWithDefaultErrorMessage() = %q", selectionMsg)
	}
}

func TestErrorConstructors(t *testing.T) {
	t.Parallel()

	allowed := api.DefaultAllowedListSpec{SelectorAllowedListSpec: api.SelectorAllowedListSpec{
		AllowedListSpec: api.AllowedListSpec{Exact: []string{"allowed"}, Regex: "allowed-.*"},
	}}
	selectorAllowed := api.SelectorAllowedListSpec{AllowedListSpec: api.AllowedListSpec{Exact: []string{"allowed"}}}

	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "custom quota", err: apierrors.NewCustomResourceQuotaError("pods.v1", 3), want: "pods.v1"},
		{name: "device forbidden", err: apierrors.NewDeviceClassForbidden("gpu", selectorAllowed), want: "Device Class gpu is forbidden"},
		{name: "device undefined", err: apierrors.NewDeviceClassUndefined(selectorAllowed), want: "Selected DeviceClass is forbidden"},
		{name: "gateway class", err: apierrors.NewGatewayClassError("public", stderrors.New("missing")), want: "Failed to resolve Gateway Class public"},
		{name: "gateway", err: apierrors.NewGatewayError(gatewayv1.ObjectName("gw"), stderrors.New("missing")), want: "Failed to resolve Gateway gw"},
		{name: "gateway forbidden", err: apierrors.NewGatewayClassForbidden("public", allowed), want: "Gateway Class public is forbidden"},
		{name: "gateway undefined", err: apierrors.NewGatewayClassUndefined(allowed), want: "No gateway Class is forbidden"},
		{name: "ingress class", err: apierrors.NewIngressClassError("nginx", stderrors.New("missing")), want: "Failed to resolve Ingress Class nginx"},
		{name: "ingress forbidden", err: apierrors.NewIngressClassForbidden("nginx", allowed), want: "Ingress Class nginx is forbidden"},
		{name: "ingress collision", err: apierrors.NewIngressHostnameCollision("example.com"), want: "example.com is already used"},
		{name: "empty ingress hostname", err: apierrors.NewEmptyIngressHostname(api.AllowedListSpec{Exact: []string{"example.com"}, Regex: ".*\\.example\\.com"}), want: "empty hostname is not allowed"},
		{name: "ingress hostnames invalid", err: apierrors.NewIngressHostnamesNotValid([]string{"bad_host"}, []string{"other.com"}, api.AllowedListSpec{Exact: []string{"example.com"}}), want: "Hostnames [bad_host] are not valid"},
		{name: "ingress undefined", err: apierrors.NewIngressClassUndefined(allowed), want: "No Ingress Class is forbidden"},
		{name: "ingress not valid", err: apierrors.NewIngressClassNotValid("nginx", allowed), want: "Ingress Class nginx is forbidden"},
		{name: "namespace quota", err: apierrors.NewNamespaceQuotaExceededError(), want: "Cannot exceed Namespace quota"},
		{name: "node labels", err: apierrors.NewNodeLabelForbiddenError(&api.ForbiddenListSpec{Exact: []string{"node-role"}, Regex: "forbidden.*"}), want: "some labels are marked as forbidden"},
		{name: "node annotations", err: apierrors.NewNodeAnnotationForbiddenError(&api.ForbiddenListSpec{Exact: []string{"internal"}}), want: "some annotations are marked as forbidden"},
		{name: "priority class", err: apierrors.NewPriorityClassError("high", stderrors.New("missing")), want: "Failed to resolve Priority Class high"},
		{name: "pod metadata", err: apierrors.NewNoPodMetadata("pod"), want: "Skipping labels sync for pod"},
		{name: "missing registry", err: apierrors.NewMissingContainerRegistryError("nginx"), want: "missing repository"},
		{name: "registry forbidden", err: apierrors.NewContainerRegistryForbidden("docker.io/nginx", api.AllowedListSpec{Exact: []string{"ghcr.io"}, Regex: "registry.*"}), want: "registry is forbidden"},
		{name: "pull policy", err: apierrors.NewImagePullPolicyForbidden("Always", "app", []string{"IfNotPresent"}), want: "ImagePullPolicy Always"},
		{name: "pod priority forbidden", err: apierrors.NewPodPriorityClassForbidden("high", allowed), want: "Pod Priority Class high is forbidden"},
		{name: "pod runtime forbidden", err: apierrors.NewPodRuntimeClassForbidden("kata", allowed), want: "Pod Runtime Class kata is forbidden"},
		{name: "services metadata", err: apierrors.NewNoServicesMetadata("service"), want: "Skipping labels sync for service"},
		{name: "external service IP forbidden empty", err: apierrors.NewExternalServiceIPForbidden(nil), want: "does not allow the use of Service with external IPs"},
		{name: "external service IP forbidden cidr", err: apierrors.NewExternalServiceIPForbidden([]api.AllowedIP{"10.0.0.0/8"}), want: "10.0.0.0/8"},
		{name: "nodeport disabled", err: apierrors.NewNodePortDisabledError(), want: "NodePort service types are forbidden"},
		{name: "external name disabled", err: apierrors.NewExternalNameDisabledError(), want: "ExternalName service types are forbidden"},
		{name: "loadbalancer disabled", err: apierrors.NewLoadBalancerDisabled(), want: "LoadBalancer service types are forbidden"},
		{name: "storage class", err: apierrors.NewStorageClassError("fast", stderrors.New("missing")), want: "Failed to resolve Storage Class fast"},
		{name: "storage class not valid", err: apierrors.NewStorageClassNotValid(allowed), want: "A valid Storage Class must be used"},
		{name: "storage forbidden", err: apierrors.NewStorageClassForbidden("slow", allowed), want: "Storage Class slow is forbidden"},
		{name: "tenant object", err: apierrors.NewNonTenantObject("obj"), want: "doesn't belong to tenant"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.err.Error(); !strings.Contains(got, tt.want) {
				t.Fatalf("Error() = %q, want substring %q", got, tt.want)
			}
		})
	}
}

func TestEventedStorageErrors(t *testing.T) {
	t.Parallel()

	tests := []error{
		apierrors.NewMissingTenantPVLabelsError("pv-a", events.ActionValidationDenied),
		apierrors.NewCrossTenantPVMountError("pv-a", events.ActionValidationDenied),
		apierrors.NewPVSelectorError(events.ActionValidationDenied),
		apierrors.NewPvNotFoundError("pv-a", events.ActionValidationDenied),
	}

	for _, err := range tests {
		evented, ok := err.(apierrors.EventedError)
		if !ok {
			t.Fatalf("%T does not implement EventedError", err)
		}
		if evented.Reason() != events.ReasonCrossTenantReference {
			t.Fatalf("Reason() = %q, want %q", evented.Reason(), events.ReasonCrossTenantReference)
		}
		if evented.Action() != events.ActionValidationDenied {
			t.Fatalf("Action() = %q, want %q", evented.Action(), events.ActionValidationDenied)
		}
		if err.Error() == "" {
			t.Fatalf("Error() is empty")
		}
	}
}

func TestMiscErrors(t *testing.T) {
	t.Parallel()

	for _, err := range []error{
		apierrors.RunningInOutOfClusterModeError{},
		apierrors.CaNotYetValidError{},
		apierrors.CaExpiredError{},
	} {
		if err.Error() == "" {
			t.Fatalf("%T Error() is empty", err)
		}
	}
}

func TestIgnoreGone(t *testing.T) {
	t.Parallel()

	if !apierrors.IgnoreGone(nil) {
		t.Fatalf("IgnoreGone(nil) = false, want true")
	}
	if !apierrors.IgnoreGone(k8sapierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "pods"}, "missing")) {
		t.Fatalf("IgnoreGone(NotFound) = false, want true")
	}
	if !apierrors.IgnoreGone(&k8sapierrors.StatusError{ErrStatus: metav1.Status{
		Reason: metav1.StatusReasonForbidden,
		Details: &metav1.StatusDetails{Causes: []metav1.StatusCause{{
			Type: corev1.NamespaceTerminatingCause,
		}}},
	}}) {
		t.Fatalf("IgnoreGone(namespace terminating) = false, want true")
	}
	if !apierrors.IgnoreGone(stderrors.New("widget not found")) {
		t.Fatalf("IgnoreGone(string not found) = false, want true")
	}
	if apierrors.IgnoreGone(stderrors.New("boom")) {
		t.Fatalf("IgnoreGone(other error) = true, want false")
	}
}
