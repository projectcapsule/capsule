// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

// The kinds whose claim templates Kubernetes controllers create claims from.
var templateKinds = []string{"Pod", "ReplicationController", "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job", "CronJob"}

func ephemeralClaim(name string, class *string) corev1.Volume {
	return corev1.Volume{Name: name, Ephemeral: &corev1.EphemeralVolumeSource{
		VolumeClaimTemplate: &corev1.PersistentVolumeClaimTemplate{Spec: newClaim("", class).Spec},
	}}
}

func emptyDir(name string) corev1.Volume {
	return corev1.Volume{Name: name, EmptyDir: &corev1.EmptyDirVolumeSource{}}
}

func claimTemplate(name string, class *string) corev1.PersistentVolumeClaim {
	return corev1.PersistentVolumeClaim{Name: name, Spec: newClaim("", class).Spec}
}

// workload returns an object of kind whose Pod spec or Pod template has volumes.
func workload(kind, namespace string, volumes ...corev1.Volume) client.Object {
	template := corev1.PodTemplateSpec{
		Labels: map[string]string{"app": "db"},
		Spec:   corev1.PodSpec{Containers: []corev1.Container{{Name: "db", Image: "registry.example.com/db:v1"}}, Volumes: volumes},
	}
	selector := &metav1.LabelSelector{MatchLabels: template.Labels}
	meta := metav1.ObjectMeta{Name: "db", Namespace: namespace, UID: "db"}

	switch kind {
	case "Pod":
		return &corev1.Pod{APIVersion: "v1", Kind: kind, ObjectMeta: meta, Spec: template.Spec}
	case "ReplicationController":
		return &corev1.ReplicationController{APIVersion: "v1", Kind: kind, ObjectMeta: meta,
			Spec: corev1.ReplicationControllerSpec{Selector: template.Labels, Template: &template}}
	case "Deployment":
		return &appsv1.Deployment{APIVersion: "apps/v1", Kind: kind, ObjectMeta: meta,
			Spec: appsv1.DeploymentSpec{Selector: selector, Template: template}}
	case "StatefulSet":
		return &appsv1.StatefulSet{APIVersion: "apps/v1", Kind: kind, ObjectMeta: meta,
			Spec: appsv1.StatefulSetSpec{Selector: selector, Template: template}}
	case "DaemonSet":
		return &appsv1.DaemonSet{APIVersion: "apps/v1", Kind: kind, ObjectMeta: meta,
			Spec: appsv1.DaemonSetSpec{Selector: selector, Template: template}}
	case "ReplicaSet":
		return &appsv1.ReplicaSet{APIVersion: "apps/v1", Kind: kind, ObjectMeta: meta,
			Spec: appsv1.ReplicaSetSpec{Selector: selector, Template: template}}
	case "Job":
		return &batchv1.Job{APIVersion: "batch/v1", Kind: kind, ObjectMeta: meta,
			Spec: batchv1.JobSpec{Template: template}}
	case "CronJob":
		return &batchv1.CronJob{APIVersion: "batch/v1", Kind: kind, ObjectMeta: meta,
			Spec: batchv1.CronJobSpec{Schedule: "@daily", JobTemplate: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Template: template}}}}
	}

	panic("unsupported kind " + kind)
}

func statefulSet(namespace string, claims []corev1.PersistentVolumeClaim, volumes ...corev1.Volume) *appsv1.StatefulSet {
	obj, _ := workload("StatefulSet", namespace, volumes...).(*appsv1.StatefulSet)
	obj.Spec.VolumeClaimTemplates = claims

	return obj
}

// volumesPath is where the Pod spec of kind lists its volumes.
func volumesPath(kind string) string {
	switch kind {
	case "Pod":
		return "spec.volumes"
	case "CronJob":
		return "spec.jobTemplate.spec.template.spec.volumes"
	}

	return "spec.template.spec.volumes"
}

func ephemeralClassPath(kind string, index int, volume string) string {
	return fmt.Sprintf("%s[%d](%s).ephemeral.volumeClaimTemplate.spec.volumeAttributesClassName", volumesPath(kind), index, volume)
}

// objectRequest builds the admission request for obj, as an update of old when
// it is set, made by alice.
func objectRequest(tb testing.TB, obj, old client.Object) admission.Request {
	tb.Helper()

	gvk := obj.GetObjectKind().GroupVersionKind()
	raw, err := json.Marshal(obj)
	require.NoError(tb, err)

	req := admission.Request{
		Kind:      metav1.GroupVersionKind{Group: gvk.Group, Version: gvk.Version, Kind: gvk.Kind},
		Namespace: obj.GetNamespace(), Name: obj.GetName(), Operation: admissionv1.Create,
		Object: runtime.RawExtension{Raw: raw}, UserInfo: authenticationv1.UserInfo{Username: "alice"}}

	if old != nil {
		oldRaw, err := json.Marshal(old)
		require.NoError(tb, err)

		req.Operation, req.OldObject = admissionv1.Update, runtime.RawExtension{Raw: oldRaw}
	}

	return req
}

func notAllowedAt(class, path string) string {
	return fmt.Sprintf("volume attributes class %q at %s is not allowed by namespace rule", class, path)
}

func TestVolumeAttributesClassClaimTemplates(t *testing.T) {
	for _, kind := range templateKinds {
		for _, tc := range []struct {
			name       string
			volumes    []corev1.Volume
			denied     string
			classReads int64
			skipped    bool
		}{
			{name: "allowed classes", volumes: []corev1.Volume{ephemeralClaim("scratch", new("silver")), emptyDir("tmp"), ephemeralClaim("cache", new("gold"))}, classReads: 1},
			{
				name: "a forbidden class", volumes: []corev1.Volume{ephemeralClaim("scratch", new("silver")), emptyDir("tmp"), ephemeralClaim("cache", new("bronze"))}, classReads: 1,
				denied: notAllowedAt("bronze", ephemeralClassPath(kind, 2, "cache")),
			},
			{name: "a class two templates name is read once", volumes: []corev1.Volume{ephemeralClaim("scratch", new("gold")), ephemeralClaim("cache", new("gold"))}, classReads: 1},
			{
				// The first value a rule blocks decides; the later ones aren't read.
				name: "the first forbidden class decides", volumes: []corev1.Volume{ephemeralClaim("scratch", new("ghost")), ephemeralClaim("cache", new("bronze"))}, classReads: 1,
				denied: notAllowedAt("ghost", ephemeralClassPath(kind, 0, "scratch")),
			},
			{name: "templates without a class are not checked", volumes: []corev1.Volume{ephemeralClaim("scratch", nil), ephemeralClaim("cache", new(""))}, skipped: true},
			{name: "other volumes are not checked", volumes: []corev1.Volume{emptyDir("tmp")}, skipped: true},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				cl, decoder := singleClassProfile(t, allowClasses())
				recorder := newRecorder()

				response := admitClaim(t.Context(), cl, decoder, recorder, objectRequest(t, workload(kind, "tenant-a", tc.volumes...), nil))
				if tc.denied == "" {
					require.Nil(t, response, "successful checks must continue the admission chain")
					require.Empty(t, recorder.emitted)
				} else {
					requireDenied(t, response, tc.denied)
					require.Equal(t, []string{events.ReasonForbiddenVolumeAttributesClass}, recorder.reasons())

					regarding, ok := recorder.emitted[0].Regarding().(client.Object)
					require.True(t, ok)
					require.Equal(t, kind, regarding.GetObjectKind().GroupVersionKind().Kind, "the event belongs to the object with the template")
					require.Equal(t, "db", regarding.GetName())
				}

				require.Equal(t, tc.classReads, cl.classGets.Load())

				if tc.skipped {
					require.Zero(t, cl.gets.Load(), "a request without a class must not read the tenant, rules or class")
				}
			})
		}
	}
}

func TestVolumeAttributesClassStatefulSetClaimTemplates(t *testing.T) {
	for _, tc := range []struct {
		name    string
		obj     *appsv1.StatefulSet
		denied  string
		skipped bool
	}{
		{name: "allowed claim templates", obj: statefulSet("tenant-a", []corev1.PersistentVolumeClaim{claimTemplate("data", new("silver")), claimTemplate("logs", new("gold"))})},
		{
			name:   "a forbidden claim template",
			obj:    statefulSet("tenant-a", []corev1.PersistentVolumeClaim{claimTemplate("data", new("silver")), claimTemplate("logs", new("bronze"))}),
			denied: notAllowedAt("bronze", "spec.volumeClaimTemplates[1](logs).spec.volumeAttributesClassName"),
		},
		{
			name:   "a forbidden ephemeral volume next to allowed claim templates",
			obj:    statefulSet("tenant-a", []corev1.PersistentVolumeClaim{claimTemplate("data", new("silver"))}, ephemeralClaim("scratch", new("bronze"))),
			denied: notAllowedAt("bronze", "spec.template.spec.volumes[0](scratch).ephemeral.volumeClaimTemplate.spec.volumeAttributesClassName"),
		},
		{name: "claim templates without a class are not checked", obj: statefulSet("tenant-a", []corev1.PersistentVolumeClaim{claimTemplate("data", nil), claimTemplate("logs", new(""))}), skipped: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cl, decoder := singleClassProfile(t, allowClasses())

			response := admitClaim(t.Context(), cl, decoder, newRecorder(), objectRequest(t, tc.obj, nil))
			if tc.denied == "" {
				require.Nil(t, response)
			} else {
				requireDenied(t, response, tc.denied)
			}

			if tc.skipped {
				require.Zero(t, cl.gets.Load())
			}
		})
	}
}

func TestVolumeAttributesClassTemplateUpdates(t *testing.T) {
	deployment := func(volumes ...corev1.Volume) client.Object { return workload("Deployment", "tenant-a", volumes...) }
	image := func(obj client.Object) client.Object {
		deploy, _ := obj.(*appsv1.Deployment)
		deploy.Spec.Template.Spec.Containers[0].Image = "registry.example.com/db:v2"

		return deploy
	}
	scaled := func(obj *appsv1.StatefulSet) *appsv1.StatefulSet {
		obj.Spec.Replicas = new(int32(3))

		return obj
	}
	labeled := func(obj client.Object) client.Object {
		obj.SetLabels(map[string]string{"retention": "weekly"})

		return obj
	}
	data := []corev1.PersistentVolumeClaim{claimTemplate("data", new("bronze"))}

	for _, tc := range []struct {
		name     string
		old, obj client.Object
		denied   string
		skipped  bool
	}{
		{
			name: "adding a template with a forbidden class",
			old:  deployment(ephemeralClaim("scratch", new("silver"))), obj: deployment(ephemeralClaim("scratch", new("silver")), ephemeralClaim("cache", new("bronze"))),
			denied: notAllowedAt("bronze", ephemeralClassPath("Deployment", 1, "cache")),
		},
		{
			name: "changing a template to a forbidden class",
			old:  deployment(ephemeralClaim("scratch", new("silver"))), obj: deployment(ephemeralClaim("scratch", new("bronze"))),
			denied: notAllowedAt("bronze", ephemeralClassPath("Deployment", 0, "scratch")),
		},
		{name: "changing a template to an allowed class", old: deployment(ephemeralClaim("scratch", new("bronze"))), obj: deployment(ephemeralClaim("scratch", new("silver")))},
		{name: "a rollout keeps a class the rules no longer allow", old: deployment(ephemeralClaim("scratch", new("bronze"))), obj: image(deployment(ephemeralClaim("scratch", new("bronze")))), skipped: true},
		{name: "removing a template", old: deployment(ephemeralClaim("scratch", new("bronze")), ephemeralClaim("cache", new("silver"))), obj: deployment(ephemeralClaim("cache", new("silver"))), skipped: true},
		{
			// Another template creates other claims, even with a class one already sets.
			name: "another template with a class the rules no longer allow",
			old:  deployment(ephemeralClaim("scratch", new("bronze"))), obj: deployment(ephemeralClaim("scratch", new("bronze")), ephemeralClaim("cache", new("bronze"))),
			denied: notAllowedAt("bronze", ephemeralClassPath("Deployment", 1, "cache")),
		},
		{
			name: "renaming a template with a class the rules no longer allow",
			old:  deployment(ephemeralClaim("scratch", new("bronze"))), obj: deployment(ephemeralClaim("cache", new("bronze"))),
			denied: notAllowedAt("bronze", ephemeralClassPath("Deployment", 0, "cache")),
		},
		{name: "a scale keeps the claim templates", old: statefulSet("tenant-a", data), obj: scaled(statefulSet("tenant-a", data)), skipped: true},
		{name: "a Pod update keeps the volumes", old: workload("Pod", "tenant-a", ephemeralClaim("scratch", new("bronze"))), obj: labeled(workload("Pod", "tenant-a", ephemeralClaim("scratch", new("bronze")))), skipped: true},
		{
			name: "a new class in a CronJob template",
			old:  workload("CronJob", "tenant-a", ephemeralClaim("scratch", new("silver"))), obj: workload("CronJob", "tenant-a", ephemeralClaim("scratch", new("bronze"))),
			denied: notAllowedAt("bronze", ephemeralClassPath("CronJob", 0, "scratch")),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cl, decoder := singleClassProfile(t, allowClasses())

			response := admitClaim(t.Context(), cl, decoder, newRecorder(), objectRequest(t, tc.obj, tc.old))
			if tc.denied == "" {
				require.Nil(t, response)
			} else {
				requireDenied(t, response, tc.denied)
			}

			if tc.skipped {
				require.Zero(t, cl.gets.Load(), "an update that keeps the classes must not read the tenant, rules or class")
			}
		})
	}

	t.Run("subresources are not validated", func(t *testing.T) {
		for _, subresource := range []string{"scale", "status"} {
			cl, decoder := singleClassProfile(t, allowClasses())
			req := objectRequest(t, statefulSet("tenant-a", data), statefulSet("tenant-a", nil))
			req.SubResource = subresource
			require.Nil(t, admitClaim(t.Context(), cl, decoder, newRecorder(), req))
			require.Zero(t, cl.gets.Load())
		}
	})

	t.Run("an undecodable stored object is a server error", func(t *testing.T) {
		cl, decoder := singleClassProfile(t, allowClasses())
		req := objectRequest(t, deployment(ephemeralClaim("scratch", new("bronze"))), deployment())
		req.OldObject.Raw = []byte(`{"spec":{"template":[]}}`)
		response := admitClaim(t.Context(), cl, decoder, newRecorder(), req)
		require.NotNil(t, response)
		require.False(t, response.Allowed)
	})
}

// The StatefulSet and ephemeral volume controllers create claims under their own
// identity, so a rule for an audience can only apply to the request with the
// template.
func TestVolumeAttributesClassControllerCreatedClaims(t *testing.T) {
	forAlice := denyClassNames("bronze")
	forAlice.Audience = []rules.Audience{{Kind: rules.AudienceKindUser, Name: "alice"}}

	as := func(req admission.Request, user string) admission.Request {
		req.UserInfo = authenticationv1.UserInfo{Username: user, Groups: []string{"system:serviceaccounts", "system:serviceaccounts:kube-system"}}

		return req
	}
	deniedAt := func(path string) string {
		return fmt.Sprintf("volume attributes class %q at %s is denied by namespace rule", "bronze", path)
	}
	claim := func(name string) *corev1.PersistentVolumeClaim {
		obj := newClaim("tenant-a", new("bronze"))
		obj.Name = name

		return obj
	}

	for _, tc := range []struct {
		name               string
		template           client.Object
		path               string
		controller         admission.Request
		controllerIdentity string
	}{
		{
			name:       "StatefulSet claim templates",
			template:   statefulSet("tenant-a", []corev1.PersistentVolumeClaim{claimTemplate("data", new("bronze"))}),
			path:       "spec.volumeClaimTemplates[0](data).spec.volumeAttributesClassName",
			controller: claimRequest(t, claim("data-db-0"), nil), controllerIdentity: "system:serviceaccount:kube-system:statefulset-controller",
		},
		{
			name:       "generic ephemeral volumes of a Pod",
			template:   workload("Pod", "tenant-a", ephemeralClaim("scratch", new("bronze"))),
			path:       ephemeralClassPath("Pod", 0, "scratch"),
			controller: claimRequest(t, claim("db-scratch"), nil), controllerIdentity: "system:serviceaccount:kube-system:ephemeral-volume-controller",
		},
		{
			name:       "generic ephemeral volumes of a Deployment template",
			template:   workload("Deployment", "tenant-a", ephemeralClaim("scratch", new("bronze"))),
			path:       ephemeralClassPath("Deployment", 0, "scratch"),
			controller: objectRequest(t, workload("ReplicaSet", "tenant-a", ephemeralClaim("scratch", new("bronze"))), nil), controllerIdentity: "system:serviceaccount:kube-system:deployment-controller",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cl, decoder := singleClassProfile(t, forAlice)

			requireDenied(t, admitClaim(t.Context(), cl, decoder, newRecorder(), objectRequest(t, tc.template, nil)), deniedAt(tc.path))
			require.Nil(t, admitClaim(t.Context(), cl, decoder, newRecorder(), as(tc.controller, tc.controllerIdentity)),
				"the rule for alice does not apply to the controller")
		})
	}
}

func TestVolumeAttributesClassTemplateConditions(t *testing.T) {
	// Conditions see the admitted object, which for a template is the workload.
	conditional := allowClassNames("silver")
	conditional.Enforce.Conditions = []rules.AdmissionCondition{{Name: "deployments", Expression: `request.kind.kind == "Deployment"`}}

	for _, kind := range []string{"Deployment", "StatefulSet"} {
		t.Run(kind, func(t *testing.T) {
			cl, decoder := singleClassProfile(t, conditional)

			response := admitClaim(t.Context(), cl, decoder, newRecorder(), objectRequest(t, workload(kind, "tenant-a", ephemeralClaim("scratch", new("bronze"))), nil))
			if kind == "Deployment" {
				requireDenied(t, response, notAllowedAt("bronze", ephemeralClassPath(kind, 0, "scratch")))
			} else {
				require.Nil(t, response)
			}
		})
	}
}
