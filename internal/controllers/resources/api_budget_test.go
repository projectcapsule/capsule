// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/flowcontrol"
)

func TestReplicationAPIBudgetReservesBootstrapCapacity(t *testing.T) {
	base := &rest.Config{QPS: 0.001, Burst: 30}
	background, bootstrap := ReplicationAPIConfigs(base)
	require.Nil(t, base.RateLimiter, "the caller's configuration remains unchanged")
	require.Nil(t, background.RateLimiter, "background limits retain their existing semantics")
	require.Equal(t, base.QPS, bootstrap.RateLimiter.QPS())
	for range 30 {
		require.True(t, bootstrap.RateLimiter.TryAccept())
	}
	require.False(t, bootstrap.RateLimiter.TryAccept(), "bootstrap identities share one bounded bucket")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, bootstrap.RateLimiter.Wait(ctx), context.Canceled)
}

func TestReplicationAPIBudgetDefaultsUnlimitedAndCustom(t *testing.T) {
	_, bootstrap := ReplicationAPIConfigs(&rest.Config{})
	require.Equal(t, rest.DefaultQPS, bootstrap.RateLimiter.QPS())
	background, bootstrap := ReplicationAPIConfigs(&rest.Config{QPS: -1})
	require.Nil(t, background.RateLimiter)
	require.Nil(t, bootstrap.RateLimiter)
	custom := flowcontrol.NewFakeAlwaysRateLimiter()
	background, bootstrap = ReplicationAPIConfigs(&rest.Config{QPS: 10, Burst: 10, RateLimiter: custom})
	require.Same(t, custom, background.RateLimiter)
	require.Same(t, custom, bootstrap.RateLimiter)
}
