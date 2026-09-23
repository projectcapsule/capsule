// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

func TestProtectionAllowsNamespaceCleanup(t *testing.T) {
	for name, handler := range map[string]handlers.Handler{"replication": ReplicaHandler(), "permit": ResourcePermitResourceHandler()} {
		for _, state := range []string{"active", "deleting", "terminating", "missing", "read error", "other namespace", "recreated", "cluster scoped"} {
			for _, deleting := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/delete=%t", name, state, deleting), func(t *testing.T) {
					c, req := replicationAdmissionFixture(t, false, false, 1)
					require.NoError(t, corev1.AddToScheme(c.Scheme()))
					ns := &corev1.Namespace{Name: req.Namespace}
					if state == "recreated" {
						ns.Status.Phase = corev1.NamespaceTerminating
					}
					if state != "missing" {
						require.NoError(t, c.Create(t.Context(), ns))
					}
					reads := 0
					reader := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						reads++
						require.Equal(t, client.ObjectKey{Name: req.Namespace}, key)
						if state == "read error" {
							return errors.New("namespace unavailable")
						}
						if err := c.Get(ctx, key, obj, opts...); err != nil {
							return err
						}
						ns := obj.(*corev1.Namespace)
						if state == "deleting" {
							ns.DeletionTimestamp = new(metav1.Now())
						}
						if state == "terminating" {
							ns.Status.Phase = corev1.NamespaceTerminating
						}
						if state == "recreated" {
							ns.Status.Phase = corev1.NamespaceActive
						}
						return nil
					}})
					// A terminating namespace in tenant B must not exempt tenant A.
					if state == "other namespace" {
						require.NoError(t, c.Create(t.Context(), &corev1.Namespace{Name: "tenant-b", Status: corev1.NamespaceStatus{Phase: corev1.NamespaceTerminating}}))
					}
					if state == "cluster scoped" {
						req.Namespace = ""
					}
					obj := protectedResourcePermitResource("system:serviceaccount:tenant-a:runner")
					obj.SetAPIVersion("v1")
					obj.SetKind("ConfigMap")
					obj.SetName(req.Name)
					obj.SetNamespace(req.Namespace)
					obj.SetLabels(map[string]string{meta.ReplicationProtectionLabel: meta.ValueTrue, meta.ResourcePermitProtectionLabel: meta.ValueTrue})
					raw, err := json.Marshal(obj)
					require.NoError(t, err)
					req.Object = runtime.RawExtension{Raw: raw}
					req.OldObject = req.Object
					decoder := admission.NewDecoder(c.Scheme())
					call := handler.OnUpdate(c, reader, decoder, nil)
					if deleting {
						call = handler.OnDelete(c, reader, decoder, nil)
					}
					response := call(t.Context(), req)
					if deleting && (state == "deleting" || state == "terminating") {
						require.Nil(t, response, "namespace cleanup must continue through the admission chain")
					} else {
						require.NotNil(t, response)
						require.False(t, response.Allowed)
						if deleting && state == "read error" {
							require.EqualValues(t, 500, response.Result.Code)
							require.Contains(t, response.Result.Message, "namespace unavailable")
						} else {
							require.EqualValues(t, 403, response.Result.Code)
						}
					}
					if !deleting || state == "cluster scoped" {
						require.Zero(t, reads, "updates and cluster resources must not read namespace termination")
					} else {
						require.Equal(t, 1, reads)
					}
				})
			}
		}
	}
}

func TestProtectionDeletionPreservesAdmissionErrors(t *testing.T) {
	for name, handler := range map[string]handlers.Handler{"replication": ReplicaHandler(), "permit": ResourcePermitResourceHandler()} {
		t.Run(name, func(t *testing.T) {
			c, req := replicationAdmissionFixture(t, false, false, 1)
			req.OldObject.Raw = []byte("{")
			// No namespace read or allow decision may mask a decoding failure.
			response := handler.OnDelete(c, nil, admission.NewDecoder(c.Scheme()), nil)(t.Context(), req)
			require.NotNil(t, response)
			require.False(t, response.Allowed)
			require.EqualValues(t, 500, response.Result.Code)
		})
	}
}

func BenchmarkProtectionDeletion(b *testing.B) {
	b.Setenv(configuration.EnvironmentServiceaccountName, "capsule")
	b.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")
	for name, handler := range map[string]handlers.Handler{"replication": ReplicaHandler(), "permit": ResourcePermitResourceHandler()} {
		for _, state := range []string{"active", "terminating", "authorized", "controller"} {
			for _, namespaces := range []int{1, 1000} {
				b.Run(fmt.Sprintf("%s/%s/namespaces=%d", name, state, namespaces), func(b *testing.B) {
					c, req := replicationAdmissionFixture(b, false, true, 1)
					ns := &corev1.Namespace{Name: req.Namespace}
					if state == "terminating" {
						ns.Status.Phase = corev1.NamespaceTerminating
					}
					require.NoError(b, c.Create(b.Context(), ns))
					for i := 1; i < namespaces; i++ {
						name := fmt.Sprintf("unrelated-tenant-%d", i)
						require.NoError(b, c.Create(b.Context(), &corev1.Namespace{Name: name, Labels: map[string]string{meta.TenantLabel: name}}))
					}
					obj := protectedResourcePermitResource("system:serviceaccount:tenant-a:runner")
					obj.SetAPIVersion("v1")
					obj.SetKind("ConfigMap")
					obj.SetName(req.Name)
					obj.SetNamespace(req.Namespace)
					obj.SetLabels(map[string]string{meta.ReplicationProtectionLabel: meta.ValueTrue, meta.ResourcePermitProtectionLabel: meta.ValueTrue})
					raw, err := json.Marshal(obj)
					require.NoError(b, err)
					req.OldObject = runtime.RawExtension{Raw: raw}
					if state == "authorized" {
						req.UserInfo.Username = "system:serviceaccount:tenant-a:runner"
					}
					if state == "controller" {
						req.UserInfo.Username = "system:serviceaccount:capsule-system:capsule"
					}
					reads := 0
					reader := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if _, ok := obj.(*corev1.Namespace); ok {
							reads++
						}
						return c.Get(ctx, key, obj, opts...)
					}})
					call := handler.OnDelete(c, reader, admission.NewDecoder(c.Scheme()), nil)
					b.ReportAllocs()
					for b.Loop() {
						response := call(b.Context(), req)
						if (response != nil) != (state == "active") || (response != nil && response.Result.Code != 403) {
							b.Fatalf("unexpected admission decision: %#v", response)
						}
					}
					b.ReportMetric(float64(reads)/float64(b.N), "namespace-reads/op")
				})
			}
		}
	}
}
