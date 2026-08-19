// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

// Resolves the ServiceAccount the given replication resource must be replicated with,
// either declared by the resource itself or defaulted by the Capsule configuration.
// A nil result states no impersonation is required.
//
// The resolution is the sole difference between the replication resources, since the
// ServiceAccount reference they declare is scoped differently.
type serviceAccountResolver[T client.Object] func(
	cfg configuration.Configuration,
	log logr.Logger,
	obj T,
) *meta.NamespacedRFC1123ObjectReferenceWithNamespace

// Loads the client a replication resource must be replicated with, dealing with the
// optional ServiceAccount impersonation.
//
// The type parameter keeps the resolver bound to the resource it can actually resolve,
// as the ServiceAccount reference lives on the resource specific part of the spec.
type impersonatedClientLoader[T client.Object] struct {
	client        client.Client
	configuration configuration.Configuration
	impersonation *cache.ImpersonationCache
	resolve       serviceAccountResolver[T]
}

// Load returns the client along with the ServiceAccount identity it acts as.
// The identity is always resolved, even along an error and even when no impersonation is
// required, in which case it is the one of the controller itself: callers willing to
// report it on the status can do so unconditionally.
func (l impersonatedClientLoader[T]) Load(
	ctx context.Context,
	log logr.Logger,
	obj T,
) (client.Client, *meta.NamespacedRFC1123ObjectReferenceWithNamespace, error) {
	sa := l.resolve(l.configuration, log, obj)
	if sa == nil {
		// No impersonation required: the controller replicates with its own identity.
		name, namespace := configuration.ControllerServiceAccount()

		return l.client, &meta.NamespacedRFC1123ObjectReferenceWithNamespace{
			Name:      meta.RFC1123Name(name),
			Namespace: meta.RFC1123SubdomainName(namespace),
		}, nil
	}

	re, err := l.configuration.ServiceAccountClient(ctx)
	if err != nil {
		log.Error(err, "failed to load impersonated rest client")

		return nil, sa, err
	}

	log.V(5).Info("using impersonation client", "serviceaccount", sa.Name, "namespace", sa.Namespace)

	c, err := l.impersonation.LoadOrCreate(ctx, log, re, l.client.Scheme(), *sa)

	return c, sa, err
}

// Resolves the ServiceAccount of a GlobalTenantResource: being cluster scoped, it must
// declare the Namespace of the ServiceAccount along with its name.
func globalServiceAccount(
	cfg configuration.Configuration,
	log logr.Logger,
	tntResource *capsulev1beta2.GlobalTenantResource,
) *meta.NamespacedRFC1123ObjectReferenceWithNamespace {
	if sa := tntResource.Spec.ServiceAccount; sa != nil {
		name := sa.Name.String()
		ns := sa.Namespace.String()

		if name == "" || ns == "" {
			log.V(4).Info("serviceAccount reference is set but incomplete; ignoring",
				"name", name, "namespace", ns,
			)

			return nil
		}

		return &meta.NamespacedRFC1123ObjectReferenceWithNamespace{
			Name:      sa.Name,
			Namespace: sa.Namespace,
		}
	}

	props := cfg.ServiceAccountClientProperties()

	name := props.GlobalDefaultServiceAccount.String()
	ns := props.GlobalDefaultServiceAccountNamespace.String()

	nameSet := name != ""
	nsSet := ns != ""

	if nameSet != nsSet {
		log.V(2).Info("invalid config: global default service account requires both name and namespace",
			"name", name, "namespace", ns,
		)

		return nil
	}

	if !nameSet && !nsSet {
		return nil
	}

	return &meta.NamespacedRFC1123ObjectReferenceWithNamespace{
		Name:      props.GlobalDefaultServiceAccount,
		Namespace: props.GlobalDefaultServiceAccountNamespace,
	}
}

// Resolves the ServiceAccount of a TenantResource: being namespaced, the ServiceAccount
// is always expected to live in the very same Namespace of the resource, preventing a
// Tenant owner from impersonating an identity outside of its boundaries.
func namespacedServiceAccount(
	cfg configuration.Configuration,
	_ logr.Logger,
	tntResource *capsulev1beta2.TenantResource,
) *meta.NamespacedRFC1123ObjectReferenceWithNamespace {
	if tntResource.Spec.ServiceAccount != nil {
		return &meta.NamespacedRFC1123ObjectReferenceWithNamespace{
			Name:      tntResource.Spec.ServiceAccount.Name,
			Namespace: meta.RFC1123SubdomainName(tntResource.Namespace),
		}
	}

	props := cfg.ServiceAccountClientProperties()

	if props.TenantDefaultServiceAccount == "" {
		return nil
	}

	return &meta.NamespacedRFC1123ObjectReferenceWithNamespace{
		Name:      props.TenantDefaultServiceAccount,
		Namespace: meta.RFC1123SubdomainName(tntResource.Namespace),
	}
}
