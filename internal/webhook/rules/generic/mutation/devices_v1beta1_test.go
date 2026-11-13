// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"encoding/json"
	"fmt"
	"testing"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func TestDeviceClassDefaultsBeta1(t *testing.T) {
	for _, kind := range []string{"ResourceClaim", "ResourceClaimTemplate"} {
		for _, tc := range []struct {
			name, input, want string
			changed           bool
		}{
			{"omitted", `{"name":"gpu"}`, `{"name":"gpu","deviceClassName":"gpu"}`, true},
			{"null", `{"name":"gpu","deviceClassName":null}`, `{"name":"gpu","deviceClassName":"gpu"}`, true},
			{"empty", `{"name":"gpu","deviceClassName":""}`, `{"name":"gpu","deviceClassName":"gpu"}`, true},
			{"explicit", `{"name":"gpu","deviceClassName":"explicit"}`, `{"name":"gpu","deviceClassName":"explicit"}`, false},
			{"settings", `{"name":"gpu","count":2,"adminAccess":true,"selectors":[{"cel":{"expression":"true"}}]}`, `{"name":"gpu","deviceClassName":"gpu","count":2,"adminAccess":true,"selectors":[{"cel":{"expression":"true"}}]}`, true},
			{"empty alternatives", `{"name":"gpu","firstAvailable":[]}`, `{"name":"gpu","deviceClassName":"gpu","firstAvailable":[]}`, true},
			{"null alternatives", `{"name":"gpu","firstAvailable":null}`, `{"name":"gpu","deviceClassName":"gpu","firstAvailable":null}`, true},
			{"alternatives", `{"name":"gpu","firstAvailable":[{"name":"one"},{"name":"two","deviceClassName":"explicit"}]}`, `{"name":"gpu","firstAvailable":[{"name":"one","deviceClassName":"gpu"},{"name":"two","deviceClassName":"explicit"}]}`, true},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				var input, want map[string]any
				require.NoError(t, json.Unmarshal([]byte(tc.input), &input))
				require.NoError(t, json.Unmarshal([]byte(tc.want), &want))
				obj := deviceDefaultObject("v1beta1", kind, []any{input})
				before := obj.DeepCopy()
				bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{deviceDefault("gpu"), deviceDefault("later")}}}
				changed, err := mutateDeviceClassDefaults(t.Context(), obj, bodies, nil)
				require.NoError(t, err)
				require.Equal(t, tc.changed, changed)
				require.Equal(t, want, deviceDefaultRequests(obj)[0])
				require.Equal(t, deviceDefaultRequests(before)[0], input, "borrowed request maps must remain immutable")
				require.Equal(t, before.Object["metadata"], obj.Object["metadata"])
				require.Equal(t, before.Object["status"], obj.Object["status"])
				changed, err = mutateDeviceClassDefaults(t.Context(), obj, bodies, nil)
				require.NoError(t, err)
				require.False(t, changed, "reinvocation must preserve the chosen class")
			})
		}
		for _, bad := range []any{nil, int64(1), map[string]any{"deviceClassName": true}, map[string]any{"firstAvailable": "invalid"}, map[string]any{"firstAvailable": []any{nil}}, map[string]any{"firstAvailable": []any{map[string]any{"deviceClassName": true}}}} {
			t.Run(kind+"/malformed/"+fmt.Sprint(bad), func(t *testing.T) {
				obj := deviceDefaultObject("v1beta1", kind, []any{map[string]any{"name": "first"}, bad})
				before := obj.DeepCopy()
				changed, err := mutateDeviceClassDefaults(t.Context(), obj, []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{deviceDefault("gpu")}}}, nil)
				require.ErrorContains(t, err, "request[1]")
				require.False(t, changed)
				require.Equal(t, before, obj, "malformed requests must not publish partial defaults")
			})
		}
	}
}

func TestDeviceDefaultBeta1Registration(t *testing.T) {
	for _, kind := range []string{"ResourceClaim", "ResourceClaimTemplate"} {
		for _, fallback := range []bool{false, true} {
			for _, explicit := range []string{"", "explicit"} {
				t.Run(fmt.Sprintf("%s/fallback=%t/class=%s", kind, fallback, explicit), func(t *testing.T) {
					c, reads, decoder := mutationRegistrationFixture(t, 100)
					body := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{deviceDefault("gpu")}}
					status := &capsulev1beta2.RuleStatus{}
					require.NoError(t, c.Get(t.Context(), client.ObjectKey{Namespace: "selected", Name: meta.NameForManagedRuleStatus()}, status))
					if fallback {
						require.NoError(t, c.Delete(t.Context(), status))
						tnt := &capsulev1beta2.Tenant{}
						require.NoError(t, c.Get(t.Context(), client.ObjectKey{Name: "tenant"}, tnt))
						tnt.Spec.Rules = []*rules.NamespaceRuleBodyTenant{{NamespaceRuleBodyNamespace: body}}
						require.NoError(t, c.Update(t.Context(), tnt))
					} else {
						status.Spec, status.Status.Rules = []*rules.NamespaceRuleBodyNamespace{body}, []*rules.NamespaceRuleBodyNamespace{body}
						require.NoError(t, c.Update(t.Context(), status))
					}
					obj := deviceDefaultObject("v1beta1", kind, []any{map[string]any{"name": "gpu", "deviceClassName": explicit}})
					req := listenerSetRequest(t, obj, admissionv1.Create, "")
					req.DryRun = new(true)
					require.Equal(t, "v1beta1", req.Kind.Version, "wildcard mutation receives the original version")
					reads.Store(0)
					response := Register(nil, nil).GetHandlers()[0].OnCreate(c, c, decoder, events.NewDiscardRecorder())(t.Context(), req)
					require.EqualValues(t, profileReads(fallback), reads.Load(), "defaulting must not read DeviceClasses or unrelated tenants")
					if explicit != "" {
						require.Nil(t, response, "an explicit class must continue the chain unchanged")
						return
					}
					require.NotNil(t, response)
					require.True(t, response.Allowed)
					encoded, err := json.Marshal(response.Patches)
					require.NoError(t, err)
					patch, err := jsonpatch.DecodePatch(encoded)
					require.NoError(t, err)
					raw, err := patch.Apply(req.Object.Raw)
					require.NoError(t, err)
					require.NoError(t, json.Unmarshal(raw, &obj.Object))
					require.Equal(t, "gpu", deviceDefaultRequests(obj)[0].(map[string]any)["deviceClassName"])
					require.NotContains(t, deviceDefaultRequests(obj)[0], "exactly")
				})
			}
		}
	}
}
