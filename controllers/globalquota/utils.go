package globalquota

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	capsuleutils "github.com/projectcapsule/capsule/pkg/utils"
)

// Get all matching namespaces (just names)
func GetMatchingGlobalQuotaNamespacesByName(
	ctx context.Context,
	c client.Client,
	quota *capsulev1beta2.GlobalResourceQuota,
) (nsNames []string, err error) {
	namespaces, err := GetMatchingGlobalQuotaNamespaces(ctx, c, quota)
	if err != nil {
		return
	}

	nsNames = make([]string, 0, len(namespaces))
	for _, ns := range namespaces {
		nsNames = append(nsNames, ns.Name)
	}

	return
}

// Get all matching namespaces
func GetMatchingGlobalQuotaNamespaces(
	ctx context.Context,
	c client.Client,
	quota *capsulev1beta2.GlobalResourceQuota,
) (namespaces []corev1.Namespace, err error) {
	// Collect Namespaces (Matching)
	namespaces = make([]corev1.Namespace, 0)
	seenNamespaces := make(map[string]struct{})

	// Get Item within Resource Quota
	objectLabel, err := capsuleutils.GetTypeLabel(&capsulev1beta2.Tenant{})
	if err != nil {
		return
	}

	for _, selector := range quota.Spec.Selectors {
		selected, err := selector.GetMatchingNamespaces(ctx, c)
		if err != nil {
			continue
		}

		for _, ns := range selected {
			// Skip if namespace is being deleted
			if !ns.ObjectMeta.DeletionTimestamp.IsZero() {
				continue
			}

			if _, exists := seenNamespaces[ns.Name]; exists {
				continue // Skip duplicates
			}

			if selector.MustTenantNamespace {
				if _, ok := ns.Labels[objectLabel]; !ok {
					continue
				}
			}

			seenNamespaces[ns.Name] = struct{}{}
			namespaces = append(namespaces, ns)
		}
	}

	return
}

// Returns for an item it's name as Kubernetes object
func ItemObjectName(itemName api.Name, quota *capsulev1beta2.GlobalResourceQuota) string {
	// Generate a name using the tenant name and item name
	return fmt.Sprintf("capsule-%s-%s", quota.Name, itemName)
}

func (r *Manager) emitEvent(object runtime.Object, namespace string, res controllerutil.OperationResult, msg string, err error) {
	eventType := corev1.EventTypeNormal

	if err != nil {
		eventType = corev1.EventTypeWarning
		res = "Error"
	}

	r.Recorder.AnnotatedEventf(object, map[string]string{"OperationResult": string(res)}, eventType, namespace, msg)
}
