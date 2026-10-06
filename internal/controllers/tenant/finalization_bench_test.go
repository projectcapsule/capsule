// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

// Measure the deletion reconciliation decision, including namespace cleanup and
// finalizer eligibility, without deleting the fixture Tenant between iterations.
// Fake-client selection is not an API-server or informer-index latency estimate.
func BenchmarkTenantFinalization(b *testing.B) {
	for _, unrelated := range []int{0, 100, 1000} {
		for _, mode := range []string{"waiting", "release"} {
			b.Run(fmt.Sprintf("unrelated=%d/%s", unrelated, mode), func(b *testing.B) {
				tnt := deletingLifecycleTenant()
				objects := []client.Object{tnt}
				for i := range unrelated {
					other := &capsulev1beta2.Tenant{Name: fmt.Sprintf("other-%d", i), UID: "other-uid"}
					objects = append(objects, other, cleanupOwnedNamespace(other, other.Name+"-ns", false))
				}
				if mode == "waiting" {
					ns := cleanupOwnedNamespace(tnt, "held", true)
					objects = append(objects, ns)
					tnt.Status.Spaces = []*capsulev1beta2.TenantStatusNamespaceItem{{Name: ns.Name, UID: ns.UID, Conditions: meta.ConditionList{}}}
				}
				manager, _ := namespaceCleanupFixture(b, objects...)
				calls := &mockclient.CallCounter{}
				counted := interceptor.NewClient(manager.Client.(client.WithWatch), calls.Interceptors())
				manager.Client = counted
				manager.reader = counted
				b.ReportAllocs()
				for b.Loop() {
					candidate := tnt.DeepCopy()
					if err := manager.reconcile(b.Context(), logr.Discard(), candidate); err != nil {
						b.Fatal(err)
					}
					if kept := controllerutil.ContainsFinalizer(candidate, meta.ControllerFinalizer); kept != (mode == "waiting") {
						b.Fatal("incorrect finalizer decision")
					}
				}
				calls.Report(b)
			})
		}
	}
}
