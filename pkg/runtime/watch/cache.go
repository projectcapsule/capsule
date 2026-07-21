// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package watch

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	ctrlcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
)

// MetadataCacheFactory builds the per-watch metadata-only caches for the
// manager. Trigger informers must not share the controller cache: they strip
// heavy metadata via a cache-wide transform, and consumers of the shared cache
// rely on exactly those fields (managedFields drives SSA ownership reads
// during prune and adoption). The watch's label selector is pushed down to the
// apiserver, so only matching objects are streamed and cached. Each cache is
// started and stopped by the Manager; nothing else needs to run it.
func MetadataCacheFactory(clu cluster.Cluster) CacheFactory {
	return func(selector labels.Selector) (ctrlcache.Cache, error) {
		return ctrlcache.New(clu.GetConfig(), ctrlcache.Options{
			HTTPClient:           clu.GetHTTPClient(),
			Scheme:               clu.GetScheme(),
			Mapper:               clu.GetRESTMapper(),
			DefaultTransform:     TransformStripHeavyMetadata,
			DefaultLabelSelector: selector,
		})
	}
}

// TransformStripHeavyMetadata drops the metadata nobody can match a trigger
// on and which dominates a metadata-only object's size: managedFields
// (~25% of cached bytes measured) and the kubectl last-applied annotation
// (a full copy of the object body on kubectl-applied resources). Sinks match
// on labels, namespace and deletion timestamp only; those pass through
// untouched. Tombstones are unwrapped and their inner object stripped;
// non-objects pass through for the informer machinery to handle.
func TransformStripHeavyMetadata(in any) (any, error) {
	obj := metaObject(in)
	if obj == nil {
		return in, nil
	}

	obj.SetManagedFields(nil)

	ann := obj.GetAnnotations()
	if _, ok := ann[corev1.LastAppliedConfigAnnotation]; ok {
		delete(ann, corev1.LastAppliedConfigAnnotation)

		if len(ann) == 0 {
			ann = nil
		}

		obj.SetAnnotations(ann)
	}

	return in, nil
}
