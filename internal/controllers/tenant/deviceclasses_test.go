// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	resourcesv1 "k8s.io/api/resource/v1"
	resourcesv1beta2 "k8s.io/api/resource/v1beta2"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

func TestDiscoverDeviceClass(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		versions []string
		want     string
	}{
		{name: "stable", versions: []string{"v1"}, want: "v1"},
		{name: "Kubernetes 1.33", versions: []string{"v1beta2"}, want: "v1beta2"},
		{name: "prefer stable", versions: []string{"v1beta2", "v1"}, want: "v1"},
		{name: "DRA unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var versions []schema.GroupVersion
			for _, version := range tc.versions {
				versions = append(versions, schema.GroupVersion{Group: resourcesv1.GroupName, Version: version})
			}
			mapper := meta.NewDefaultRESTMapper(versions)
			for _, version := range tc.versions {
				mapper.Add(schema.GroupVersionKind{Group: resourcesv1.GroupName, Version: version, Kind: "DeviceClass"}, meta.RESTScopeRoot)
			}
			manager := &Manager{}
			obj, err := manager.discoverDeviceClass(mapper)
			require.NoError(t, err)
			require.Equal(t, tc.want, manager.classes.deviceVersion)
			switch tc.want {
			case "v1":
				require.IsType(t, &resourcesv1.DeviceClass{}, obj)
			case "v1beta2":
				require.IsType(t, &resourcesv1beta2.DeviceClass{}, obj)
			default:
				require.Nil(t, obj)
			}
		})
	}
	err := errors.New("discovery unavailable")
	manager := &Manager{}
	_, got := manager.discoverDeviceClass(deviceClassErrorMapper{err: err})
	require.ErrorIs(t, got, err)
}

type deviceClassErrorMapper struct {
	meta.RESTMapper
	err error
}

func (m deviceClassErrorMapper) RESTMappings(schema.GroupKind, ...string) ([]*meta.RESTMapping, error) {
	return nil, m.err
}

func TestDeviceClassStatusAcrossVersions(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"v1", "v1beta2"} {
		t.Run(version, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, capsulev1beta2.AddToScheme(scheme))
			require.NoError(t, resourcesv1.AddToScheme(scheme))
			require.NoError(t, resourcesv1beta2.AddToScheme(scheme))
			mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: resourcesv1.GroupName, Version: version}})
			mapper.Add(schema.GroupVersionKind{Group: resourcesv1.GroupName, Version: version, Kind: "DeviceClass"}, meta.RESTScopeRoot)
			manager := &Manager{}
			class, err := manager.discoverDeviceClass(mapper)
			require.NoError(t, err)
			class.SetName("gpu")
			class.SetLabels(map[string]string{"tenant": "a"})
			var objects []client.Object
			for _, name := range []string{"a", "b"} {
				objects = append(objects, &capsulev1beta2.Tenant{Name: name, Spec: capsulev1beta2.TenantSpec{
					DeviceClasses: &api.SelectorAllowedListSpec{LabelSelector: metav1.LabelSelector{MatchLabels: map[string]string{"tenant": name}}},
				}})
			}
			cl := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&capsulev1beta2.Tenant{}).WithObjects(append(objects, class)...).Build()
			manager.Client, manager.reader = cl, cl
			handler := manager.tenantClassEventHandler(manager.collectAvailableDeviceClasses)
			check := func(owner string) {
				t.Helper()
				for _, name := range []string{"a", "b"} {
					tenant := &capsulev1beta2.Tenant{}
					require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Name: name}, tenant))
					if name == owner {
						require.Equal(t, []string{"gpu"}, tenant.Status.Classes.DeviceClasses)
					} else {
						require.Empty(t, tenant.Status.Classes.DeviceClasses)
					}
				}
			}
			handler.CreateFunc(t.Context(), event.TypedCreateEvent[client.Object]{Object: class}, nil)
			check("a")
			old := class.DeepCopyObject().(client.Object)
			class.SetLabels(map[string]string{"tenant": "b"})
			require.NoError(t, cl.Update(t.Context(), class))
			handler.UpdateFunc(t.Context(), event.TypedUpdateEvent[client.Object]{ObjectOld: old, ObjectNew: class}, nil)
			check("b")
			require.NoError(t, cl.Delete(t.Context(), class))
			handler.DeleteFunc(t.Context(), event.TypedDeleteEvent[client.Object]{Object: class}, nil)
			check("")
		})
	}
}
