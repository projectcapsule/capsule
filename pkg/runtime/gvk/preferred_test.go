// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package gvk_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	apidiscoveryv2 "k8s.io/api/apidiscovery/v2"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
)

func TestPreferredRESTMappingColdDiscovery(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		versions []string
		want     string
		fail     bool
	}{
		{name: "1.33 beta APIs", versions: []string{"v1beta2", "v1beta1"}, want: "v1beta2"},
		{name: "stable only", versions: []string{"v1"}, want: "v1"},
		{name: "prefer stable regardless of server ordering", versions: []string{"v1beta2", "v1"}, want: "v1"},
		{name: "unsupported versions", versions: []string{"v1beta1"}},
		{name: "DRA disabled"},
		{name: "discovery failure", fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			kind := schema.GroupKind{Group: "resource.k8s.io", Kind: "DeviceClass"}
			group := apidiscoveryv2.APIGroupDiscovery{ObjectMeta: metav1.ObjectMeta{Name: kind.Group}}
			for _, version := range tc.versions {
				group.Versions = append(group.Versions, apidiscoveryv2.APIVersionDiscovery{
					Version: version, Freshness: apidiscoveryv2.DiscoveryFreshnessCurrent,
					Resources: []apidiscoveryv2.APIResourceDiscovery{{
						Resource: "deviceclasses", Scope: apidiscoveryv2.ScopeCluster,
						ResponseKind: &metav1.GroupVersionKind{Group: kind.Group, Version: version, Kind: kind.Kind},
						Verbs:        []string{"get", "list", "watch"},
					}},
				})
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.fail {
					http.Error(w, "discovery unavailable", http.StatusServiceUnavailable)
					return
				}
				if r.URL.Path != "/api" && r.URL.Path != "/apis" {
					http.NotFound(w, r)
					return
				}
				response := apidiscoveryv2.APIGroupDiscoveryList{
					TypeMeta: metav1.TypeMeta{APIVersion: apidiscoveryv2.SchemeGroupVersion.String(), Kind: "APIGroupDiscoveryList"},
				}
				if r.URL.Path == "/api" {
					response.Items = []apidiscoveryv2.APIGroupDiscovery{{
						Versions: []apidiscoveryv2.APIVersionDiscovery{{Version: "v1", Freshness: apidiscoveryv2.DiscoveryFreshnessCurrent}},
					}}
				}
				if r.URL.Path == "/apis" && len(tc.versions) > 0 {
					response.Items = []apidiscoveryv2.APIGroupDiscovery{group}
				}
				w.Header().Set("Content-Type", "application/json;g=apidiscovery.k8s.io;v=v2;as=APIGroupDiscoveryList")
				if err := json.NewEncoder(w).Encode(response); err != nil {
					t.Errorf("encode discovery response: %v", err)
				}
			}))
			t.Cleanup(server.Close)
			mapper, err := apiutil.NewDynamicRESTMapper(&rest.Config{Host: server.URL}, server.Client())
			require.NoError(t, err)
			// Each first call exercises a cold mapper, including aggregated discovery.
			// Repeat to verify the same selection after its cache is populated.
			for range 2 {
				mapping, err := gvk.PreferredRESTMapping(mapper, kind, "v1", "v1beta2")
				switch {
				case tc.fail:
					require.Error(t, err)
					require.False(t, meta.IsNoMatchError(err), "discovery failures must not become optional-API skips")
				case tc.want == "":
					require.True(t, meta.IsNoMatchError(err), "expected absent supported API, got %v", err)
				default:
					require.NoError(t, err)
					require.Equal(t, tc.want, mapping.GroupVersionKind.Version)
				}
			}
		})
	}
}
