// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"errors"
	"strings"
	"testing"

	api "github.com/projectcapsule/capsule/pkg/api/rules"
)

type testObject struct {
	Values []Value
}

type testRule struct {
	Name        string
	ShouldMatch bool
	MatchValue  any
	Err         error
}

type enforceSpec struct {
	action api.ActionType
	items  []testRule
}

type testFixture struct {
	items map[*api.NamespaceRuleEnforceBody][]testRule
}

func TestEvaluateEnforce_ValidationErrors(t *testing.T) {
	t.Parallel()

	validFixture := newTestFixture()

	validSet := validFixture.set("test", nil)

	tests := []struct {
		name    string
		set     Set[testRule, testObject]
		wantErr string
	}{
		{
			name: "empty set name",
			set: Set[testRule, testObject]{
				Values:  validSet.Values,
				Rules:   validSet.Rules,
				Matches: validSet.Matches,
			},
			wantErr: "rule set name is empty",
		},
		{
			name: "nil values extractor",
			set: Set[testRule, testObject]{
				Name:    "test",
				Rules:   validSet.Rules,
				Matches: validSet.Matches,
			},
			wantErr: "test: values extractor is nil",
		},
		{
			name: "nil rules extractor",
			set: Set[testRule, testObject]{
				Name:    "test",
				Values:  validSet.Values,
				Matches: validSet.Matches,
			},
			wantErr: "test: rules extractor is nil",
		},
		{
			name: "nil matcher",
			set: Set[testRule, testObject]{
				Name:   "test",
				Values: validSet.Values,
				Rules:  validSet.Rules,
			},
			wantErr: "test: matcher is nil",
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			evaluation, err := EvaluateEnforce(
				testObject{Values: []Value{{Value: "a", Path: "spec.value"}}},
				[]*api.NamespaceRuleEnforceBody{{Action: api.ActionTypeAllow}},
				tt.set,
			)

			if err == nil {
				t.Fatalf("EvaluateEnforce() expected error, got nil")
			}

			if evaluation != nil {
				t.Fatalf("EvaluateEnforce() evaluation = %#v, want nil on validation error", evaluation)
			}

			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("EvaluateEnforce() error = %q, want containing %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestEvaluateEnforce_EmptyInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		obj            testObject
		enforceSpecs   []enforceSpec
		includeNilBody bool
	}{
		{
			name: "no values",
			obj:  testObject{},
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeDeny,
					items:  []testRule{{Name: "deny", ShouldMatch: true}},
				},
			},
		},
		{
			name: "only empty value",
			obj: testObject{
				Values: []Value{{Value: "", Path: "spec.value"}},
			},
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeDeny,
					items:  []testRule{{Name: "deny", ShouldMatch: true}},
				},
			},
		},
		{
			name: "nil enforce bodies",
			obj: testObject{
				Values: []Value{{Value: "a", Path: "spec.value"}},
			},
			enforceSpecs: nil,
		},
		{
			name: "empty enforce bodies",
			obj: testObject{
				Values: []Value{{Value: "a", Path: "spec.value"}},
			},
			enforceSpecs: []enforceSpec{},
		},
		{
			name: "nil enforce body is ignored",
			obj: testObject{
				Values: []Value{{Value: "a", Path: "spec.value"}},
			},
			includeNilBody: true,
		},
		{
			name: "enforce body without rule items is ignored",
			obj: testObject{
				Values: []Value{{Value: "a", Path: "spec.value"}},
			},
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeDeny,
				},
			},
		},
		{
			name: "non matching rule item is ignored",
			obj: testObject{
				Values: []Value{{Value: "a", Path: "spec.value"}},
			},
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeDeny,
					items:  []testRule{{Name: "deny", ShouldMatch: false}},
				},
			},
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fixture := newTestFixture()
			enforceBodies := buildEnforceBodies(fixture, tt.enforceSpecs)

			if tt.includeNilBody {
				enforceBodies = append(enforceBodies, nil)
			}

			evaluation, err := EvaluateEnforce(tt.obj, enforceBodies, fixture.set("registry", nil))
			if err != nil {
				t.Fatalf("EvaluateEnforce() unexpected error: %v", err)
			}

			assertNoBlocking(t, evaluation)
			assertNoFinal(t, evaluation)

			if len(evaluation.Audits) != 0 {
				t.Fatalf("audits = %d, want 0", len(evaluation.Audits))
			}
		})
	}
}

func TestEvaluateEnforce_LastMatchingAllowDenyWins(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		enforceSpecs    []enforceSpec
		wantBlocking    bool
		wantFinalAction api.ActionType
	}{
		{
			name: "single allow allows",
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeAllow,
					items:  []testRule{{Name: "allow", ShouldMatch: true}},
				},
			},
			wantFinalAction: api.ActionTypeAllow,
		},
		{
			name: "single deny denies",
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeDeny,
					items:  []testRule{{Name: "deny", ShouldMatch: true}},
				},
			},
			wantBlocking:    true,
			wantFinalAction: api.ActionTypeDeny,
		},
		{
			name: "deny then allow allows",
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeDeny,
					items:  []testRule{{Name: "deny", ShouldMatch: true}},
				},
				{
					action: api.ActionTypeAllow,
					items:  []testRule{{Name: "allow", ShouldMatch: true}},
				},
			},
			wantFinalAction: api.ActionTypeAllow,
		},
		{
			name: "allow then deny denies",
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeAllow,
					items:  []testRule{{Name: "allow", ShouldMatch: true}},
				},
				{
					action: api.ActionTypeDeny,
					items:  []testRule{{Name: "deny", ShouldMatch: true}},
				},
			},
			wantBlocking:    true,
			wantFinalAction: api.ActionTypeDeny,
		},
		{
			name: "unmatched later deny does not override previous allow",
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeAllow,
					items:  []testRule{{Name: "allow", ShouldMatch: true}},
				},
				{
					action: api.ActionTypeDeny,
					items:  []testRule{{Name: "deny", ShouldMatch: false}},
				},
			},
			wantFinalAction: api.ActionTypeAllow,
		},
		{
			name: "unmatched later allow does not override previous deny",
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeDeny,
					items:  []testRule{{Name: "deny", ShouldMatch: true}},
				},
				{
					action: api.ActionTypeAllow,
					items:  []testRule{{Name: "allow", ShouldMatch: false}},
				},
			},
			wantBlocking:    true,
			wantFinalAction: api.ActionTypeDeny,
		},
		{
			name: "multiple matching decisive rules last allow wins",
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeAllow,
					items:  []testRule{{Name: "allow-1", ShouldMatch: true}},
				},
				{
					action: api.ActionTypeDeny,
					items:  []testRule{{Name: "deny", ShouldMatch: true}},
				},
				{
					action: api.ActionTypeAllow,
					items:  []testRule{{Name: "allow-2", ShouldMatch: true}},
				},
			},
			wantFinalAction: api.ActionTypeAllow,
		},
		{
			name: "multiple matching decisive rules last deny wins",
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeDeny,
					items:  []testRule{{Name: "deny-1", ShouldMatch: true}},
				},
				{
					action: api.ActionTypeAllow,
					items:  []testRule{{Name: "allow", ShouldMatch: true}},
				},
				{
					action: api.ActionTypeDeny,
					items:  []testRule{{Name: "deny-2", ShouldMatch: true}},
				},
			},
			wantBlocking:    true,
			wantFinalAction: api.ActionTypeDeny,
		},
		{
			name: "only unmatched allow does not implicitly deny",
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeAllow,
					items:  []testRule{{Name: "allow", ShouldMatch: false}},
				},
			},
		},
		{
			name: "only unmatched deny allows",
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeDeny,
					items:  []testRule{{Name: "deny", ShouldMatch: false}},
				},
			},
		},
		{
			name: "unrelated allow before unrelated deny does not block",
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeAllow,
					items:  []testRule{{Name: "allow", ShouldMatch: false}},
				},
				{
					action: api.ActionTypeDeny,
					items:  []testRule{{Name: "deny", ShouldMatch: false}},
				},
			},
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fixture := newTestFixture()

			evaluation, err := EvaluateEnforce(
				testObject{Values: []Value{{Value: "app:1", Path: "containers[0]"}}},
				buildEnforceBodies(fixture, tt.enforceSpecs),
				fixture.set("registry", nil),
			)
			if err != nil {
				t.Fatalf("EvaluateEnforce() unexpected error: %v", err)
			}

			if tt.wantBlocking {
				if evaluation.Blocking == nil {
					t.Fatalf("Blocking = nil, want decision")
				}

				if err := evaluation.BlockingError(); err == nil {
					t.Fatalf("BlockingError() = nil, want error")
				}
			} else {
				assertNoBlocking(t, evaluation)
			}

			if tt.wantFinalAction == "" {
				assertNoFinal(t, evaluation)

				return
			}

			if evaluation.Final == nil {
				t.Fatalf("Final = nil, want action %q", tt.wantFinalAction)
			}

			if evaluation.Final.Action != tt.wantFinalAction {
				t.Fatalf("Final.Action = %q, want %q", evaluation.Final.Action, tt.wantFinalAction)
			}

			if tt.wantBlocking && evaluation.Blocking != evaluation.Final {
				t.Fatalf("Blocking and Final should point to same deny decision")
			}
		})
	}
}

func TestEvaluateEnforce_AuditSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		enforceSpecs []enforceSpec
		wantAudits   int
		wantBlocking bool
		wantFinal    api.ActionType
	}{
		{
			name: "single audit allows and records audit",
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeAudit,
					items:  []testRule{{Name: "audit", ShouldMatch: true}},
				},
			},
			wantAudits: 1,
		},
		{
			name: "multiple audits are all recorded",
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeAudit,
					items:  []testRule{{Name: "audit-1", ShouldMatch: true}},
				},
				{
					action: api.ActionTypeAudit,
					items:  []testRule{{Name: "audit-2", ShouldMatch: true}},
				},
				{
					action: api.ActionTypeAudit,
					items:  []testRule{{Name: "audit-3", ShouldMatch: true}},
				},
			},
			wantAudits: 3,
		},
		{
			name: "unmatched audit is ignored",
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeAudit,
					items:  []testRule{{Name: "audit", ShouldMatch: false}},
				},
			},
			wantAudits: 0,
		},
		{
			name: "audit plus deny records audit and denies",
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeAudit,
					items:  []testRule{{Name: "audit", ShouldMatch: true}},
				},
				{
					action: api.ActionTypeDeny,
					items:  []testRule{{Name: "deny", ShouldMatch: true}},
				},
			},
			wantAudits:   1,
			wantBlocking: true,
			wantFinal:    api.ActionTypeDeny,
		},
		{
			name: "deny plus audit records audit but final deny remains",
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeDeny,
					items:  []testRule{{Name: "deny", ShouldMatch: true}},
				},
				{
					action: api.ActionTypeAudit,
					items:  []testRule{{Name: "audit", ShouldMatch: true}},
				},
			},
			wantAudits:   1,
			wantBlocking: true,
			wantFinal:    api.ActionTypeDeny,
		},
		{
			name: "audit plus allow records audit and allows",
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeAudit,
					items:  []testRule{{Name: "audit", ShouldMatch: true}},
				},
				{
					action: api.ActionTypeAllow,
					items:  []testRule{{Name: "allow", ShouldMatch: true}},
				},
			},
			wantAudits: 1,
			wantFinal:  api.ActionTypeAllow,
		},
		{
			name: "allow plus audit records audit and final allow remains",
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeAllow,
					items:  []testRule{{Name: "allow", ShouldMatch: true}},
				},
				{
					action: api.ActionTypeAudit,
					items:  []testRule{{Name: "audit", ShouldMatch: true}},
				},
			},
			wantAudits: 1,
			wantFinal:  api.ActionTypeAllow,
		},
		{
			name: "audit does not cause implicit deny even with unmatched allow",
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeAllow,
					items:  []testRule{{Name: "allow", ShouldMatch: false}},
				},
				{
					action: api.ActionTypeAudit,
					items:  []testRule{{Name: "audit", ShouldMatch: true}},
				},
			},
			wantAudits: 1,
		},
		{
			name: "audit plus allow plus later deny denies and records audit",
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeAudit,
					items:  []testRule{{Name: "audit", ShouldMatch: true}},
				},
				{
					action: api.ActionTypeAllow,
					items:  []testRule{{Name: "allow", ShouldMatch: true}},
				},
				{
					action: api.ActionTypeDeny,
					items:  []testRule{{Name: "deny", ShouldMatch: true}},
				},
			},
			wantAudits:   1,
			wantBlocking: true,
			wantFinal:    api.ActionTypeDeny,
		},
		{
			name: "audit plus deny plus later allow allows and records audit",
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeAudit,
					items:  []testRule{{Name: "audit", ShouldMatch: true}},
				},
				{
					action: api.ActionTypeDeny,
					items:  []testRule{{Name: "deny", ShouldMatch: true}},
				},
				{
					action: api.ActionTypeAllow,
					items:  []testRule{{Name: "allow", ShouldMatch: true}},
				},
			},
			wantAudits: 1,
			wantFinal:  api.ActionTypeAllow,
		},
		{
			name: "unmatched audit after matching allow does not change final allow",
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeAllow,
					items:  []testRule{{Name: "allow", ShouldMatch: true}},
				},
				{
					action: api.ActionTypeAudit,
					items:  []testRule{{Name: "audit", ShouldMatch: false}},
				},
			},
			wantAudits: 0,
			wantFinal:  api.ActionTypeAllow,
		},
		{
			name: "unmatched audit after matching deny does not change final deny",
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeDeny,
					items:  []testRule{{Name: "deny", ShouldMatch: true}},
				},
				{
					action: api.ActionTypeAudit,
					items:  []testRule{{Name: "audit", ShouldMatch: false}},
				},
			},
			wantAudits:   0,
			wantBlocking: true,
			wantFinal:    api.ActionTypeDeny,
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fixture := newTestFixture()

			evaluation, err := EvaluateEnforce(
				testObject{Values: []Value{{Value: "app:1", Path: "containers[0]"}}},
				buildEnforceBodies(fixture, tt.enforceSpecs),
				fixture.set("registry", nil),
			)
			if err != nil {
				t.Fatalf("EvaluateEnforce() unexpected error: %v", err)
			}

			if len(evaluation.Audits) != tt.wantAudits {
				t.Fatalf("audits = %d, want %d", len(evaluation.Audits), tt.wantAudits)
			}

			for _, audit := range evaluation.Audits {
				if audit.Action != api.ActionTypeAudit {
					t.Fatalf("audit action = %q, want %q", audit.Action, api.ActionTypeAudit)
				}

				if audit.Message == "" {
					t.Fatalf("audit message is empty")
				}
			}

			if tt.wantBlocking {
				if evaluation.Blocking == nil {
					t.Fatalf("Blocking = nil, want decision")
				}
			} else {
				assertNoBlocking(t, evaluation)
			}

			if tt.wantFinal == "" {
				assertNoFinal(t, evaluation)

				return
			}

			if evaluation.Final == nil {
				t.Fatalf("Final = nil, want %q", tt.wantFinal)
			}

			if evaluation.Final.Action != tt.wantFinal {
				t.Fatalf("Final.Action = %q, want %q", evaluation.Final.Action, tt.wantFinal)
			}
		})
	}
}

func TestEvaluateEnforce_MultipleValues(t *testing.T) {
	t.Parallel()

	t.Run("continues after allowed first value and blocks on second value", func(t *testing.T) {
		t.Parallel()

		set := Set[string, testObject]{
			Name:        "registry",
			EventReason: "ForbiddenRegistry",
			Values: func(obj testObject) []Value {
				return obj.Values
			},
			Rules: func(_ *api.NamespaceRuleEnforceBody) []string {
				return []string{"bad"}
			},
			Matches: func(rule string, value Value) (Match, error) {
				return Match{Matched: value.Value == rule}, nil
			},
		}

		evaluation, err := EvaluateEnforce(
			testObject{
				Values: []Value{
					{Value: "good", Path: "containers[0]"},
					{Value: "bad", Path: "containers[1]"},
				},
			},
			[]*api.NamespaceRuleEnforceBody{
				{Action: api.ActionTypeDeny},
			},
			set,
		)
		if err != nil {
			t.Fatalf("EvaluateEnforce() unexpected error: %v", err)
		}

		if evaluation.Blocking == nil {
			t.Fatalf("Blocking = nil, want decision")
		}

		if evaluation.Blocking.Value.Value != "bad" {
			t.Fatalf("Blocking.Value.Value = %q, want %q", evaluation.Blocking.Value.Value, "bad")
		}

		if evaluation.Blocking.Value.Path != "containers[1]" {
			t.Fatalf("Blocking.Value.Path = %q, want %q", evaluation.Blocking.Value.Path, "containers[1]")
		}
	})

	t.Run("skips empty values and evaluates non-empty values", func(t *testing.T) {
		t.Parallel()

		fixture := newTestFixture()

		evaluation, err := EvaluateEnforce(
			testObject{
				Values: []Value{
					{Value: "", Path: "containers[0]"},
					{Value: "bad", Path: "containers[1]"},
				},
			},
			[]*api.NamespaceRuleEnforceBody{
				fixture.enforce(api.ActionTypeDeny, testRule{Name: "deny", ShouldMatch: true}),
			},
			fixture.set("registry", nil),
		)
		if err != nil {
			t.Fatalf("EvaluateEnforce() unexpected error: %v", err)
		}

		if evaluation.Blocking == nil {
			t.Fatalf("Blocking = nil, want decision")
		}

		if evaluation.Blocking.Value.Path != "containers[1]" {
			t.Fatalf("Blocking.Value.Path = %q, want containers[1]", evaluation.Blocking.Value.Path)
		}
	})

	t.Run("audits all matching values", func(t *testing.T) {
		t.Parallel()

		fixture := newTestFixture()

		evaluation, err := EvaluateEnforce(
			testObject{
				Values: []Value{
					{Value: "a", Path: "containers[0]"},
					{Value: "b", Path: "containers[1]"},
				},
			},
			[]*api.NamespaceRuleEnforceBody{
				fixture.enforce(api.ActionTypeAudit, testRule{Name: "audit", ShouldMatch: true}),
			},
			fixture.set("registry", nil),
		)
		if err != nil {
			t.Fatalf("EvaluateEnforce() unexpected error: %v", err)
		}

		if len(evaluation.Audits) != 2 {
			t.Fatalf("audits = %d, want 2", len(evaluation.Audits))
		}

		if evaluation.Audits[0].Value.Path != "containers[0]" {
			t.Fatalf("first audit path = %q, want containers[0]", evaluation.Audits[0].Value.Path)
		}

		if evaluation.Audits[1].Value.Path != "containers[1]" {
			t.Fatalf("second audit path = %q, want containers[1]", evaluation.Audits[1].Value.Path)
		}
	})

	t.Run("stops after first blocking value", func(t *testing.T) {
		t.Parallel()

		set := Set[string, testObject]{
			Name: "registry",
			Values: func(obj testObject) []Value {
				return obj.Values
			},
			Rules: func(_ *api.NamespaceRuleEnforceBody) []string {
				return []string{"bad", "worse"}
			},
			Matches: func(rule string, value Value) (Match, error) {
				return Match{Matched: value.Value == rule}, nil
			},
		}

		evaluation, err := EvaluateEnforce(
			testObject{
				Values: []Value{
					{Value: "bad", Path: "containers[0]"},
					{Value: "worse", Path: "containers[1]"},
				},
			},
			[]*api.NamespaceRuleEnforceBody{
				{Action: api.ActionTypeDeny},
			},
			set,
		)
		if err != nil {
			t.Fatalf("EvaluateEnforce() unexpected error: %v", err)
		}

		if evaluation.Blocking == nil {
			t.Fatalf("Blocking = nil, want decision")
		}

		if evaluation.Blocking.Value.Value != "bad" {
			t.Fatalf("Blocking.Value.Value = %q, want bad", evaluation.Blocking.Value.Value)
		}
	})
}

func TestEvaluateEnforce_MatchedValueAndMessages(t *testing.T) {
	t.Parallel()

	t.Run("matched value is propagated to final decision", func(t *testing.T) {
		t.Parallel()

		fixture := newTestFixture()

		matchedValue := map[string]string{"rule": "compiled-registry-rule"}

		evaluation, err := EvaluateEnforce(
			testObject{Values: []Value{{Value: "app:1", Path: "containers[0]"}}},
			[]*api.NamespaceRuleEnforceBody{
				fixture.enforce(api.ActionTypeAllow, testRule{
					Name:        "allow",
					ShouldMatch: true,
					MatchValue:  matchedValue,
				}),
			},
			fixture.set("registry", nil),
		)
		if err != nil {
			t.Fatalf("EvaluateEnforce() unexpected error: %v", err)
		}

		if evaluation.Final == nil {
			t.Fatalf("Final = nil, want decision")
		}

		got, ok := evaluation.Final.MatchedValue.(map[string]string)
		if !ok {
			t.Fatalf("Final.MatchedValue type = %T, want map[string]string", evaluation.Final.MatchedValue)
		}

		if got["rule"] != "compiled-registry-rule" {
			t.Fatalf("Final.MatchedValue[rule] = %q", got["rule"])
		}
	})

	t.Run("matched value is propagated to blocking decision", func(t *testing.T) {
		t.Parallel()

		fixture := newTestFixture()

		matchedValue := "compiled-deny-rule"

		evaluation, err := EvaluateEnforce(
			testObject{Values: []Value{{Value: "app:1", Path: "containers[0]"}}},
			[]*api.NamespaceRuleEnforceBody{
				fixture.enforce(api.ActionTypeDeny, testRule{
					Name:        "deny",
					ShouldMatch: true,
					MatchValue:  matchedValue,
				}),
			},
			fixture.set("registry", nil),
		)
		if err != nil {
			t.Fatalf("EvaluateEnforce() unexpected error: %v", err)
		}

		if evaluation.Blocking == nil {
			t.Fatalf("Blocking = nil, want decision")
		}

		if evaluation.Blocking.MatchedValue != matchedValue {
			t.Fatalf("Blocking.MatchedValue = %#v, want %#v", evaluation.Blocking.MatchedValue, matchedValue)
		}
	})

	t.Run("matched value is propagated to audit decision", func(t *testing.T) {
		t.Parallel()

		fixture := newTestFixture()

		matchedValue := "compiled-audit-rule"

		evaluation, err := EvaluateEnforce(
			testObject{Values: []Value{{Value: "app:1", Path: "containers[0]"}}},
			[]*api.NamespaceRuleEnforceBody{
				fixture.enforce(api.ActionTypeAudit, testRule{
					Name:        "audit",
					ShouldMatch: true,
					MatchValue:  matchedValue,
				}),
			},
			fixture.set("registry", nil),
		)
		if err != nil {
			t.Fatalf("EvaluateEnforce() unexpected error: %v", err)
		}

		if len(evaluation.Audits) != 1 {
			t.Fatalf("audits = %d, want 1", len(evaluation.Audits))
		}

		if evaluation.Audits[0].MatchedValue != matchedValue {
			t.Fatalf("audit MatchedValue = %#v, want %#v", evaluation.Audits[0].MatchedValue, matchedValue)
		}
	})

	t.Run("custom message is used", func(t *testing.T) {
		t.Parallel()

		fixture := newTestFixture()

		evaluation, err := EvaluateEnforce(
			testObject{Values: []Value{{Value: "app:1", Path: "containers[0]"}}},
			[]*api.NamespaceRuleEnforceBody{
				fixture.enforce(api.ActionTypeDeny, testRule{Name: "deny", ShouldMatch: true}),
			},
			fixture.set("registry", func(action api.ActionType, value Value, matchedValue any) string {
				return "custom: " + string(action) + " " + value.Value
			}),
		)
		if err != nil {
			t.Fatalf("EvaluateEnforce() unexpected error: %v", err)
		}

		if evaluation.Blocking == nil {
			t.Fatalf("Blocking = nil, want decision")
		}

		if evaluation.Blocking.Message != "custom: deny app:1" {
			t.Fatalf("Blocking.Message = %q, want custom message", evaluation.Blocking.Message)
		}
	})

	t.Run("default messages are populated", func(t *testing.T) {
		t.Parallel()

		for _, action := range []api.ActionType{
			api.ActionTypeAllow,
			api.ActionTypeDeny,
			api.ActionTypeAudit,
		} {
			action := action

			t.Run(string(action), func(t *testing.T) {
				t.Parallel()

				fixture := newTestFixture()

				evaluation, err := EvaluateEnforce(
					testObject{Values: []Value{{Value: "app:1", Path: "containers[0]"}}},
					[]*api.NamespaceRuleEnforceBody{
						fixture.enforce(action, testRule{Name: string(action), ShouldMatch: true}),
					},
					fixture.set("registry", nil),
				)
				if err != nil {
					t.Fatalf("EvaluateEnforce() unexpected error: %v", err)
				}

				switch action {
				case api.ActionTypeAllow:
					if evaluation.Final == nil || evaluation.Final.Message == "" {
						t.Fatalf("allow final message is empty")
					}
				case api.ActionTypeDeny:
					if evaluation.Blocking == nil || evaluation.Blocking.Message == "" {
						t.Fatalf("deny blocking message is empty")
					}
				case api.ActionTypeAudit:
					if len(evaluation.Audits) != 1 || evaluation.Audits[0].Message == "" {
						t.Fatalf("audit message is empty")
					}
				}
			})
		}
	})
}

func TestEvaluateEnforce_DefaultActionAndUnsupportedAction(t *testing.T) {
	t.Parallel()

	t.Run("empty action defaults to deny", func(t *testing.T) {
		t.Parallel()

		fixture := newTestFixture()

		evaluation, err := EvaluateEnforce(
			testObject{Values: []Value{{Value: "app:1", Path: "containers[0]"}}},
			[]*api.NamespaceRuleEnforceBody{
				fixture.enforce("", testRule{Name: "default-deny", ShouldMatch: true}),
			},
			fixture.set("registry", nil),
		)
		if err != nil {
			t.Fatalf("EvaluateEnforce() unexpected error: %v", err)
		}

		if evaluation.Blocking == nil {
			t.Fatalf("Blocking = nil, want default deny decision")
		}

		if evaluation.Blocking.Action != api.ActionTypeDeny {
			t.Fatalf("Blocking.Action = %q, want %q", evaluation.Blocking.Action, api.ActionTypeDeny)
		}
	})

	t.Run("unsupported action returns error with partial evaluation", func(t *testing.T) {
		t.Parallel()

		fixture := newTestFixture()

		evaluation, err := EvaluateEnforce(
			testObject{Values: []Value{{Value: "app:1", Path: "containers[0]"}}},
			[]*api.NamespaceRuleEnforceBody{
				fixture.enforce(api.ActionTypeAudit, testRule{Name: "audit", ShouldMatch: true}),
				fixture.enforce(api.ActionType("unsupported"), testRule{Name: "bad", ShouldMatch: true}),
			},
			fixture.set("registry", nil),
		)
		if err == nil {
			t.Fatalf("EvaluateEnforce() expected error, got nil")
		}

		if !strings.Contains(err.Error(), `registry: unsupported rule action "unsupported"`) {
			t.Fatalf("error = %q, want unsupported action message", err.Error())
		}

		if evaluation == nil {
			t.Fatalf("evaluation = nil, want partial evaluation")
		}

		if len(evaluation.Audits) != 1 {
			t.Fatalf("audits = %d, want 1 from partial evaluation", len(evaluation.Audits))
		}
	})
}

func TestEvaluateEnforce_MatcherErrors(t *testing.T) {
	t.Parallel()

	t.Run("matcher error is wrapped and returns partial evaluation", func(t *testing.T) {
		t.Parallel()

		fixture := newTestFixture()

		wantErr := errors.New("matcher failed")

		evaluation, err := EvaluateEnforce(
			testObject{Values: []Value{{Value: "app:1", Path: "containers[0]"}}},
			[]*api.NamespaceRuleEnforceBody{
				fixture.enforce(api.ActionTypeAllow, testRule{Name: "allow", ShouldMatch: true, Err: wantErr}),
			},
			fixture.set("registry", nil),
		)

		if err == nil {
			t.Fatalf("EvaluateEnforce() expected error, got nil")
		}

		if !errors.Is(err, wantErr) {
			t.Fatalf("EvaluateEnforce() error = %v, want wrapping %v", err, wantErr)
		}

		if !strings.Contains(err.Error(), "registry: invalid rule") {
			t.Fatalf("EvaluateEnforce() error = %q, want invalid rule context", err.Error())
		}

		if evaluation == nil {
			t.Fatalf("evaluation = nil, want partial evaluation")
		}
	})

	t.Run("matcher error after audit keeps audit in partial evaluation", func(t *testing.T) {
		t.Parallel()

		fixture := newTestFixture()

		wantErr := errors.New("matcher failed")

		evaluation, err := EvaluateEnforce(
			testObject{Values: []Value{{Value: "app:1", Path: "containers[0]"}}},
			[]*api.NamespaceRuleEnforceBody{
				fixture.enforce(api.ActionTypeAudit, testRule{Name: "audit", ShouldMatch: true}),
				fixture.enforce(api.ActionTypeAllow, testRule{Name: "allow", Err: wantErr}),
			},
			fixture.set("registry", nil),
		)

		if err == nil {
			t.Fatalf("EvaluateEnforce() expected error, got nil")
		}

		if !errors.Is(err, wantErr) {
			t.Fatalf("EvaluateEnforce() error = %v, want wrapping %v", err, wantErr)
		}

		if evaluation == nil {
			t.Fatalf("evaluation = nil, want partial evaluation")
		}

		if len(evaluation.Audits) != 1 {
			t.Fatalf("audits = %d, want 1", len(evaluation.Audits))
		}
	})
}

func TestEvaluation_BlockingError(t *testing.T) {
	t.Parallel()

	t.Run("nil evaluation has no blocking error", func(t *testing.T) {
		t.Parallel()

		var evaluation *Evaluation

		if err := evaluation.BlockingError(); err != nil {
			t.Fatalf("BlockingError() = %v, want nil", err)
		}
	})

	t.Run("evaluation without blocking has no blocking error", func(t *testing.T) {
		t.Parallel()

		evaluation := &Evaluation{}

		if err := evaluation.BlockingError(); err != nil {
			t.Fatalf("BlockingError() = %v, want nil", err)
		}
	})

	t.Run("evaluation with blocking returns decision error", func(t *testing.T) {
		t.Parallel()

		decision := &Decision{
			SetName: "registry",
			Action:  api.ActionTypeDeny,
			Message: "denied",
		}

		evaluation := &Evaluation{
			Blocking: decision,
		}

		err := evaluation.BlockingError()
		if err == nil {
			t.Fatalf("BlockingError() = nil, want error")
		}

		var decisionErr *DecisionError
		if !errors.As(err, &decisionErr) {
			t.Fatalf("BlockingError() type = %T, want *DecisionError", err)
		}

		if decisionErr.Decision != decision {
			t.Fatalf("DecisionError.Decision = %#v, want original decision", decisionErr.Decision)
		}

		if err.Error() != "denied" {
			t.Fatalf("BlockingError().Error() = %q, want denied", err.Error())
		}
	})
}

func TestDecisionError_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  *DecisionError
		want string
	}{
		{
			name: "nil error receiver",
			err:  nil,
			want: "namespace rule decision denied request",
		},
		{
			name: "nil decision",
			err:  &DecisionError{},
			want: "namespace rule decision denied request",
		},
		{
			name: "decision message",
			err: &DecisionError{
				Decision: &Decision{
					Message: "custom denied message",
				},
			},
			want: "custom denied message",
		},
		{
			name: "empty decision message",
			err: &DecisionError{
				Decision: &Decision{},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.err.Error()
			if got != tt.want {
				t.Fatalf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEvaluation_Append(t *testing.T) {
	t.Parallel()

	t.Run("nil receiver is no-op", func(t *testing.T) {
		t.Parallel()

		var evaluation *Evaluation

		evaluation.Append(&Evaluation{
			Audits: []*Decision{{SetName: "audit"}},
			Final:  &Decision{Action: api.ActionTypeAllow},
		})
	})

	t.Run("nil other is no-op", func(t *testing.T) {
		t.Parallel()

		evaluation := &Evaluation{
			Audits: []*Decision{{SetName: "audit-1"}},
			Final:  &Decision{Action: api.ActionTypeAllow},
		}

		evaluation.Append(nil)

		if len(evaluation.Audits) != 1 {
			t.Fatalf("audits = %d, want 1", len(evaluation.Audits))
		}

		if evaluation.Final == nil || evaluation.Final.Action != api.ActionTypeAllow {
			t.Fatalf("Final = %#v, want allow", evaluation.Final)
		}
	})

	t.Run("appends audits and replaces final and blocking", func(t *testing.T) {
		t.Parallel()

		initialFinal := &Decision{SetName: "initial", Action: api.ActionTypeAllow}
		newFinal := &Decision{SetName: "new", Action: api.ActionTypeDeny}
		newBlocking := &Decision{SetName: "new", Action: api.ActionTypeDeny}

		evaluation := &Evaluation{
			Audits: []*Decision{{SetName: "audit-1"}},
			Final:  initialFinal,
		}

		evaluation.Append(&Evaluation{
			Audits:   []*Decision{{SetName: "audit-2"}, {SetName: "audit-3"}},
			Final:    newFinal,
			Blocking: newBlocking,
		})

		if len(evaluation.Audits) != 3 {
			t.Fatalf("audits = %d, want 3", len(evaluation.Audits))
		}

		if evaluation.Final != newFinal {
			t.Fatalf("Final = %#v, want new final", evaluation.Final)
		}

		if evaluation.Blocking != newBlocking {
			t.Fatalf("Blocking = %#v, want new blocking", evaluation.Blocking)
		}
	})

	t.Run("does not replace final or blocking with nil", func(t *testing.T) {
		t.Parallel()

		final := &Decision{SetName: "final", Action: api.ActionTypeAllow}
		blocking := &Decision{SetName: "blocking", Action: api.ActionTypeDeny}

		evaluation := &Evaluation{
			Final:    final,
			Blocking: blocking,
		}

		evaluation.Append(&Evaluation{
			Audits: []*Decision{{SetName: "audit"}},
		})

		if evaluation.Final != final {
			t.Fatalf("Final was replaced unexpectedly")
		}

		if evaluation.Blocking != blocking {
			t.Fatalf("Blocking was replaced unexpectedly")
		}

		if len(evaluation.Audits) != 1 {
			t.Fatalf("audits = %d, want 1", len(evaluation.Audits))
		}
	})
}

func TestDecisionMessage(t *testing.T) {
	t.Parallel()

	set := Set[testRule, testObject]{
		Name: "registry",
	}

	value := Value{
		Value: "harbor/app:1",
		Path:  "containers[0]",
	}

	tests := []struct {
		name   string
		action api.ActionType
		want   string
	}{
		{
			name:   "audit",
			action: api.ActionTypeAudit,
			want:   `registry "harbor/app:1" at containers[0] matched audit namespace rule`,
		},
		{
			name:   "deny",
			action: api.ActionTypeDeny,
			want:   `registry "harbor/app:1" at containers[0] is denied by namespace rule`,
		},
		{
			name:   "allow",
			action: api.ActionTypeAllow,
			want:   `registry "harbor/app:1" at containers[0] is allowed by namespace rule`,
		},
		{
			name:   "unknown",
			action: api.ActionType("custom"),
			want:   `registry "harbor/app:1" at containers[0] matched namespace rule action "custom"`,
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := decisionMessage(set, tt.action, value, nil)
			if got != tt.want {
				t.Fatalf("decisionMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDecisionMessage_CustomMessageReceivesMatchedValue(t *testing.T) {
	t.Parallel()

	set := Set[testRule, testObject]{
		Name: "registry",
		Message: func(action api.ActionType, value Value, matchedValue any) string {
			return string(action) + ":" + value.Value + ":" + matchedValue.(string)
		},
	}

	value := Value{
		Value: "harbor/app:1",
		Path:  "containers[0]",
	}

	got := decisionMessage(set, api.ActionTypeAudit, value, "compiled-rule")
	want := "audit:harbor/app:1:compiled-rule"

	if got != want {
		t.Fatalf("decisionMessage() = %q, want %q", got, want)
	}
}

func newTestFixture() *testFixture {
	return &testFixture{
		items: make(map[*api.NamespaceRuleEnforceBody][]testRule),
	}
}

func (f *testFixture) set(
	name string,
	message func(action api.ActionType, value Value, matchedValue any) string,
) Set[testRule, testObject] {
	return Set[testRule, testObject]{
		Name:        name,
		EventReason: "TestReason",
		Values: func(obj testObject) []Value {
			return obj.Values
		},
		Rules: func(enforce *api.NamespaceRuleEnforceBody) []testRule {
			if enforce == nil {
				return nil
			}

			return f.items[enforce]
		},
		Matches: func(rule testRule, value Value) (Match, error) {
			if rule.Err != nil {
				return Match{}, rule.Err
			}

			return Match{
				Matched:      rule.ShouldMatch,
				MatchedValue: rule.MatchValue,
			}, nil
		},
		Message: message,
	}
}

func (f *testFixture) enforce(
	action api.ActionType,
	rules ...testRule,
) *api.NamespaceRuleEnforceBody {
	body := &api.NamespaceRuleEnforceBody{
		Action: action,
	}

	f.items[body] = rules

	return body
}

func buildEnforceBodies(
	fixture *testFixture,
	specs []enforceSpec,
) []*api.NamespaceRuleEnforceBody {
	out := make([]*api.NamespaceRuleEnforceBody, 0, len(specs))

	for _, spec := range specs {
		out = append(out, fixture.enforce(spec.action, spec.items...))
	}

	return out
}

func assertNoBlocking(t *testing.T, evaluation *Evaluation) {
	t.Helper()

	if evaluation == nil {
		t.Fatalf("evaluation = nil, want non-nil")
	}

	if evaluation.Blocking != nil {
		t.Fatalf("Blocking = %#v, want nil", evaluation.Blocking)
	}

	if err := evaluation.BlockingError(); err != nil {
		t.Fatalf("BlockingError() = %v, want nil", err)
	}
}

func assertNoFinal(t *testing.T, evaluation *Evaluation) {
	t.Helper()

	if evaluation == nil {
		t.Fatalf("evaluation = nil, want non-nil")
	}

	if evaluation.Final != nil {
		t.Fatalf("Final = %#v, want nil", evaluation.Final)
	}
}
