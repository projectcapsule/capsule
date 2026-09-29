// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	resourcesv1 "k8s.io/api/resource/v1"
	resourcesv1beta2 "k8s.io/api/resource/v1beta2"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
)

// discoverDeviceClass selects the optional class API once, before starting the
// watch. Kubernetes 1.33 serves v1beta2; prefer v1 when the server supports it.
// Discovery failures disable status tracking until restart, as for other optional
// class APIs. Admission continues to authorize classes independently of this watch.
func (r *Manager) discoverDeviceClass(mapper meta.RESTMapper) client.Object {
	mapping, err := gvk.PreferredRESTMapping(mapper,
		resourcesv1.SchemeGroupVersion.WithKind("DeviceClass").GroupKind(),
		resourcesv1.SchemeGroupVersion.Version, resourcesv1beta2.SchemeGroupVersion.Version,
	)
	if meta.IsNoMatchError(err) {
		return nil
	}

	if err != nil {
		r.Log.Error(err, "unable to discover optional DeviceClass API; DeviceClass status tracking disabled until restart")

		return nil
	}

	r.classes.deviceVersion = mapping.GroupVersionKind.Version
	if r.classes.deviceVersion == resourcesv1beta2.SchemeGroupVersion.Version {
		return &resourcesv1beta2.DeviceClass{}
	}

	return &resourcesv1.DeviceClass{}
}
