package api

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"k8s.io/apiserver/pkg/authentication/user"
)

func TestServiceAccountReference_GetFullName(t *testing.T) {
	ref := ServiceAccountReference{
		Name:      Name("my-sa"),
		Namespace: Name("my-ns"),
	}

	expected := fmt.Sprintf("%smy-ns:my-sa", serviceaccount.ServiceAccountUsernamePrefix)
	assert.Equal(t, expected, ref.GetFullName())
}

func TestServiceAccountReference_GetAttributes_Success(t *testing.T) {
	ref := ServiceAccountReference{
		Name:      Name("my-sa"),
		Namespace: Name("my-ns"),
	}

	name, namespace, groups, err := ref.GetAttributes()
	assert.NoError(t, err)
	assert.Equal(t, "my-sa", name)
	assert.Equal(t, "my-ns", namespace)
	assert.Contains(t, groups, serviceaccount.ServiceAccountGroupPrefix+"my-ns")
	assert.Contains(t, groups, serviceaccount.AllServiceAccountsGroup)
	assert.Contains(t, groups, user.AllAuthenticated)
}

func TestServiceAccountReference_GetAttributes_Invalid(t *testing.T) {
	// Invalid because name or namespace is empty
	ref := ServiceAccountReference{
		Name:      Name(""),
		Namespace: Name(""),
	}

	name, namespace, groups, err := ref.GetAttributes()
	assert.Error(t, err)
	assert.Empty(t, name)
	assert.Empty(t, namespace)
	assert.Empty(t, groups)
}
