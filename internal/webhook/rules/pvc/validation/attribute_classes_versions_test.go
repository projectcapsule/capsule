// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

// versionedHandler returns a handler whose clock the test moves.
func versionedHandler() (handlers.Handler, func(time.Duration)) {
	now := time.Unix(1_700_000_000, 0)
	handler := newAttributeClassHandler(nil, cache.NewRegexCache(), cache.NewLabelSelectorCache(), classCompiler, func() time.Time { return now })

	return handler, func(d time.Duration) { now = now.Add(d) }
}

// Kubernetes 1.33 serves VolumeAttributesClass only as storage.k8s.io/v1beta1,
// behind a feature gate, and 1.34 and later serve v1.
func TestVolumeAttributesClassServedVersions(t *testing.T) {
	allowCustomer := attributeClassRule(rules.ActionTypeAllow, customerClasses)
	denyCustomer := attributeClassRule(rules.ActionTypeDeny, customerClasses)
	unlabeled := attributeClassRule(rules.ActionTypeAllow, rules.StorageClassMatch{
		MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "env", Operator: metav1.LabelSelectorOpDoesNotExist}},
	})

	for _, tc := range []struct {
		name              string
		unserved          []string
		rule              *rules.NamespaceRuleBodyNamespace
		class             string
		denied            string
		classReads, betas int64
	}{
		{name: "v1 is read first", rule: allowCustomer, class: "gold", classReads: 1},
		{name: "a beta-only API matches an allow selector", unserved: []string{"v1"}, rule: allowCustomer, class: "gold", classReads: 2, betas: 1},
		{name: "a beta-only API matches a deny selector", unserved: []string{"v1"}, rule: denyCustomer, class: "gold", classReads: 2, betas: 1, denied: "is denied by namespace rule"},
		{name: "a beta-only API without the class", unserved: []string{"v1"}, rule: allowCustomer, class: "ghost", classReads: 2, betas: 1, denied: notAllowedAt("ghost", attributeClassPath)},
		// A class without labels exists and matches DoesNotExist; a missing class matches nothing.
		{name: "a class without labels from a beta-only API", unserved: []string{"v1"}, rule: unlabeled, class: "silver", classReads: 2, betas: 1},
		{name: "a missing class from a beta-only API", unserved: []string{"v1"}, rule: unlabeled, class: "ghost", classReads: 2, betas: 1, denied: notAllowedAt("ghost", attributeClassPath)},
		// A cluster without the API has no classes for a selector to match.
		{name: "no served API misses an allow selector", unserved: []string{"v1", "v1beta1"}, rule: allowCustomer, class: "gold", classReads: 2, betas: 1, denied: notAllowedAt("gold", attributeClassPath)},
		{name: "no served API misses a deny selector", unserved: []string{"v1", "v1beta1"}, rule: denyCustomer, class: "gold", classReads: 2, betas: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cl, decoder := singleClassProfile(t, tc.rule)
			cl.unserved = tc.unserved
			handler, _ := versionedHandler()

			response := runClaim(t.Context(), handler, cl, decoder, newRecorder(), claimRequest(t, newClaim("tenant-a", new(tc.class)), nil))
			if tc.denied == "" {
				require.Nil(t, response)
			} else {
				requireDenied(t, response, tc.denied)
			}

			require.Equal(t, tc.classReads, cl.classGets.Load())
			require.Equal(t, tc.betas, cl.betaGets.Load())
		})
	}
}

// Reading a version the cluster doesn't serve costs the REST mapper a discovery
// request, so it is skipped for a while.
func TestVolumeAttributesClassUnservedVersionsAreSkipped(t *testing.T) {
	request := func(t *testing.T, cl *classClient, decoder admission.Decoder, handler handlers.Handler, class string) {
		t.Helper()

		require.Nil(t, runClaim(t.Context(), handler, cl, decoder, newRecorder(), claimRequest(t, newClaim("tenant-a", new(class)), nil)))
	}

	t.Run("a beta-only API", func(t *testing.T) {
		cl, decoder := singleClassProfile(t, attributeClassRule(rules.ActionTypeAllow, customerClasses))
		cl.unserved = []string{"v1"}
		handler, advance := versionedHandler()

		request(t, cl, decoder, handler, "gold")
		require.Equal(t, []int64{2, 1}, []int64{cl.classGets.Load(), cl.betaGets.Load()})

		// Another class, read in another request, goes to v1beta1 directly.
		advance(unservedVersionTTL - time.Second)
		cl.classGets.Store(0)
		cl.betaGets.Store(0)
		request(t, cl, decoder, handler, "gold")
		require.Equal(t, []int64{1, 1}, []int64{cl.classGets.Load(), cl.betaGets.Load()})

		// After the TTL v1 is tried again, and is read once it is served.
		advance(2 * time.Second)
		cl.unserved = nil
		cl.classGets.Store(0)
		cl.betaGets.Store(0)
		request(t, cl, decoder, handler, "gold")
		require.Equal(t, []int64{1, 0}, []int64{cl.classGets.Load(), cl.betaGets.Load()})
	})

	t.Run("no served API", func(t *testing.T) {
		cl, decoder := singleClassProfile(t, attributeClassRule(rules.ActionTypeDeny, customerClasses))
		cl.unserved = []string{"v1", "v1beta1"}
		handler, advance := versionedHandler()

		request(t, cl, decoder, handler, "gold")
		require.Equal(t, int64(2), cl.classGets.Load())

		cl.classGets.Store(0)
		request(t, cl, decoder, handler, "gold")
		require.Zero(t, cl.classGets.Load(), "a cluster without the API is not asked again before the TTL")

		advance(unservedVersionTTL)
		request(t, cl, decoder, handler, "gold")
		require.Equal(t, int64(2), cl.classGets.Load())
	})

	t.Run("a failed read does not skip the version", func(t *testing.T) {
		cl, decoder := singleClassProfile(t, attributeClassRule(rules.ActionTypeAllow, customerClasses))
		cl.classErr = errors.New("apiserver unavailable")
		handler, _ := versionedHandler()

		response := runClaim(t.Context(), handler, cl, decoder, newRecorder(), claimRequest(t, newClaim("tenant-a", new("gold")), nil))
		require.NotNil(t, response)
		require.Equal(t, int32(http.StatusInternalServerError), response.Result.Code)
		require.Zero(t, cl.betaGets.Load(), "an error other than an unserved API must not fall back")

		cl.classErr = nil
		cl.classGets.Store(0)
		request(t, cl, decoder, handler, "gold")
		require.Equal(t, []int64{1, 0}, []int64{cl.classGets.Load(), cl.betaGets.Load()})
	})
}
