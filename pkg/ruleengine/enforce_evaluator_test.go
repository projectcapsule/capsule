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
	Name         string
	ShouldMatch  bool
	MatchedValue any
	Detail       string
	Err          error
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
	validSet := validFixture.set("registry", nil)

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
				Name:    "registry",
				Rules:   validSet.Rules,
				Matches: validSet.Matches,
			},
			wantErr: "registry: values extractor is nil",
		},
		{
			name: "nil rules extractor",
			set: Set[testRule, testObject]{
				Name:    "registry",
				Values:  validSet.Values,
				Matches: validSet.Matches,
			},
			wantErr: "registry: rules extractor is nil",
		},
		{
			name: "nil matcher",
			set: Set[testRule, testObject]{
				Name:   "registry",
				Values: validSet.Values,
				Rules:  validSet.Rules,
			},
			wantErr: "registry: matcher is nil",
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			evaluation, err := EvaluateEnforce(
				testObject{Values: []Value{{Value: "harbor/app:1", Path: "spec.containers[0].image"}}},
				[]*api.NamespaceRuleEnforceBody{{Action: api.ActionTypeAllow}},
				tt.set,
			)

			if err == nil {
				t.Fatalf("expected error, got nil")
			}

			if evaluation != nil {
				t.Fatalf("expected nil evaluation on setup validation error, got %#v", evaluation)
			}

			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
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
					items: []testRule{
						{Name: "deny", ShouldMatch: true},
					},
				},
			},
		},
		{
			name: "empty value is skipped",
			obj: testObject{
				Values: []Value{
					{Value: "", Path: "spec.value"},
				},
			},
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeDeny,
					items: []testRule{
						{Name: "deny", ShouldMatch: true},
					},
				},
			},
		},
		{
			name: "nil enforce bodies",
			obj: testObject{
				Values: []Value{
					{Value: "harbor/app:1", Path: "spec.containers[0].image"},
				},
			},
		},
		{
			name: "empty enforce bodies",
			obj: testObject{
				Values: []Value{
					{Value: "harbor/app:1", Path: "spec.containers[0].image"},
				},
			},
			enforceSpecs: []enforceSpec{},
		},
		{
			name: "nil enforce body is ignored",
			obj: testObject{
				Values: []Value{
					{Value: "harbor/app:1", Path: "spec.containers[0].image"},
				},
			},
			includeNilBody: true,
		},
		{
			name: "enforce body without rule items is ignored",
			obj: testObject{
				Values: []Value{
					{Value: "harbor/app:1", Path: "spec.containers[0].image"},
				},
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
				Values: []Value{
					{Value: "harbor/app:1", Path: "spec.containers[0].image"},
				},
			},
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeDeny,
					items: []testRule{
						{Name: "deny", ShouldMatch: false},
					},
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
				t.Fatalf("unexpected error: %v", err)
			}

			assertNoBlocking(t, evaluation)
			assertNoFinal(t, evaluation)

			if len(evaluation.Audits) != 0 {
				t.Fatalf("expected no audits, got %d", len(evaluation.Audits))
			}
		})
	}
}

func TestEvaluateEnforce_LastMatchingAllowDenyWins(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		enforceSpecs    []enforceSpec
		wantFinalAction api.ActionType
		wantBlocking    bool
		wantMessage     string
	}{
		{
			name: "single allow allows",
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeAllow,
					items: []testRule{
						{Name: "harbor/.*", ShouldMatch: true},
					},
				},
			},
			wantFinalAction: api.ActionTypeAllow,
			wantMessage:     `registry "harbor/app:1" at spec.containers[0].image is allowed by namespace rule: matched allowed rule harbor/.*`,
		},
		{
			name: "single deny denies",
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeDeny,
					items: []testRule{
						{Name: "harbor/blocked/.*", ShouldMatch: true},
					},
				},
			},
			wantFinalAction: api.ActionTypeDeny,
			wantBlocking:    true,
			wantMessage:     `registry "harbor/app:1" at spec.containers[0].image is denied by namespace rule: matched denied rule harbor/blocked/.*`,
		},
		{
			name: "deny then allow allows",
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeDeny,
					items: []testRule{
						{Name: "harbor/.*", ShouldMatch: true},
					},
				},
				{
					action: api.ActionTypeAllow,
					items: []testRule{
						{Name: "harbor/app:.*", ShouldMatch: true},
					},
				},
			},
			wantFinalAction: api.ActionTypeAllow,
			wantMessage:     `registry "harbor/app:1" at spec.containers[0].image is allowed by namespace rule: matched allowed rule harbor/app:.*`,
		},
		{
			name: "allow then deny denies",
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeAllow,
					items: []testRule{
						{Name: "harbor/.*", ShouldMatch: true},
					},
				},
				{
					action: api.ActionTypeDeny,
					items: []testRule{
						{Name: "harbor/app:.*", ShouldMatch: true},
					},
				},
			},
			wantFinalAction: api.ActionTypeDeny,
			wantBlocking:    true,
			wantMessage:     `registry "harbor/app:1" at spec.containers[0].image is denied by namespace rule: matched denied rule harbor/app:.*`,
		},
		{
			name: "last matching rule wins while non matching later rules are ignored",
			enforceSpecs: []enforceSpec{
				{
					action: api.ActionTypeDeny,
					items: []testRule{
						{Name: "first-deny", ShouldMatch: true},
					},
				},
				{
					action: api.ActionTypeAllow,
					items: []testRule{
						{Name: "allow", ShouldMatch: true},
					},
				},
				{
					action: api.ActionTypeDeny,
					items: []testRule{
						{Name: "later-non-matching-deny", ShouldMatch: false},
					},
				},
			},
			wantFinalAction: api.ActionTypeAllow,
			wantMessage:     `registry "harbor/app:1" at spec.containers[0].image is allowed by namespace rule: matched allowed rule allow`,
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fixture := newTestFixture()
			evaluation, err := EvaluateEnforce(
				testObject{
					Values: []Value{
						{Value: "harbor/app:1", Path: "spec.containers[0].image"},
					},
				},
				buildEnforceBodies(fixture, tt.enforceSpecs),
				fixture.set("registry", nil),
			)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			assertFinalAction(t, evaluation, tt.wantFinalAction)

			if tt.wantBlocking {
				assertBlockingAction(t, evaluation, api.ActionTypeDeny)
			} else {
				assertNoBlocking(t, evaluation)
			}

			if evaluation.Final.Message != tt.wantMessage {
				t.Fatalf("final message = %q, want %q", evaluation.Final.Message, tt.wantMessage)
			}
		})
	}
}

func TestEvaluateEnforce_DefaultActionIsDeny(t *testing.T) {
	t.Parallel()

	fixture := newTestFixture()

	evaluation, err := EvaluateEnforce(
		testObject{
			Values: []Value{
				{Value: "harbor/app:1", Path: "spec.containers[0].image"},
			},
		},
		buildEnforceBodies(fixture, []enforceSpec{
			{
				action: "",
				items: []testRule{
					{Name: "default-deny", ShouldMatch: true},
				},
			},
		}),
		fixture.set("registry", nil),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertBlockingAction(t, evaluation, api.ActionTypeDeny)

	if evaluation.Blocking.Message != `registry "harbor/app:1" at spec.containers[0].image is denied by namespace rule: matched denied rule default-deny` {
		t.Fatalf("blocking message = %q", evaluation.Blocking.Message)
	}
}

func TestEvaluateEnforce_AllowListMissDenies(t *testing.T) {
	t.Parallel()

	fixture := newTestFixture()

	evaluation, err := EvaluateEnforce(
		testObject{
			Values: []Value{
				{Value: "docker.io/library/nginx:latest", Path: "spec.containers[0].image"},
			},
		},
		buildEnforceBodies(fixture, []enforceSpec{
			{
				action: api.ActionTypeAllow,
				items: []testRule{
					{Name: "harbor/.*", ShouldMatch: false},
					{Name: "registry.local/.*", ShouldMatch: false},
				},
			},
		}),
		fixture.set("registry", nil),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertBlockingAction(t, evaluation, api.ActionTypeDeny)
	assertNoFinal(t, evaluation)

	want := `registry "docker.io/library/nginx:latest" at spec.containers[0].image is not allowed by namespace rule: value did not match any allowed rule. Allowed registries: harbor/.*, registry.local/.*`
	if evaluation.Blocking.Message != want {
		t.Fatalf("blocking message = %q, want %q", evaluation.Blocking.Message, want)
	}
}

func TestEvaluateEnforce_AllowMissWithoutRuleDescriptionsUsesBaseMessage(t *testing.T) {
	t.Parallel()

	fixture := newTestFixture()

	set := fixture.set("registry", nil)
	set.RuleDescription = nil
	set.AllowedDescription = ""

	evaluation, err := EvaluateEnforce(
		testObject{
			Values: []Value{
				{Value: "docker.io/library/nginx:latest", Path: "spec.containers[0].image"},
			},
		},
		buildEnforceBodies(fixture, []enforceSpec{
			{
				action: api.ActionTypeAllow,
				items: []testRule{
					{Name: "harbor/.*", ShouldMatch: false},
				},
			},
		}),
		set,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertBlockingAction(t, evaluation, api.ActionTypeDeny)

	want := `registry "docker.io/library/nginx:latest" at spec.containers[0].image is not allowed by namespace rule`
	if evaluation.Blocking.Message != want {
		t.Fatalf("blocking message = %q, want %q", evaluation.Blocking.Message, want)
	}
}

func TestEvaluateEnforce_AllowMissDescriptionsAreLimited(t *testing.T) {
	t.Parallel()

	fixture := newTestFixture()

	items := make([]testRule, 0, 12)
	for i := 0; i < 12; i++ {
		items = append(items, testRule{
			Name:        "rule-" + string(rune('a'+i)),
			ShouldMatch: false,
		})
	}

	evaluation, err := EvaluateEnforce(
		testObject{
			Values: []Value{
				{Value: "unmatched", Path: "spec.value"},
			},
		},
		buildEnforceBodies(fixture, []enforceSpec{
			{
				action: api.ActionTypeAllow,
				items:  items,
			},
		}),
		fixture.set("registry", nil),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertBlockingAction(t, evaluation, api.ActionTypeDeny)

	msg := evaluation.Blocking.Message

	for _, expected := range []string{
		"rule-a",
		"rule-j",
		"and 2 more",
	} {
		if !strings.Contains(msg, expected) {
			t.Fatalf("expected message %q to contain %q", msg, expected)
		}
	}

	for _, unexpected := range []string{
		"rule-k",
		"rule-l",
	} {
		if strings.Contains(msg, unexpected) {
			t.Fatalf("expected message %q not to contain %q", msg, unexpected)
		}
	}
}

func TestEvaluateEnforce_AuditIsObservational(t *testing.T) {
	t.Parallel()

	fixture := newTestFixture()

	evaluation, err := EvaluateEnforce(
		testObject{
			Values: []Value{
				{Value: "audit/app:1", Path: "spec.containers[0].image"},
			},
		},
		buildEnforceBodies(fixture, []enforceSpec{
			{
				action: api.ActionTypeAudit,
				items: []testRule{
					{
						Name:        "audit/.*",
						ShouldMatch: true,
						Detail:      `"audit/app:1" matched registry rule audit/.*`,
					},
				},
			},
		}),
		fixture.set("registry", nil),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertNoBlocking(t, evaluation)
	assertNoFinal(t, evaluation)

	if len(evaluation.Audits) != 1 {
		t.Fatalf("expected 1 audit, got %d", len(evaluation.Audits))
	}

	audit := evaluation.Audits[0]

	if audit.Action != api.ActionTypeAudit {
		t.Fatalf("audit action = %q, want %q", audit.Action, api.ActionTypeAudit)
	}

	if audit.MatchedRule != "audit/.*" {
		t.Fatalf("audit matched rule = %q, want %q", audit.MatchedRule, "audit/.*")
	}

	if audit.MatchDetail != `"audit/app:1" matched registry rule audit/.*` {
		t.Fatalf("audit match detail = %q", audit.MatchDetail)
	}

	want := `registry "audit/app:1" at spec.containers[0].image matched audit namespace rule: "audit/app:1" matched registry rule audit/.*`
	if audit.Message != want {
		t.Fatalf("audit message = %q, want %q", audit.Message, want)
	}
}

func TestEvaluateEnforce_AuditDoesNotSatisfyAllowList(t *testing.T) {
	t.Parallel()

	fixture := newTestFixture()

	evaluation, err := EvaluateEnforce(
		testObject{
			Values: []Value{
				{Value: "audit/app:1", Path: "spec.containers[0].image"},
			},
		},
		buildEnforceBodies(fixture, []enforceSpec{
			{
				action: api.ActionTypeAudit,
				items: []testRule{
					{Name: "audit/.*", ShouldMatch: true},
				},
			},
			{
				action: api.ActionTypeAllow,
				items: []testRule{
					{Name: "allowed/.*", ShouldMatch: false},
				},
			},
		}),
		fixture.set("registry", nil),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(evaluation.Audits) != 1 {
		t.Fatalf("expected 1 audit, got %d", len(evaluation.Audits))
	}

	assertBlockingAction(t, evaluation, api.ActionTypeDeny)
	assertNoFinal(t, evaluation)

	if !strings.Contains(evaluation.Blocking.Message, "Allowed registries: allowed/.*") {
		t.Fatalf("blocking message = %q", evaluation.Blocking.Message)
	}
}

func TestEvaluateEnforce_MatchDetailOverridesMatchedRuleInMessage(t *testing.T) {
	t.Parallel()

	fixture := newTestFixture()

	evaluation, err := EvaluateEnforce(
		testObject{
			Values: []Value{
				{Value: "10.0.171.239", Path: "spec.loadBalancerIP"},
			},
		},
		buildEnforceBodies(fixture, []enforceSpec{
			{
				action: api.ActionTypeDeny,
				items: []testRule{
					{
						Name:         "10.0.0.0/8",
						ShouldMatch:  true,
						MatchedValue: "10.0.0.0/8",
						Detail:       "10.0.171.239 is contained in 10.0.0.0/8",
					},
				},
			},
		}),
		fixture.set("loadBalancer CIDR", nil),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertBlockingAction(t, evaluation, api.ActionTypeDeny)

	blocking := evaluation.Blocking

	if blocking.MatchedRule != "10.0.0.0/8" {
		t.Fatalf("matched rule = %q, want %q", blocking.MatchedRule, "10.0.0.0/8")
	}

	if blocking.MatchDetail != "10.0.171.239 is contained in 10.0.0.0/8" {
		t.Fatalf("match detail = %q", blocking.MatchDetail)
	}

	want := `loadBalancer CIDR "10.0.171.239" at spec.loadBalancerIP is denied by namespace rule: 10.0.171.239 is contained in 10.0.0.0/8`
	if blocking.Message != want {
		t.Fatalf("blocking message = %q, want %q", blocking.Message, want)
	}
}

func TestEvaluateEnforce_CustomMessageOverridesDefaultMessage(t *testing.T) {
	t.Parallel()

	fixture := newTestFixture()

	set := fixture.set("registry", func(action api.ActionType, value Value, matched any) string {
		return "custom message"
	})

	evaluation, err := EvaluateEnforce(
		testObject{
			Values: []Value{
				{Value: "harbor/app:1", Path: "spec.containers[0].image"},
			},
		},
		buildEnforceBodies(fixture, []enforceSpec{
			{
				action: api.ActionTypeDeny,
				items: []testRule{
					{Name: "harbor/.*", ShouldMatch: true},
				},
			},
		}),
		set,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertBlockingAction(t, evaluation, api.ActionTypeDeny)

	if evaluation.Blocking.Message != "custom message" {
		t.Fatalf("blocking message = %q, want custom message", evaluation.Blocking.Message)
	}
}

func TestEvaluateEnforce_MatcherError(t *testing.T) {
	t.Parallel()

	fixture := newTestFixture()
	matchErr := errors.New("invalid regex")

	evaluation, err := EvaluateEnforce(
		testObject{
			Values: []Value{
				{Value: "harbor/app:1", Path: "spec.containers[0].image"},
			},
		},
		buildEnforceBodies(fixture, []enforceSpec{
			{
				action: api.ActionTypeDeny,
				items: []testRule{
					{
						Name:        "bad-rule",
						ShouldMatch: true,
						Err:         matchErr,
					},
				},
			},
		}),
		fixture.set("registry", nil),
	)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	if evaluation == nil {
		t.Fatalf("expected non-nil evaluation after runtime matcher error")
	}

	if !strings.Contains(err.Error(), "registry: invalid rule: invalid regex") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestEvaluateEnforce_UnsupportedAction(t *testing.T) {
	t.Parallel()

	fixture := newTestFixture()

	evaluation, err := EvaluateEnforce(
		testObject{
			Values: []Value{
				{Value: "harbor/app:1", Path: "spec.containers[0].image"},
			},
		},
		buildEnforceBodies(fixture, []enforceSpec{
			{
				action: api.ActionType("invalid"),
				items: []testRule{
					{Name: "harbor/.*", ShouldMatch: true},
				},
			},
		}),
		fixture.set("registry", nil),
	)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	if evaluation == nil {
		t.Fatalf("expected non-nil evaluation after runtime action error")
	}

	if !strings.Contains(err.Error(), `registry: unsupported rule action "invalid"`) {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestEvaluation_BlockingError(t *testing.T) {
	t.Parallel()

	if err := (*Evaluation)(nil).BlockingError(); err != nil {
		t.Fatalf("nil evaluation BlockingError() = %v, want nil", err)
	}

	evaluation := &Evaluation{}
	if err := evaluation.BlockingError(); err != nil {
		t.Fatalf("empty evaluation BlockingError() = %v, want nil", err)
	}

	evaluation.Blocking = &Decision{
		Message: "blocked by rule",
	}

	err := evaluation.BlockingError()
	if err == nil {
		t.Fatalf("expected blocking error, got nil")
	}

	if err.Error() != "blocked by rule" {
		t.Fatalf("blocking error = %q, want %q", err.Error(), "blocked by rule")
	}
}

func TestDecisionError_ErrorFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  *DecisionError
	}{
		{
			name: "nil error",
			err:  nil,
		},
		{
			name: "nil decision",
			err:  &DecisionError{},
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.err.Error() != "namespace rule decision denied request" {
				t.Fatalf("Error() = %q", tt.err.Error())
			}
		})
	}
}

func TestEvaluation_Append(t *testing.T) {
	t.Parallel()

	t.Run("nil receiver or nil other does nothing", func(t *testing.T) {
		t.Parallel()

		var evaluation *Evaluation
		evaluation.Append(&Evaluation{})

		nonNil := &Evaluation{}
		nonNil.Append(nil)

		assertNoBlocking(t, nonNil)
		assertNoFinal(t, nonNil)

		if len(nonNil.Audits) != 0 {
			t.Fatalf("expected no audits, got %d", len(nonNil.Audits))
		}
	})

	t.Run("appends audits and overrides final and blocking", func(t *testing.T) {
		t.Parallel()

		firstAudit := &Decision{Action: api.ActionTypeAudit, Message: "audit-1"}
		secondAudit := &Decision{Action: api.ActionTypeAudit, Message: "audit-2"}
		final := &Decision{Action: api.ActionTypeAllow, Message: "final"}
		blocking := &Decision{Action: api.ActionTypeDeny, Message: "blocking"}

		evaluation := &Evaluation{
			Audits: []*Decision{
				firstAudit,
			},
		}

		evaluation.Append(&Evaluation{
			Audits: []*Decision{
				secondAudit,
			},
			Final:    final,
			Blocking: blocking,
		})

		if len(evaluation.Audits) != 2 {
			t.Fatalf("audits = %d, want 2", len(evaluation.Audits))
		}

		if evaluation.Audits[0] != firstAudit {
			t.Fatalf("first audit pointer was not preserved")
		}

		if evaluation.Audits[1] != secondAudit {
			t.Fatalf("second audit pointer was not appended")
		}

		if evaluation.Final != final {
			t.Fatalf("final was not updated")
		}

		if evaluation.Blocking != blocking {
			t.Fatalf("blocking was not updated")
		}
	})
}

func TestMessageHelpers(t *testing.T) {
	t.Parallel()

	set := Set[testRule, testObject]{
		Name:               "registry",
		AllowedDescription: "Allowed registries",
		RuleDescription: func(rule testRule) string {
			return rule.Name
		},
	}

	t.Run("allowedLabel default", func(t *testing.T) {
		t.Parallel()

		defaultSet := Set[testRule, testObject]{}

		if got := allowedLabel(defaultSet); got != "Allowed values" {
			t.Fatalf("allowedLabel() = %q, want %q", got, "Allowed values")
		}
	})

	t.Run("appendMatchContext uses detail before rule", func(t *testing.T) {
		t.Parallel()

		got := appendMatchContext("message", "rule", "detail", "matched rule")
		if got != "message: detail" {
			t.Fatalf("appendMatchContext() = %q, want %q", got, "message: detail")
		}
	})

	t.Run("appendMatchContext uses rule when detail is empty", func(t *testing.T) {
		t.Parallel()

		got := appendMatchContext("message", "rule", "", "matched rule")
		if got != "message: matched rule rule" {
			t.Fatalf("appendMatchContext() = %q", got)
		}
	})

	t.Run("appendMatchContext leaves message unchanged without context", func(t *testing.T) {
		t.Parallel()

		got := appendMatchContext("message", "", "", "matched rule")
		if got != "message" {
			t.Fatalf("appendMatchContext() = %q, want message", got)
		}
	})

	t.Run("describeRules skips empty descriptions", func(t *testing.T) {
		t.Parallel()

		got := describeRules(set, []testRule{
			{Name: "first"},
			{Name: ""},
			{Name: "second"},
		})

		if got != "first, second" {
			t.Fatalf("describeRules() = %q, want %q", got, "first, second")
		}
	})
}

func newTestFixture() *testFixture {
	return &testFixture{
		items: map[*api.NamespaceRuleEnforceBody][]testRule{},
	}
}

func (f *testFixture) set(
	name string,
	message func(api.ActionType, Value, any) string,
) Set[testRule, testObject] {
	return Set[testRule, testObject]{
		Name:               name,
		EventReason:        "NamespaceRuleViolation",
		AllowedDescription: "Allowed registries",
		Values: func(obj testObject) []Value {
			return obj.Values
		},
		Rules: func(enforce *api.NamespaceRuleEnforceBody) []testRule {
			return f.items[enforce]
		},
		Matches: func(rule testRule, value Value) (Match, error) {
			if rule.Err != nil {
				return Match{}, rule.Err
			}

			if !rule.ShouldMatch {
				return Match{}, nil
			}

			matchedValue := rule.MatchedValue
			if matchedValue == nil {
				matchedValue = rule.Name
			}

			return Match{
				Matched:      true,
				MatchedValue: matchedValue,
				Detail:       rule.Detail,
			}, nil
		},
		Message: message,
		RuleDescription: func(rule testRule) string {
			return rule.Name
		},
	}
}

func buildEnforceBodies(
	fixture *testFixture,
	specs []enforceSpec,
) []*api.NamespaceRuleEnforceBody {
	if len(specs) == 0 {
		return nil
	}

	out := make([]*api.NamespaceRuleEnforceBody, 0, len(specs))

	for _, spec := range specs {
		enforce := &api.NamespaceRuleEnforceBody{
			Action: spec.action,
		}

		fixture.items[enforce] = spec.items
		out = append(out, enforce)
	}

	return out
}

func assertBlockingAction(t *testing.T, evaluation *Evaluation, action api.ActionType) {
	t.Helper()

	if evaluation == nil {
		t.Fatalf("expected evaluation, got nil")
	}

	if evaluation.Blocking == nil {
		t.Fatalf("expected blocking decision, got nil")
	}

	if evaluation.Blocking.Action != action {
		t.Fatalf("blocking action = %q, want %q", evaluation.Blocking.Action, action)
	}
}

func assertNoBlocking(t *testing.T, evaluation *Evaluation) {
	t.Helper()

	if evaluation == nil {
		t.Fatalf("expected evaluation, got nil")
	}

	if evaluation.Blocking != nil {
		t.Fatalf("expected no blocking decision, got %#v", evaluation.Blocking)
	}
}

func assertFinalAction(t *testing.T, evaluation *Evaluation, action api.ActionType) {
	t.Helper()

	if evaluation == nil {
		t.Fatalf("expected evaluation, got nil")
	}

	if evaluation.Final == nil {
		t.Fatalf("expected final decision, got nil")
	}

	if evaluation.Final.Action != action {
		t.Fatalf("final action = %q, want %q", evaluation.Final.Action, action)
	}
}

func assertNoFinal(t *testing.T, evaluation *Evaluation) {
	t.Helper()

	if evaluation == nil {
		t.Fatalf("expected evaluation, got nil")
	}

	if evaluation.Final != nil {
		t.Fatalf("expected no final decision, got %#v", evaluation.Final)
	}
}
