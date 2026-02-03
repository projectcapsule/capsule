package predicates_test

import (
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

func TestUpdatedLabelsPredicate_StaticEvents(t *testing.T) {
	g := NewWithT(t)
	p := predicates.UpdatedLabelsPredicate{}

	g.Expect(p.Create(event.CreateEvent{})).To(BeTrue())
	g.Expect(p.Delete(event.DeleteEvent{})).To(BeTrue())
	g.Expect(p.Generic(event.GenericEvent{})).To(BeFalse())
}

func TestUpdatedLabelsPredicate_Update(t *testing.T) {
	pod := func(labels map[string]string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "p",
				Labels:    labels,
			},
		}
	}

	p := predicates.UpdatedLabelsPredicate{}

	tests := []struct {
		name string
		old  *corev1.Pod
		new  *corev1.Pod
		want bool
	}{
		{
			name: "nil old => false",
			old:  pod(nil),
			new:  pod(map[string]string{"a": "1"}),
			want: true,
		},
		{
			name: "nil new => false",
			old:  pod(map[string]string{"a": "1"}),
			new:  pod(nil),
			want: true,
		},
		{
			name: "both nil => false",
			old:  pod(nil),
			new:  pod(nil),
			want: false,
		},
		{
			name: "labels unchanged => false",
			old:  pod(map[string]string{"a": "1", "b": "2"}),
			new:  pod(map[string]string{"b": "2", "a": "1"}), // order irrelevant
			want: false,
		},
		{
			name: "label value changed => true",
			old:  pod(map[string]string{"a": "1"}),
			new:  pod(map[string]string{"a": "2"}),
			want: true,
		},
		{
			name: "label added => true",
			old:  pod(map[string]string{"a": "1"}),
			new:  pod(map[string]string{"a": "1", "b": "2"}),
			want: true,
		},
		{
			name: "label removed => true",
			old:  pod(map[string]string{"a": "1", "b": "2"}),
			new:  pod(map[string]string{"a": "1"}),
			want: true,
		},
		{
			name: "nil vs empty labels => unchanged (MapEqual treats both len==0 as equal)",
			old:  pod(nil),
			new:  pod(map[string]string{}),
			want: false,
		},
		{
			name: "empty vs nil labels => unchanged (MapEqual treats both len==0 as equal)",
			old:  pod(map[string]string{}),
			new:  pod(nil),
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			ev := event.UpdateEvent{
				ObjectOld: tt.old,
				ObjectNew: tt.new,
			}

			got := p.Update(ev)
			g.Expect(got).To(Equal(tt.want))
		})
	}
}
