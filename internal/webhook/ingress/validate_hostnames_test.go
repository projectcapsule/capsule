// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ingress

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/sets"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

// tenantWithAllowedHostnames builds a minimal Tenant carrying the given exact
// allow-list and regex under spec.ingressOptions.allowedHostnames.
func tenantWithAllowedHostnames(exact []string, regex string) capsulev1beta2.Tenant {
	return capsulev1beta2.Tenant{
		Spec: capsulev1beta2.TenantSpec{
			IngressOptions: capsulev1beta2.IngressOptions{
				AllowedHostnames: &api.AllowedListSpec{
					Exact: exact,
					Regex: regex,
				},
			},
		},
	}
}

func TestValidateHostnames(t *testing.T) {
	t.Parallel()

	const (
		appsRegex = `^[a-z0-9-]{3,40}\.apps\.example\.com$`
	)

	tests := []struct {
		name string
		// tenant carries the allowed hostnames configuration under test.
		tenant capsulev1beta2.Tenant
		// hostnames are the Ingress hostnames being validated.
		hostnames []string
		// wantErr is true when the hostnames must be denied.
		wantErr bool
		// wantDenied lists hostnames that must appear in the denial message.
		wantDenied []string
		// wantAbsent lists hostnames that must NOT appear as denied (e.g. valid
		// via regex but outside the exact list).
		wantAbsent []string
	}{
		{
			name:      "no allowed hostnames configured allows everything",
			tenant:    capsulev1beta2.Tenant{},
			hostnames: []string{"anything.example.com"},
			wantErr:   false,
		},
		{
			name:      "empty hostname set is allowed",
			tenant:    tenantWithAllowedHostnames([]string{"a.example.com"}, ""),
			hostnames: nil,
			wantErr:   false,
		},
		{
			name:      "all hostnames in exact list are allowed",
			tenant:    tenantWithAllowedHostnames([]string{"a.example.com", "b.example.com"}, ""),
			hostnames: []string{"a.example.com", "b.example.com"},
			wantErr:   false,
		},
		{
			name:       "hostname outside exact list without regex is denied",
			tenant:     tenantWithAllowedHostnames([]string{"a.example.com"}, ""),
			hostnames:  []string{"a.example.com", "c.example.com"},
			wantErr:    true,
			wantDenied: []string{"c.example.com"},
		},
		{
			name:      "hostnames matching regex are allowed",
			tenant:    tenantWithAllowedHostnames(nil, `.*\.clastix\.io`),
			hostnames: []string{"foo.clastix.io", "bar.clastix.io"},
			wantErr:   false,
		},
		{
			name:      "mixed exact and regex hostnames are allowed",
			tenant:    tenantWithAllowedHostnames([]string{"a.example.com"}, `.*\.clastix\.io`),
			hostnames: []string{"a.example.com", "foo.clastix.io"},
			wantErr:   false,
		},
		{
			name:       "hostname not matching regex is denied",
			tenant:     tenantWithAllowedHostnames(nil, `.*\.clastix\.io`),
			hostnames:  []string{"foo.example.com"},
			wantErr:    true,
			wantDenied: []string{"foo.example.com"},
		},
		{
			name:   "denies only the hostname that is neither in the exact list nor matches the regex",
			tenant: tenantWithAllowedHostnames([]string{"allowed.example.com"}, appsRegex),
			hostnames: []string{
				"allowed.example.com",  // allowed via exact
				"web.apps.example.com", // allowed via regex
				"denied.example.com",   // denied: neither exact nor regex
			},
			wantErr:    true,
			wantDenied: []string{"denied.example.com"},
			wantAbsent: []string{"web.apps.example.com"},
		},
		{
			name:       "invalid regex denies hostnames outside the exact list",
			tenant:     tenantWithAllowedHostnames([]string{"a.example.com"}, "("),
			hostnames:  []string{"a.example.com", "b.example.com"},
			wantErr:    true,
			wantDenied: []string{"b.example.com"},
		},
		{
			name:      "invalid regex is ignored when every hostname is in the exact list",
			tenant:    tenantWithAllowedHostnames([]string{"a.example.com"}, "("),
			hostnames: []string{"a.example.com"},
			wantErr:   false,
		},
	}

	h := &hostnames{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := h.validateHostnames(tt.tenant, sets.New[string](tt.hostnames...))

			if tt.wantErr && err == nil {
				t.Fatalf("expected hostnames %v to be denied, got no error", tt.hostnames)
			}

			if !tt.wantErr {
				if err != nil {
					t.Fatalf("expected hostnames %v to be allowed, got error: %v", tt.hostnames, err)
				}

				return
			}

			msg := err.Error()

			for _, denied := range tt.wantDenied {
				if !strings.Contains(msg, denied) {
					t.Errorf("expected denial message to mention %q, got: %s", denied, msg)
				}
			}

			for _, absent := range tt.wantAbsent {
				if strings.Contains(msg, absent) {
					t.Errorf("did not expect denial message to mention allowed hostname %q, got: %s", absent, msg)
				}
			}
		})
	}
}

// TestValidateHostnamesDeterministic guards against the historical
// non-deterministic bug: because the hostname set has a randomized iteration
// order, validation of the same input must always produce the same result.
func TestValidateHostnamesDeterministic(t *testing.T) {
	t.Parallel()

	tenant := tenantWithAllowedHostnames(
		[]string{"allowed.example.com"},
		`^[a-z0-9-]{3,40}\.apps\.example\.com$`,
	)

	hostnameSet := sets.New[string](
		"allowed.example.com",
		"web.apps.example.com",
		"denied.example.com",
	)

	h := &hostnames{}

	for i := 0; i < 50; i++ {
		err := h.validateHostnames(tenant, hostnameSet)
		if err == nil {
			t.Fatalf("iteration %d: expected denial, got no error", i)
		}

		msg := err.Error()
		if !strings.Contains(msg, "denied.example.com") {
			t.Fatalf("iteration %d: expected message to mention denied.example.com, got: %s", i, msg)
		}

		if strings.Contains(msg, "web.apps.example.com") {
			t.Fatalf("iteration %d: message wrongly mentions regex-allowed hostname: %s", i, msg)
		}
	}
}
