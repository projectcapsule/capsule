// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"fmt"
	"sort"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic/dynamicinformer"
	clientcache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/users"
)

// Identity is part of both stream sharing and event provenance. An empty account
// is the explicitly configured controller execution identity, not a fallback
// when an impersonated LIST/WATCH fails.
type resourceWatchIdentity struct {
	account   cache.Key
	transport capsulev1beta2.ServiceAccountClient
}

// Resolve through the same helpers as replication, with one configuration read
// for the whole planning pass/event rather than one read per candidate.
type watchConfiguration struct {
	configuration.Configuration

	properties capsulev1beta2.ServiceAccountClient
}

func (c watchConfiguration) ServiceAccountClientProperties() capsulev1beta2.ServiceAccountClient {
	return c.properties
}

func (r *ResourceWatches) watchProperties(ctx context.Context) (capsulev1beta2.ServiceAccountClient, error) {
	if r.configurationName == "" {
		return r.configuration.ServiceAccountClientProperties(), nil
	}

	object := &capsulev1beta2.CapsuleConfiguration{}
	if err := r.client.Get(ctx, client.ObjectKey{Name: r.configurationName}, object, client.UnsafeDisableDeepCopy); err != nil {
		return capsulev1beta2.ServiceAccountClient{}, fmt.Errorf("read watch execution configuration: %w", err)
	}

	return object.Spec.Impersonation, nil
}

func watchIdentity(source client.Object, props capsulev1beta2.ServiceAccountClient) resourceWatchIdentity {
	var account *meta.NamespacedRFC1123ObjectReferenceWithNamespace

	cfg := watchConfiguration{properties: props}

	switch object := source.(type) {
	case *capsulev1beta2.TenantResource:
		account = namespacedServiceAccount(cfg, logr.Discard(), object)
	case *capsulev1beta2.GlobalTenantResource:
		account = globalServiceAccount(cfg, logr.Discard(), object)
	}

	if account == nil {
		return resourceWatchIdentity{}
	}

	// Defaults resolve the account but do not change the transport of an
	// already resolved account. Retire streams only for relevant changes.
	props.GlobalDefaultServiceAccount = ""
	props.GlobalDefaultServiceAccountNamespace = ""
	props.TenantDefaultServiceAccount = ""

	return resourceWatchIdentity{account: cache.Key{Namespace: account.Namespace.String(), Name: account.Name.String()}, transport: props}
}

func resourceWatchKeys(resource schema.GroupVersionResource, identities map[resourceWatchIdentity]map[string]struct{}) []resourceWatchKey {
	var keys []resourceWatchKey

	for identity, namespaces := range identities {
		if _, global := namespaces[""]; global {
			keys = append(keys, resourceWatchKey{resource: resource, identity: identity})

			// A namespaced execution identity may be allowed to watch one
			// namespace but forbidden to list/watch cluster-wide. Its global
			// subscription must not retire permitted local streams.
			if identity.account == (cache.Key{}) {
				continue
			}
		}

		for namespace := range namespaces {
			if namespace == "" {
				continue
			}

			keys = append(keys, resourceWatchKey{resource: resource, namespace: namespace, identity: identity})
		}
	}

	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]

		return a.identity.account.Namespace+"/"+a.identity.account.Name+"/"+a.namespace < b.identity.account.Namespace+"/"+b.identity.account.Name+"/"+b.namespace
	})

	return keys
}

func (r *ResourceWatches) resourceInformer(ctx context.Context, key resourceWatchKey, kind schema.GroupVersionKind) (clientcache.SharedIndexInformer, error) {
	if key.identity.account == (cache.Key{}) {
		return dynamicinformer.NewFilteredDynamicInformer(r.dynamic, key.resource, key.namespace, 0, clientcache.Indexers{}, nil).Informer(), nil
	}

	base, err := r.configuration.ServiceAccountClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("load watch execution configuration: %w", err)
	}

	// Clients have the bounded stream lifetime. Reusing a process-wide client
	// keyed only by account could retain a superseded impersonation endpoint.
	c, err := users.ImpersonatedKubernetesClientForServiceAccount(base, r.client.Scheme(), meta.NamespacedRFC1123ObjectReferenceWithNamespace{
		Name: meta.RFC1123Name(key.identity.account.Name), Namespace: meta.RFC1123SubdomainName(key.identity.account.Namespace),
	})
	if err != nil {
		return nil, err
	}

	watched, ok := c.(client.WithWatch)
	if !ok {
		return nil, fmt.Errorf("execution client does not support watches")
	}

	list := func() *unstructured.UnstructuredList {
		object := &unstructured.UnstructuredList{}
		object.SetGroupVersionKind(kind.GroupVersion().WithKind(kind.Kind + "List"))

		return object
	}

	lw := &clientcache.ListWatch{
		ListWithContextFunc: func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
			object := list()
			err := watched.List(ctx, object, &client.ListOptions{Namespace: key.namespace, Raw: &options})

			return object, err
		},
		WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
			return watched.Watch(ctx, list(), &client.ListOptions{Namespace: key.namespace, Raw: &options})
		},
	}

	return clientcache.NewSharedIndexInformer(lw, &unstructured.Unstructured{}, 0, clientcache.Indexers{}), nil
}
