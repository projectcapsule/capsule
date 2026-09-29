// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package invalidator

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
)

type recordingManager struct {
	manager.Manager
	runnables []manager.Runnable
}

func (m *recordingManager) Add(runnable manager.Runnable) error {
	m.runnables = append(m.runnables, runnable)
	return nil
}

func TestCacheInvalidationRunsOnEveryReplica(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	base, err := ctrl.NewManager(&rest.Config{Host: "https://127.0.0.1"}, ctrl.Options{
		Scheme: scheme, Metrics: metricsserver.Options{BindAddress: "0"},
	})
	require.NoError(t, err)
	// Build the real controller registration without starting clients or listeners.
	mgr := &recordingManager{Manager: base}
	r := &CacheInvalidator{}
	require.NoError(t, r.SetupWithManager(mgr, utils.ControllerOptions{ConfigurationName: "default"}, nil))
	require.Len(t, mgr.runnables, 2, "register both periodic invalidation and startup population")
	for _, runnable := range mgr.runnables {
		leaderRunnable, ok := runnable.(manager.LeaderElectionRunnable)
		require.True(t, ok)
		require.False(t, leaderRunnable.NeedLeaderElection(), "each process must maintain its own caches")
	}
}
