package template

import (
	"testing"

	"k8s.io/apimachinery/pkg/util/sets"
)

func TestNewNamespaceValidator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                         string
		allowCrossNamespaceSelection bool
		allowed                      sets.Set[string]
		namespace                    string
		wantErr                      bool
		wantErrMsg                   string
	}{
		{
			name:                         "allows empty namespace",
			allowCrossNamespaceSelection: false,
			allowed:                      sets.New[string]("solar-one", "solar-two"),
			namespace:                    "",
			wantErr:                      false,
		},
		{
			name:                         "allows any namespace when cross namespace selection is enabled",
			allowCrossNamespaceSelection: true,
			allowed:                      sets.New[string]("solar-one"),
			namespace:                    "kube-system",
			wantErr:                      false,
		},
		{
			name:                         "allows namespace contained in allowed set",
			allowCrossNamespaceSelection: false,
			allowed:                      sets.New[string]("solar-one", "solar-two"),
			namespace:                    "solar-two",
			wantErr:                      false,
		},
		{
			name:                         "rejects namespace not contained in allowed set",
			allowCrossNamespaceSelection: false,
			allowed:                      sets.New[string]("solar-one", "solar-two"),
			namespace:                    "kube-system",
			wantErr:                      true,
			wantErrMsg:                   "cross-namespace selection is not allowed. Referring a Namespace (kube-system) that is not part of the allowed namespaces",
		},
		{
			name:                         "rejects namespace when allowed set is nil",
			allowCrossNamespaceSelection: false,
			allowed:                      nil,
			namespace:                    "kube-system",
			wantErr:                      true,
			wantErrMsg:                   "cross-namespace selection is not allowed. Referring a Namespace (kube-system) that is not part of the allowed namespaces",
		},
		{
			name:                         "allows namespace when allowed set has exactly that namespace",
			allowCrossNamespaceSelection: false,
			allowed:                      sets.New[string]("kube-system"),
			namespace:                    "kube-system",
			wantErr:                      false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			validate := NewNamespaceValidator(tt.allowCrossNamespaceSelection, tt.allowed)
			err := validate(tt.namespace)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if got := err.Error(); got == "" || !contains(got, tt.wantErrMsg) {
					t.Fatalf("unexpected error message: %q", got)
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}
