package meta_test

import (
	"testing"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
)

func TestSetLooseOwnerReference(t *testing.T) {
	t.Run("nil object => no error", func(t *testing.T) {
		err := meta.SetLooseOwnerReference(nil, metav1.OwnerReference{UID: types.UID("u1")})
		if err != nil {
			t.Fatalf("expected nil err, got %v", err)
		}
	})

	t.Run("append when not present", func(t *testing.T) {
		obj := &corev1.ConfigMap{}
		obj.SetName("cm")
		obj.SetUID(types.UID("obj"))

		owner := metav1.OwnerReference{
			APIVersion: "v1",
			Kind:       "ConfigMap",
			Name:       "owner",
			UID:        types.UID("u1"),
		}

		if err := meta.SetLooseOwnerReference(obj, owner); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}

		refs := obj.GetOwnerReferences()
		if len(refs) != 1 {
			t.Fatalf("expected 1 ownerref, got %d", len(refs))
		}
		if !meta.LooseOwnerReferenceEqual(refs[0], owner) {
			t.Fatalf("ownerref mismatch: got=%v want=%v", refs[0], owner)
		}
	})

	t.Run("overwrite when same UID exists", func(t *testing.T) {
		obj := &corev1.ConfigMap{}
		obj.SetName("cm")

		orig := metav1.OwnerReference{
			APIVersion: "v1",
			Kind:       "ConfigMap",
			Name:       "old",
			UID:        types.UID("u1"),
		}
		obj.SetOwnerReferences([]metav1.OwnerReference{orig})

		repl := metav1.OwnerReference{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Name:       "new",
			UID:        types.UID("u1"),
		}

		if err := meta.SetLooseOwnerReference(obj, repl); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}

		refs := obj.GetOwnerReferences()
		if len(refs) != 1 {
			t.Fatalf("expected 1 ownerref, got %d", len(refs))
		}
		if !meta.LooseOwnerReferenceEqual(refs[0], repl) {
			t.Fatalf("expected overwritten ref %v, got %v", repl, refs[0])
		}
	})

	t.Run("multiple existing refs keep others", func(t *testing.T) {
		obj := &corev1.ConfigMap{}
		obj.SetName("cm")

		a := metav1.OwnerReference{APIVersion: "v1", Kind: "A", Name: "a", UID: types.UID("uA")}
		b := metav1.OwnerReference{APIVersion: "v1", Kind: "B", Name: "b", UID: types.UID("uB")}
		obj.SetOwnerReferences([]metav1.OwnerReference{a, b})

		replB := metav1.OwnerReference{APIVersion: "v2", Kind: "B2", Name: "b2", UID: types.UID("uB")}
		if err := meta.SetLooseOwnerReference(obj, replB); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}

		refs := obj.GetOwnerReferences()
		if len(refs) != 2 {
			t.Fatalf("expected 2 ownerrefs, got %d", len(refs))
		}

		// order preserved; only b overwritten
		if !meta.LooseOwnerReferenceEqual(refs[0], a) {
			t.Fatalf("expected first ref unchanged %v, got %v", a, refs[0])
		}
		if !meta.LooseOwnerReferenceEqual(refs[1], replB) {
			t.Fatalf("expected second ref overwritten %v, got %v", replB, refs[1])
		}
	})
}

func TestRemoveLooseOwnerReference(t *testing.T) {
	t.Run("removes by UID", func(t *testing.T) {
		obj := &corev1.ConfigMap{}
		a := metav1.OwnerReference{APIVersion: "v1", Kind: "A", Name: "a", UID: types.UID("uA")}
		b := metav1.OwnerReference{APIVersion: "v1", Kind: "B", Name: "b", UID: types.UID("uB")}
		obj.SetOwnerReferences([]metav1.OwnerReference{a, b})

		meta.RemoveLooseOwnerReference(obj, metav1.OwnerReference{UID: types.UID("uA")})

		refs := obj.GetOwnerReferences()
		if len(refs) != 1 {
			t.Fatalf("expected 1 ownerref remaining, got %d", len(refs))
		}
		if !meta.LooseOwnerReferenceEqual(refs[0], b) {
			t.Fatalf("expected remaining ref %v, got %v", b, refs[0])
		}
	})

	t.Run("no-op when UID not found", func(t *testing.T) {
		obj := &corev1.ConfigMap{}
		a := metav1.OwnerReference{APIVersion: "v1", Kind: "A", Name: "a", UID: types.UID("uA")}
		obj.SetOwnerReferences([]metav1.OwnerReference{a})

		meta.RemoveLooseOwnerReference(obj, metav1.OwnerReference{UID: types.UID("uX")})

		refs := obj.GetOwnerReferences()
		if len(refs) != 1 {
			t.Fatalf("expected 1 ownerref, got %d", len(refs))
		}
		if !meta.LooseOwnerReferenceEqual(refs[0], a) {
			t.Fatalf("expected unchanged ref %v, got %v", a, refs[0])
		}
	})

	t.Run("removes duplicates with same UID", func(t *testing.T) {
		obj := &corev1.ConfigMap{}
		a1 := metav1.OwnerReference{APIVersion: "v1", Kind: "A", Name: "a1", UID: types.UID("uA")}
		a2 := metav1.OwnerReference{APIVersion: "v1", Kind: "A", Name: "a2", UID: types.UID("uA")}
		b := metav1.OwnerReference{APIVersion: "v1", Kind: "B", Name: "b", UID: types.UID("uB")}
		obj.SetOwnerReferences([]metav1.OwnerReference{a1, b, a2})

		meta.RemoveLooseOwnerReference(obj, metav1.OwnerReference{UID: types.UID("uA")})

		refs := obj.GetOwnerReferences()
		if len(refs) != 1 {
			t.Fatalf("expected 1 ownerref remaining, got %d", len(refs))
		}
		if !meta.LooseOwnerReferenceEqual(refs[0], b) {
			t.Fatalf("expected remaining ref %v, got %v", b, refs[0])
		}
	})
}

func TestHasLooseOwnerReference(t *testing.T) {
	t.Run("true when UID present", func(t *testing.T) {
		obj := &corev1.ConfigMap{}
		obj.SetOwnerReferences([]metav1.OwnerReference{{UID: types.UID("u1")}})

		if !meta.HasLooseOwnerReference(obj, metav1.OwnerReference{UID: types.UID("u1")}) {
			t.Fatalf("expected true")
		}
	})

	t.Run("false when UID absent", func(t *testing.T) {
		obj := &corev1.ConfigMap{}
		obj.SetOwnerReferences([]metav1.OwnerReference{{UID: types.UID("u1")}})

		if meta.HasLooseOwnerReference(obj, metav1.OwnerReference{UID: types.UID("u2")}) {
			t.Fatalf("expected false")
		}
	})

	t.Run("false when no ownerrefs", func(t *testing.T) {
		obj := &corev1.ConfigMap{}
		if meta.HasLooseOwnerReference(obj, metav1.OwnerReference{UID: types.UID("u1")}) {
			t.Fatalf("expected false")
		}
	})
}

func TestGetLooseOwnerReference(t *testing.T) {
	obj := &corev1.ConfigMap{}
	obj.SetName("cm-1")
	obj.SetUID(types.UID("uid-1"))

	// Ensure GVK is set (GetObjectKind().GroupVersionKind() reads this)
	obj.GetObjectKind().SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "ConfigMap",
	})

	ref := meta.GetLooseOwnerReference(obj)

	// BUG FIXED: APIVersion must be group/version string, not Kind.
	if ref.APIVersion != "v1" {
		t.Fatalf("expected APIVersion==v1, got %q", ref.APIVersion)
	}
	if ref.Kind != "ConfigMap" {
		t.Fatalf("expected Kind==ConfigMap, got %q", ref.Kind)
	}
	if ref.Name != "cm-1" {
		t.Fatalf("expected Name==cm-1, got %q", ref.Name)
	}
	if ref.UID != types.UID("uid-1") {
		t.Fatalf("expected UID==uid-1, got %q", ref.UID)
	}
}

func TestLooseOwnerReferenceEqual(t *testing.T) {
	t.Run("equal when all fields match", func(t *testing.T) {
		a := metav1.OwnerReference{APIVersion: "v1", Kind: "K", Name: "n", UID: types.UID("u")}
		b := metav1.OwnerReference{APIVersion: "v1", Kind: "K", Name: "n", UID: types.UID("u")}
		if !meta.LooseOwnerReferenceEqual(a, b) {
			t.Fatalf("expected equal")
		}
	})

	t.Run("not equal when APIVersion differs", func(t *testing.T) {
		a := metav1.OwnerReference{APIVersion: "v1", Kind: "K", Name: "n", UID: types.UID("u")}
		b := metav1.OwnerReference{APIVersion: "v2", Kind: "K", Name: "n", UID: types.UID("u")}
		if meta.LooseOwnerReferenceEqual(a, b) {
			t.Fatalf("expected not equal")
		}
	})

	t.Run("not equal when Kind differs", func(t *testing.T) {
		a := metav1.OwnerReference{APIVersion: "v1", Kind: "K1", Name: "n", UID: types.UID("u")}
		b := metav1.OwnerReference{APIVersion: "v1", Kind: "K2", Name: "n", UID: types.UID("u")}
		if meta.LooseOwnerReferenceEqual(a, b) {
			t.Fatalf("expected not equal")
		}
	})

	t.Run("not equal when Name differs", func(t *testing.T) {
		a := metav1.OwnerReference{APIVersion: "v1", Kind: "K", Name: "n1", UID: types.UID("u")}
		b := metav1.OwnerReference{APIVersion: "v1", Kind: "K", Name: "n2", UID: types.UID("u")}
		if meta.LooseOwnerReferenceEqual(a, b) {
			t.Fatalf("expected not equal")
		}
	})

	t.Run("not equal when UID differs", func(t *testing.T) {
		a := metav1.OwnerReference{APIVersion: "v1", Kind: "K", Name: "n", UID: types.UID("u1")}
		b := metav1.OwnerReference{APIVersion: "v1", Kind: "K", Name: "n", UID: types.UID("u2")}
		if meta.LooseOwnerReferenceEqual(a, b) {
			t.Fatalf("expected not equal")
		}
	})
}
