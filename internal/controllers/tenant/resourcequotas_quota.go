// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	caperrors "github.com/projectcapsule/capsule/pkg/api/errors"
	capsuletenant "github.com/projectcapsule/capsule/pkg/tenant"
)

type legacyQuotaResource struct {
	key string
	gvr schema.GroupVersionResource
}

func legacyQuotaResources(tnt *capsulev1beta2.Tenant) []legacyQuotaResource {
	var resources []legacyQuotaResource

	for annotation := range tnt.Annotations {
		key, ok := strings.CutPrefix(annotation, capsulev1beta2.ResourceQuotaAnnotationPrefix+"/")
		if !ok {
			continue
		}

		kindGroup, version, ok := strings.Cut(key, "_")

		kind, group, valid := strings.Cut(kindGroup, ".")

		if !ok || !valid || kind == "" || version == "" {
			continue
		}

		resources = append(resources, legacyQuotaResource{key: key, gvr: schema.GroupVersionResource{Group: group, Version: version, Resource: kind}})
	}

	slices.SortFunc(resources, func(a, b legacyQuotaResource) int { return strings.Compare(a.key, b.key) })

	return resources
}

func (r *Manager) syncCustomResourceQuotaUsages(ctx context.Context, snapshot *capsulev1beta2.Tenant) error {
	if snapshot.DeletionTimestamp != nil || (len(legacyQuotaResources(snapshot)) == 0 && snapshot.Annotations[capsuletenant.LegacyQuotaReservationsAnnotation] == "") {
		return nil
	}

	reader := r.reader
	if reader == nil {
		reader = r.Client
	}
	// Fence the entire count with the version read BEFORE enumeration. A write
	// retry must recount, rather than apply an old count to a newer reservation.
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &capsulev1beta2.Tenant{}
		if err := reader.Get(ctx, client.ObjectKeyFromObject(snapshot), current); err != nil {
			return client.IgnoreNotFound(err)
		}

		if current.UID != snapshot.UID || current.DeletionTimestamp != nil {
			return nil
		}

		before := maps.Clone(current.Annotations)

		reservations, err := capsuletenant.ReadLegacyQuotaReservations(current.Annotations)
		if err != nil {
			return err
		}

		if err := r.recountLegacyQuotaResources(ctx, reader, current, reservations, legacyQuotaResources(current)); err != nil {
			return err
		}

		if err := capsuletenant.WriteLegacyQuotaReservations(current.Annotations, reservations); err != nil {
			return err
		}

		if maps.Equal(before, current.Annotations) {
			return nil
		}

		return r.Update(ctx, current)
	})
}

func (r *Manager) recountLegacyQuotaResources(ctx context.Context, reader client.Reader, current *capsulev1beta2.Tenant, reservations *capsuletenant.LegacyQuotaReservations, resources []legacyQuotaResource) error {
	configured := make(map[string]struct{}, len(resources))
	for _, res := range resources {
		configured[res.key] = struct{}{}
	}

	for key := range reservations.Pending {
		if _, ok := configured[key]; !ok {
			delete(reservations.Pending, key)
		}
	}

	if len(resources) == 0 {
		clear(reservations.Namespaces)

		return nil
	}

	namespaces, lookupErr := r.legacyQuotaNamespaces(ctx, reader, current, reservations)
	if lookupErr != nil {
		return lookupErr
	}

	for name, uid := range reservations.Namespaces {
		ns := namespaces[name]

		status := current.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{Name: name, UID: uid})

		if ns == nil || ns.UID != uid || (status != nil && status.UID == uid) {
			delete(reservations.Namespaces, name)
		}
	}

	dyn := r.DynamicClient
	if dyn == nil {
		var err error

		dyn, err = dynamic.NewForConfig(r.RESTConfig)
		if err != nil {
			return err
		}
	}

	counts := make([]legacyQuotaCount, len(resources))
	group, countContext := errgroup.WithContext(ctx)
	group.SetLimit(8)

	for i, res := range resources {
		group.Go(func() error {
			var countErr error

			counts[i], countErr = countLegacyQuotaResources(countContext, dyn, namespaces, res.gvr, reservations.Pending[res.key])

			return countErr
		})
	}

	if err := group.Wait(); err != nil {
		return err
	}

	for i, res := range resources {
		current.Annotations[capsulev1beta2.UsedAnnotationForResource(res.key)] = strconv.FormatInt(counts[i].used, 10)
		reservations.Pending[res.key] = counts[i].pending
	}

	return nil
}

// The owner index selects candidates independently of Tenant status. Pending
// reservations also supply namespaces not visible in that index yet. Each is
// checked against current UID-validated ownership using the API reader.
func (r *Manager) legacyQuotaNamespaces(ctx context.Context, reader client.Reader, tnt *capsulev1beta2.Tenant, reservations *capsuletenant.LegacyQuotaReservations) (map[string]*corev1.Namespace, error) {
	list := &corev1.NamespaceList{}
	if err := r.List(ctx, list, client.MatchingFields{capsuletenant.NamespaceOwnerReferenceIndexerFieldName: tnt.Name}); err != nil {
		return nil, fmt.Errorf("list legacy quota namespace candidates: %w", err)
	}

	candidates := make(map[string]struct{}, len(list.Items))
	for _, ns := range list.Items {
		candidates[ns.Name] = struct{}{}
	}

	for _, name := range tnt.Status.Namespaces {
		candidates[name] = struct{}{}
	}

	for _, status := range tnt.Status.Spaces {
		candidates[status.Name] = struct{}{}
	}

	for name := range reservations.Namespaces {
		candidates[name] = struct{}{}
	}

	for _, entries := range reservations.Pending {
		for _, entry := range entries {
			candidates[entry.ObjectRef.Namespace] = struct{}{}
		}
	}

	namespaces := make(map[string]*corev1.Namespace, len(candidates))

	for _, name := range slices.Sorted(maps.Keys(candidates)) {
		ns := &corev1.Namespace{}
		if err := reader.Get(ctx, client.ObjectKey{Name: name}, ns); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}

			return nil, err
		}

		if ns.DeletionTimestamp == nil && namespaceOwnedByTenant(ns, tnt) {
			namespaces[name] = ns
		}
	}

	return namespaces, nil
}

type (
	legacyQuotaObject struct{ namespace, name string }
	legacyQuotaCount  struct {
		used    int64
		pending []capsuletenant.LegacyQuotaReservation
	}
)

func countLegacyQuotaResources(ctx context.Context, dyn dynamic.Interface, namespaces map[string]*corev1.Namespace, gvr schema.GroupVersionResource, pending []capsuletenant.LegacyQuotaReservation) (legacyQuotaCount, error) {
	result := legacyQuotaCount{}
	// Retain identities only for pending requests, not for every counted object.
	wanted := make(map[legacyQuotaObject]struct{}, len(pending))

	materialized := make(map[legacyQuotaObject]types.UID, len(pending))

	for _, entry := range pending {
		wanted[legacyQuotaObject{entry.ObjectRef.Namespace, entry.ObjectRef.Name}] = struct{}{}
	}

	for _, name := range slices.Sorted(maps.Keys(namespaces)) {
		list, err := dyn.Resource(gvr).Namespace(name).List(ctx, metav1.ListOptions{})
		if err != nil {
			if caperrors.IgnoreGone(err) || apierrors.HasStatusCause(err, corev1.NamespaceTerminatingCause) {
				continue
			}

			return result, err
		}

		for _, obj := range list.Items {
			if obj.GetDeletionTimestamp() != nil {
				continue
			}

			result.used++

			key := legacyQuotaObject{name, obj.GetName()}
			if _, ok := wanted[key]; ok {
				materialized[key] = obj.GetUID()
			}
		}
	}

	now := metav1.Now()

	for _, entry := range pending {
		ns := namespaces[entry.ObjectRef.Namespace]
		if ns == nil || ns.UID != entry.NamespaceUID || (entry.ExpiresAt != nil && entry.ExpiresAt.Before(&now)) {
			continue
		}

		uid, found := materialized[legacyQuotaObject{entry.ObjectRef.Namespace, entry.ObjectRef.Name}]
		if found && (entry.ObjectRef.UID == "" || uid == entry.ObjectRef.UID) {
			continue
		}

		result.used++
		result.pending = append(result.pending, entry)
	}

	return result, nil
}
