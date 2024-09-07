package pod

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// struct indicates [value(url that should check), bool(expect result)]
type RegistryValueAndExpected struct {
	Value    string
	Expected bool
}

func checkContainerRegistryContainerRegexp(value string) bool {
	checkRegistry := NewRegistry(value)
	if checkRegistry.Registry() == "" || checkRegistry.Image() == "" || checkRegistry.Tag() == "" {
		return false
	}
	return true
}

func TestContainerRegistry_Registry_Regexp(t *testing.T) {
	resgistryValueAndExpectValue := []RegistryValueAndExpected{
		{Value: "some-Domain.domain.name/linux:latest", Expected: true},
		{Value: "some-Domain.domain.name/repository/linux:latest", Expected: true},
		{Value: "111.some-Domain.domain.com/linux:latest", Expected: true},
		{Value: "some-Domain.domain.com:8080/linux:latest", Expected: true},
		// check whether registry starts with valid characters
		{Value: "-111.some-Domain.domain.com/linux:latest", Expected: false},
		{Value: ".111.some-Domain.domain.com/linux:latest", Expected: false},
		{Value: "_111.some-Domain.domain.com/linux:latest", Expected: false},
		// check whether registry content should not include invalid characters
		{Value: "111.some_Domain.domain.com/linux:latest", Expected: false},
		// check whether registry ends with valid characters
		{Value: "111.some-Domain.domain.co-/linux:latest", Expected: false},
		{Value: "111.some-Domain.domain.co./linux:latest", Expected: false},
		{Value: "111.some-Domain.domain.co_/linux:latest", Expected: false},
	}
	for _, testStruct := range resgistryValueAndExpectValue {
		actualValue := checkContainerRegistryContainerRegexp(testStruct.Value)
		if actualValue == true {
			assert.True(t, actualValue)
		} else {
			assert.False(t, actualValue)
		}
	}
}
