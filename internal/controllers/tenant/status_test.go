// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capmeta "github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

func TestUpdateReconcilingStatusSynchronizesCurrentStatus(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add Capsule scheme: %v", err)
	}

	latest := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "tenant", Generation: 2},
		Status: capsulev1beta2.TenantStatus{
			ObservedGeneration: 2,
			State:              capsulev1beta2.TenantStateActive,
			Conditions: capmeta.ConditionList{
				capmeta.NewReadyCondition(&capsulev1beta2.Tenant{}),
				capmeta.NewCordonedCondition(&capsulev1beta2.Tenant{}),
			},
		},
	}
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(latest).Build()
	manager := &Manager{Client: reader, reader: reader}

	stale := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: latest.Name, Generation: latest.Generation},
	}
	if err := manager.updateReconcilingStatus(context.Background(), stale); err != nil {
		t.Fatalf("update reconciling status: %v", err)
	}

	if len(stale.Status.Conditions) == 0 {
		t.Fatal("current API status was not copied to stale reconcile instance")
	}
	if stale.Status.ObservedGeneration != latest.Status.ObservedGeneration {
		t.Fatalf("observed generation = %d, want %d", stale.Status.ObservedGeneration, latest.Status.ObservedGeneration)
	}
}

func TestEnsureTenantStatusInitializedTracksCordoning(t *testing.T) {
	t.Parallel()

	tenant := &capsulev1beta2.Tenant{}
	ensureTenantStatusInitialized(tenant)
	condition := tenant.Status.Conditions.GetConditionByType(capmeta.CordonedCondition)
	if condition == nil || condition.Status != metav1.ConditionFalse || condition.Reason != capmeta.ActiveReason {
		t.Fatalf("initial Cordoned condition = %#v, want False/%s", condition, capmeta.ActiveReason)
	}

	tenant.Spec.Cordoned = true
	ensureTenantStatusInitialized(tenant)
	condition = tenant.Status.Conditions.GetConditionByType(capmeta.CordonedCondition)
	if condition == nil || condition.Status != metav1.ConditionTrue || condition.Reason != capmeta.CordonedReason {
		t.Fatalf("cordoned condition = %#v, want True/%s", condition, capmeta.CordonedReason)
	}
	if tenant.Status.State != capsulev1beta2.TenantStateCordoned {
		t.Fatalf("state = %q, want Cordoned", tenant.Status.State)
	}

	tenant.Spec.Cordoned = false
	ensureTenantStatusInitialized(tenant)
	condition = tenant.Status.Conditions.GetConditionByType(capmeta.CordonedCondition)
	if condition == nil || condition.Status != metav1.ConditionFalse || condition.Reason != capmeta.ActiveReason {
		t.Fatalf("uncordoned condition = %#v, want False/%s", condition, capmeta.ActiveReason)
	}
}

func TestUpdateReconcilingStatusRepairsSpecOwnersAtCurrentGeneration(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add Capsule scheme: %v", err)
	}

	promoted := rbac.CoreOwnerSpec{
		UserSpec: rbac.UserSpec{Kind: rbac.ServiceAccountOwner, Name: "system:serviceaccount:tenant:promoted"},
	}
	specOwner := rbac.CoreOwnerSpec{
		UserSpec:     rbac.UserSpec{Kind: rbac.UserOwner, Name: "alice"},
		ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
	}
	latest := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "tenant", Generation: 2},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{{CoreOwnerSpec: specOwner}},
		},
		Status: capsulev1beta2.TenantStatus{
			ObservedGeneration: 2,
			Owners:             rbac.OwnerStatusListSpec{promoted},
		},
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&capsulev1beta2.Tenant{}).
		WithObjects(latest).
		Build()
	manager := &Manager{Client: cl, reader: cl}
	instance := latest.DeepCopy()

	if err := manager.updateReconcilingStatus(context.Background(), instance); err != nil {
		t.Fatalf("update reconciling status: %v", err)
	}

	updated := &capsulev1beta2.Tenant{}
	if err := cl.Get(context.Background(), client.ObjectKey{Name: latest.Name}, updated); err != nil {
		t.Fatalf("get updated tenant: %v", err)
	}
	if _, found := updated.Status.Owners.FindOwner(specOwner.Name, specOwner.Kind); !found {
		t.Fatalf("spec owner was not written: %#v", updated.Status.Owners)
	}
	if _, found := updated.Status.Owners.FindOwner(promoted.Name, promoted.Kind); !found {
		t.Fatalf("existing promoted owner was not preserved: %#v", updated.Status.Owners)
	}
	if updated.Status.State != capsulev1beta2.TenantStateActive {
		t.Fatalf("status state = %q, want Active", updated.Status.State)
	}
	if updated.Status.Conditions.GetConditionByType(capmeta.ReadyCondition) == nil {
		t.Fatal("Ready condition was not initialized")
	}
}

func TestUpdateTenantOwnersStatusPersistsEvaluatedOwners(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add Capsule scheme: %v", err)
	}

	stored := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "tenant"},
		Status: capsulev1beta2.TenantStatus{
			State: capsulev1beta2.TenantStateActive,
		},
	}
	owners := rbac.OwnerStatusListSpec{{
		UserSpec: rbac.UserSpec{Kind: rbac.UserOwner, Name: "alice"},
		ClusterRoles: []string{
			"admin",
			"capsule-namespace-deleter",
		},
	}}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&capsulev1beta2.Tenant{}).
		WithObjects(stored).
		Build()
	manager := &Manager{Client: cl, reader: cl}
	instance := stored.DeepCopy()
	instance.Status.Owners = owners

	if err := manager.updateTenantOwnersStatus(context.Background(), instance); err != nil {
		t.Fatalf("update tenant owner status: %v", err)
	}

	updated := &capsulev1beta2.Tenant{}
	if err := cl.Get(context.Background(), client.ObjectKey{Name: stored.Name}, updated); err != nil {
		t.Fatalf("get updated tenant: %v", err)
	}
	if !reflect.DeepEqual(updated.Status.Owners, owners) {
		t.Fatalf("status owners = %#v, want %#v", updated.Status.Owners, owners)
	}
	if updated.Status.State != stored.Status.State {
		t.Fatalf("status state = %q, want %q", updated.Status.State, stored.Status.State)
	}
}

func TestUpdateTenantOwnersStatusInitializesRequiredStatus(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add Capsule scheme: %v", err)
	}

	stored := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "tenant"}}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&capsulev1beta2.Tenant{}).
		WithObjects(stored).
		Build()
	manager := &Manager{Client: cl, reader: cl}
	instance := stored.DeepCopy()
	instance.Status.Owners = rbac.OwnerStatusListSpec{{
		UserSpec: rbac.UserSpec{Kind: rbac.UserOwner, Name: "alice"},
	}}

	if err := manager.updateTenantOwnersStatus(context.Background(), instance); err != nil {
		t.Fatalf("update tenant owner status: %v", err)
	}

	updated := &capsulev1beta2.Tenant{}
	if err := cl.Get(context.Background(), client.ObjectKey{Name: stored.Name}, updated); err != nil {
		t.Fatalf("get updated tenant: %v", err)
	}
	if updated.Status.State != capsulev1beta2.TenantStateActive {
		t.Fatalf("status state = %q, want Active", updated.Status.State)
	}
	if updated.Status.Conditions.GetConditionByType(capmeta.ReadyCondition) == nil {
		t.Fatal("Ready condition was not initialized")
	}
}

func TestUpdateTenantClassStatusPreservesOwnersAndConditions(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add Capsule scheme: %v", err)
	}

	owner := rbac.CoreOwnerSpec{UserSpec: rbac.UserSpec{Kind: rbac.UserOwner, Name: "alice"}}
	ready := capmeta.NewReadyCondition(&capsulev1beta2.Tenant{})
	cordoned := capmeta.NewCordonedCondition(&capsulev1beta2.Tenant{})
	stored := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "tenant"},
		Status: capsulev1beta2.TenantStatus{
			Owners:     rbac.OwnerStatusListSpec{owner},
			State:      capsulev1beta2.TenantStateActive,
			Conditions: capmeta.ConditionList{ready, cordoned},
		},
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&capsulev1beta2.Tenant{}).
		WithObjects(stored).
		Build()
	manager := &Manager{Client: cl, reader: cl}
	persistedBefore := &capsulev1beta2.Tenant{}
	if err := cl.Get(context.Background(), client.ObjectKey{Name: stored.Name}, persistedBefore); err != nil {
		t.Fatalf("get tenant before class update: %v", err)
	}
	if err := manager.updateTenantClassStatus(
		context.Background(),
		stored.Name,
		func(_ context.Context, tenant *capsulev1beta2.Tenant) error {
			tenant.Status.Classes.StorageClasses = []string{"fast"}

			return nil
		},
	); err != nil {
		t.Fatalf("update tenant class status: %v", err)
	}

	updated := &capsulev1beta2.Tenant{}
	if err := cl.Get(context.Background(), client.ObjectKey{Name: stored.Name}, updated); err != nil {
		t.Fatalf("get updated tenant: %v", err)
	}
	if !reflect.DeepEqual(updated.Status.Owners, persistedBefore.Status.Owners) {
		t.Fatalf("owners = %#v, want preserved %#v", updated.Status.Owners, persistedBefore.Status.Owners)
	}
	if !reflect.DeepEqual(updated.Status.Conditions, persistedBefore.Status.Conditions) {
		t.Fatalf("conditions = %#v, want preserved %#v", updated.Status.Conditions, persistedBefore.Status.Conditions)
	}
	if !reflect.DeepEqual(updated.Status.Classes.StorageClasses, []string{"fast"}) {
		t.Fatalf("storage classes = %#v, want [fast]", updated.Status.Classes.StorageClasses)
	}
}
