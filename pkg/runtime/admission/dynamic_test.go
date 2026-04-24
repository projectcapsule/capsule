// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission_test

import (
	"bytes"
	"testing"

	"github.com/projectcapsule/capsule/pkg/runtime/admission"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
)

func TestDynamicClientWithPath_EmptyPath_NoChange(t *testing.T) {
	t.Parallel()

	origURL := "https://example.com"
	in := admissionregistrationv1.WebhookClientConfig{
		URL:      &origURL,
		CABundle: []byte("ca"),
	}

	out := admission.DynamicClientWithPath(in, "")

	if out.URL == nil || *out.URL != origURL {
		t.Fatalf("URL changed unexpectedly: got=%v want=%q", ptrStr(out.URL), origURL)
	}
	if out.Service != nil {
		t.Fatalf("Service changed unexpectedly: got=%#v want=nil", out.Service)
	}
	if !bytes.Equal(out.CABundle, in.CABundle) {
		t.Fatalf("CABundle changed unexpectedly: got=%q want=%q", out.CABundle, in.CABundle)
	}
}

func TestDynamicClientWithPath_URLMode_AppendsNormalizedPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		base string
		path string
		want string
	}{
		{"no_trailing_slash_path_with_leading", "https://example.com", "/validate", "https://example.com/validate"},
		{"no_trailing_slash_path_without_leading", "https://example.com", "validate", "https://example.com/validate"},
		{"trailing_slash_base", "https://example.com/", "/validate", "https://example.com/validate"},
		{"multiple_slashes_path", "https://example.com/", "///a//b/", "https://example.com/a/b"},
		{"root_path", "https://example.com/", "/", "https://example.com/"},
		{"only_slashes_path", "https://example.com", "////", "https://example.com/"},
		{"dot_segments", "https://example.com", "/a/./b/../c", "https://example.com/a/c"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			inURL := tt.base
			in := admissionregistrationv1.WebhookClientConfig{
				URL: &inURL,
			}

			out := admission.DynamicClientWithPath(in, tt.path)

			if out.URL == nil {
				t.Fatalf("URL is nil, want %q", tt.want)
			}
			if *out.URL != tt.want {
				t.Fatalf("URL = %q, want %q", *out.URL, tt.want)
			}
		})
	}
}

func TestDynamicClientWithPath_URLMode_DoesNotMutateInput(t *testing.T) {
	t.Parallel()

	orig := "https://example.com/"
	in := admissionregistrationv1.WebhookClientConfig{URL: &orig}

	out := admission.DynamicClientWithPath(in, "/validate")

	// output should differ
	if out.URL == nil || *out.URL != "https://example.com/validate" {
		t.Fatalf("unexpected output URL: got=%v", ptrStr(out.URL))
	}

	// input must remain unchanged
	if in.URL == nil || *in.URL != "https://example.com/" {
		t.Fatalf("input was mutated: in.URL=%v", ptrStr(in.URL))
	}
}

func TestDynamicClientWithPath_ServiceMode_SetsServicePath(t *testing.T) {
	t.Parallel()

	in := admissionregistrationv1.WebhookClientConfig{
		Service: &admissionregistrationv1.ServiceReference{
			Namespace: "ns",
			Name:      "svc",
		},
	}

	out := admission.DynamicClientWithPath(in, "validate")

	if out.Service == nil || out.Service.Path == nil {
		t.Fatalf("Service/Path not set: %#v", out.Service)
	}
	if *out.Service.Path != "/validate" {
		t.Fatalf("Service.Path = %q, want %q", *out.Service.Path, "/validate")
	}

	// other fields preserved
	if out.Service.Namespace != "ns" || out.Service.Name != "svc" {
		t.Fatalf("Service fields changed unexpectedly: %#v", out.Service)
	}
}

func TestDynamicClientWithPath_ServiceMode_DoesNotMutateInputService(t *testing.T) {
	t.Parallel()

	in := admissionregistrationv1.WebhookClientConfig{
		Service: &admissionregistrationv1.ServiceReference{
			Namespace: "ns",
			Name:      "svc",
		},
	}

	// capture pointer identity
	origSvcPtr := in.Service

	out := admission.DynamicClientWithPath(in, "/validate")

	if out.Service == nil || out.Service.Path == nil || *out.Service.Path != "/validate" {
		t.Fatalf("unexpected out.Service: %#v", out.Service)
	}

	// Must not mutate input's service struct
	if in.Service == nil {
		t.Fatalf("input service became nil")
	}
	if in.Service.Path != nil {
		t.Fatalf("input service was mutated; expected Path=nil, got=%q", *in.Service.Path)
	}

	// Additionally, output must not reuse the same *ServiceReference pointer
	// (since we copy it to avoid aliasing).
	if out.Service == origSvcPtr {
		t.Fatalf("output service pointer aliases input; expected copy")
	}
}

func TestDynamicClientWithPath_URLTakesPrecedenceOverService(t *testing.T) {
	t.Parallel()

	origURL := "https://example.com/"
	in := admissionregistrationv1.WebhookClientConfig{
		URL: &origURL,
		Service: &admissionregistrationv1.ServiceReference{
			Namespace: "ns",
			Name:      "svc",
		},
	}

	out := admission.DynamicClientWithPath(in, "/validate")

	// URL mode should win
	if out.URL == nil || *out.URL != "https://example.com/validate" {
		t.Fatalf("URL = %v, want %q", ptrStr(out.URL), "https://example.com/validate")
	}

	// Ensure we did not set Service.Path in output when URL used (function returns early)
	// Service itself is shallow-copied, so it will be the same pointer as input here.
	// Importantly: it must not have been mutated.
	if out.Service == nil {
		t.Fatalf("Service unexpectedly nil; function should keep it as-is in URL mode")
	}
	if out.Service.Path != nil {
		t.Fatalf("Service.Path was unexpectedly set in URL mode: %q", *out.Service.Path)
	}
}

func TestDynamicClientWithPath_NoURLNoService_NoPanicNoChange(t *testing.T) {
	t.Parallel()

	in := admissionregistrationv1.WebhookClientConfig{}
	out := admission.DynamicClientWithPath(in, "/validate")

	// No URL/Service to apply to => should be unchanged other than shallow copy.
	if out.URL != nil {
		t.Fatalf("expected URL nil, got %v", ptrStr(out.URL))
	}
	if out.Service != nil {
		t.Fatalf("expected Service nil, got %#v", out.Service)
	}
}

func TestDynamicClientWithPath_PreservesCABundle(t *testing.T) {
	t.Parallel()

	origURL := "https://example.com"
	in := admissionregistrationv1.WebhookClientConfig{
		URL:      &origURL,
		CABundle: []byte("my-ca"),
	}

	out := admission.DynamicClientWithPath(in, "/validate")

	if !bytes.Equal(out.CABundle, []byte("my-ca")) {
		t.Fatalf("CABundle not preserved: got=%q want=%q", out.CABundle, "my-ca")
	}
}

func TestDynamicClientWithPath_Idempotent(t *testing.T) {
	t.Parallel()

	origURL := "https://example.com/"
	in := admissionregistrationv1.WebhookClientConfig{URL: &origURL}

	first := admission.DynamicClientWithPath(in, "/a//b/")
	second := admission.DynamicClientWithPath(first, "/a//b/")

	if first.URL == nil || second.URL == nil {
		t.Fatalf("unexpected nil URL: first=%v second=%v", ptrStr(first.URL), ptrStr(second.URL))
	}
	if *first.URL != *second.URL {
		t.Fatalf("not idempotent: first=%q second=%q", *first.URL, *second.URL)
	}
}

func ptrStr(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}
