// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package customquota_test

import (
	"reflect"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/customquota"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestCustomQuotaIndexers(t *testing.T) {
	t.Parallel()

	target := capsulev1beta2.CustomQuotaStatusTarget{
		GroupVersionKind: metav1.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
	}
	claim := capsulev1beta2.CustomQuotaClaimItem{
		NamespacedObjectWithUIDReference: meta.NamespacedObjectWithUIDReference{UID: types.UID("claim-uid")},
	}

	tests := []struct {
		name  string
		field string
		got   []string
		want  []string
	}{
		{
			name:  "namespaced target",
			field: customquota.NamespacedTargetReference{}.Field(),
			got: customquota.NamespacedTargetReference{}.Func()(&capsulev1beta2.CustomQuota{
				Status: capsulev1beta2.CustomQuotaStatus{Targets: []capsulev1beta2.CustomQuotaStatusTarget{target}},
			}),
			want: []string{target.String()},
		},
		{
			name:  "namespaced uid",
			field: customquota.NamespacedObjectUIDReference{}.Field(),
			got: customquota.NamespacedObjectUIDReference{}.Func()(&capsulev1beta2.CustomQuota{
				Status: capsulev1beta2.CustomQuotaStatus{Claims: []capsulev1beta2.CustomQuotaClaimItem{claim}},
			}),
			want: []string{"claim-uid"},
		},
		{
			name:  "global target",
			field: customquota.GlobalTargetReference{}.Field(),
			got: customquota.GlobalTargetReference{}.Func()(&capsulev1beta2.GlobalCustomQuota{
				Status: capsulev1beta2.GlobalCustomQuotaStatus{
					CustomQuotaStatus: capsulev1beta2.CustomQuotaStatus{Targets: []capsulev1beta2.CustomQuotaStatusTarget{target}},
				},
			}),
			want: []string{target.String()},
		},
		{
			name:  "global uid",
			field: customquota.GlobalObjectUIDReference{}.Field(),
			got: customquota.GlobalObjectUIDReference{}.Func()(&capsulev1beta2.GlobalCustomQuota{
				Status: capsulev1beta2.GlobalCustomQuotaStatus{
					CustomQuotaStatus: capsulev1beta2.CustomQuotaStatus{Claims: []capsulev1beta2.CustomQuotaClaimItem{claim}},
				},
			}),
			want: []string{"claim-uid"},
		},
	}

	for _, tt := range tests {
		if tt.field == "" {
			t.Fatalf("%s field is empty", tt.name)
		}
		if !reflect.DeepEqual(tt.got, tt.want) {
			t.Fatalf("%s got %#v, want %#v", tt.name, tt.got, tt.want)
		}
	}
}
