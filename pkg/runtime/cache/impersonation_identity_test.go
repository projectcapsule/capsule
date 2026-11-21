// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/flowcontrol"

	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestImpersonationIdentityIncludesTrustAndAccountButNotAPIBudget(t *testing.T) {
	scheme := runtime.NewScheme()
	base := &rest.Config{Host: "https://cluster.example", BearerToken: "test-token", CAData: []byte("test-ca"), NextProtos: []string{"h2"}}
	account := meta.NamespacedRFC1123ObjectReferenceWithNamespace{Name: "replicator", Namespace: "tenant-a"}
	identity := ImpersonationIdentity(base, scheme, account)
	bootstrap := rest.CopyConfig(base)
	bootstrap.QPS = 50
	bootstrap.Burst = 100
	bootstrap.RateLimiter = flowcontrol.NewFakeAlwaysRateLimiter()
	require.Equal(t, identity, ImpersonationIdentity(bootstrap, scheme, account))
	for _, change := range []string{"host", "token", "ca", "namespace", "account", "scheme", "protocols"} {
		t.Run(change, func(t *testing.T) {
			other := rest.CopyConfig(base)
			nextAccount := account
			nextScheme := scheme
			switch change {
			case "host":
				other.Host = "https://other.example"
			case "token":
				other.BearerToken = "rotated-token"
			case "ca":
				other.CAData = []byte("rotated-ca")
			case "namespace":
				nextAccount.Namespace = "tenant-b"
			case "account":
				nextAccount.Name = "other"
			case "scheme":
				nextScheme = runtime.NewScheme()
			case "protocols":
				other.NextProtos = []string{"http/1.1"}
			}
			require.NotEqual(t, identity, ImpersonationIdentity(other, nextScheme, nextAccount))
		})
	}
	base.CAData[0] = 'X'
	require.NotEqual(t, identity, ImpersonationIdentity(base, scheme, account), "published identities must not borrow credential slices")
	require.Nil(t, ImpersonationIdentity(nil, scheme, account))
}
