package resourcepool

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

// NamespacesReference defines the indexer logic for GlobalResourceQuota namespaces.
type NamespacesReference struct {
	Obj client.Object
}

func (o NamespacesReference) Object() client.Object {
	return o.Obj
}

func (o NamespacesReference) Field() string {
	return ".status.namespaces"
}

//nolint:forcetypeassert
func (o NamespacesReference) Func() client.IndexerFunc {
	return func(object client.Object) []string {
		grq, ok := object.(*capsulev1beta2.ResourcePool)
		if !ok {
			return nil
		}

		namespaces := make([]string, 0, len(grq.Status.Claims))
		for ns := range grq.Status.Claims {
			namespaces = append(namespaces, ns)
		}

		return namespaces
	}
}
