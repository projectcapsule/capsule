package cache

import (
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/pkg/api"
)

func set(ids ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		m[id] = struct{}{}
	}
	return m
}

func TestRegistryRuleSetCache_GetOrBuild_ReturnsFromCacheFlag(t *testing.T) {
	c := NewRegistryRuleSetCache()

	rules := []api.OCIRegistry{
		{
			Registry:   "harbor/.*",
			Validation: []api.RegistryValidationTarget{api.ValidateImages, api.ValidateVolumes},
			Policy:     []corev1.PullPolicy{corev1.PullNever},
		},
	}

	rs1, fromCache1, err := c.GetOrBuild(rules)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if rs1 == nil {
		t.Fatalf("expected ruleset, got nil")
	}
	if fromCache1 {
		t.Fatalf("expected fromCache=false on first build, got true")
	}

	rs2, fromCache2, err := c.GetOrBuild(rules)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if rs2 == nil {
		t.Fatalf("expected ruleset, got nil")
	}
	if !fromCache2 {
		t.Fatalf("expected fromCache=true on second call, got false")
	}

	if rs1 != rs2 {
		t.Fatalf("expected same cached pointer, got rs1=%p rs2=%p", rs1, rs2)
	}
}

func TestRuleSetCache_GetOrBuild_EmptyReturnsNil(t *testing.T) {
	c := NewRegistryRuleSetCache()

	rs, _, err := c.GetOrBuild(nil)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if rs != nil {
		t.Fatalf("expected nil ruleset, got %#v", rs)
	}

	rs, _, err = c.GetOrBuild([]api.OCIRegistry{})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if rs != nil {
		t.Fatalf("expected nil ruleset, got %#v", rs)
	}

	if got := c.Stats(); got != 0 {
		t.Fatalf("expected Stats()=0, got %d", got)
	}
}

func TestRuleSetCache_GetOrBuild_InvalidRegexReturnsError(t *testing.T) {
	c := NewRegistryRuleSetCache()

	// invalid regex
	rules := []api.OCIRegistry{
		{
			Registry:   "([",
			Validation: []api.RegistryValidationTarget{api.ValidateImages},
			Policy:     []corev1.PullPolicy{corev1.PullAlways},
		},
	}

	rs, _, err := c.GetOrBuild(rules)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if rs != nil {
		t.Fatalf("expected nil ruleset on error, got %#v", rs)
	}

	if got := c.Stats(); got != 0 {
		t.Fatalf("expected Stats()=0 after failing build, got %d", got)
	}
}

func TestRuleSetCache_GetOrBuild_DeduplicatesByContent(t *testing.T) {
	c := NewRegistryRuleSetCache()

	rulesA := []api.OCIRegistry{
		{
			Registry:   "harbor/.*",
			Validation: []api.RegistryValidationTarget{api.ValidateImages, api.ValidateVolumes},
			Policy:     []corev1.PullPolicy{corev1.PullNever},
		},
	}

	// same content but different backing slice
	rulesB := []api.OCIRegistry{
		{
			Registry:   "harbor/.*",
			Validation: []api.RegistryValidationTarget{api.ValidateImages, api.ValidateVolumes},
			Policy:     []corev1.PullPolicy{corev1.PullNever},
		},
	}

	rs1, _, err := c.GetOrBuild(rulesA)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	rs2, _, err := c.GetOrBuild(rulesB)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	// the whole point: should be the exact same pointer
	if rs1 != rs2 {
		t.Fatalf("expected same cached pointer, got rs1=%p rs2=%p", rs1, rs2)
	}

	if got := c.Stats(); got != 1 {
		t.Fatalf("expected Stats()=1, got %d", got)
	}

	// sanity: compiled fields are correct (no DeepEqual; check specific invariants)
	if rs1.ID == "" {
		t.Fatalf("expected non-empty ruleset ID")
	}
	if len(rs1.Compiled) != 1 {
		t.Fatalf("expected 1 compiled rule, got %d", len(rs1.Compiled))
	}
	cr := rs1.Compiled[0]
	if cr.RE == nil {
		t.Fatalf("expected compiled regexp, got nil")
	}
	if cr.Registry != "harbor/.*" {
		t.Fatalf("expected Registry to match input, got %q", cr.Registry)
	}
	if !cr.ValidateImages || !cr.ValidateVolumes {
		t.Fatalf("expected ValidateImages and ValidateVolumes true, got images=%v volumes=%v", cr.ValidateImages, cr.ValidateVolumes)
	}
	if rs1.HasImages != true || rs1.HasVolumes != true {
		t.Fatalf("expected ruleset flags HasImages/HasVolumes true, got images=%v volumes=%v", rs1.HasImages, rs1.HasVolumes)
	}
	if cr.AllowedPolicy == nil {
		t.Fatalf("expected AllowedPolicy map non-nil")
	}
	if _, ok := cr.AllowedPolicy[corev1.PullNever]; !ok {
		t.Fatalf("expected AllowedPolicy to contain PullNever")
	}
}

func TestRuleSetCache_GetOrBuild_OrderMatters_LaterWins(t *testing.T) {
	c := NewRegistryRuleSetCache()

	// Two rules with same items but swapped order
	// hashRules preserves rule order, so the IDs must differ.
	rules1 := []api.OCIRegistry{
		{Registry: ".*", Validation: []api.RegistryValidationTarget{api.ValidateImages}, Policy: []corev1.PullPolicy{corev1.PullAlways}},
		{Registry: "harbor/.*", Validation: []api.RegistryValidationTarget{api.ValidateImages}},
	}
	rules2 := []api.OCIRegistry{
		{Registry: "harbor/.*", Validation: []api.RegistryValidationTarget{api.ValidateImages}},
		{Registry: ".*", Validation: []api.RegistryValidationTarget{api.ValidateImages}, Policy: []corev1.PullPolicy{corev1.PullAlways}},
	}

	rs1, _, err := c.GetOrBuild(rules1)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	rs2, _, err := c.GetOrBuild(rules2)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	if rs1 == rs2 {
		t.Fatalf("expected different cached entries due to different rule order, got same pointer %p", rs1)
	}
	if rs1.ID == rs2.ID {
		t.Fatalf("expected different IDs for different order, got same %q", rs1.ID)
	}
	if got := c.Stats(); got != 2 {
		t.Fatalf("expected Stats()=2, got %d", got)
	}

	// Verify compiled slice preserves the rule order we provided
	if len(rs1.Compiled) != 2 {
		t.Fatalf("expected 2 compiled rules, got %d", len(rs1.Compiled))
	}
	if rs1.Compiled[0].Registry != ".*" || rs1.Compiled[1].Registry != "harbor/.*" {
		t.Fatalf("expected compiled order to match input for rules1, got %q then %q",
			rs1.Compiled[0].Registry, rs1.Compiled[1].Registry)
	}
}

func TestRuleSetCache_GetOrBuild_ConcurrentReturnsSamePointer(t *testing.T) {
	c := NewRegistryRuleSetCache()

	rules := []api.OCIRegistry{
		{
			Registry:   "harbor/.*",
			Validation: []api.RegistryValidationTarget{api.ValidateImages, api.ValidateVolumes},
			Policy:     []corev1.PullPolicy{corev1.PullAlways, corev1.PullIfNotPresent},
		},
	}

	const workers = 32
	var wg sync.WaitGroup
	wg.Add(workers)

	results := make([]*RuleSet, workers)
	errs := make([]error, workers)

	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			rs, _, err := c.GetOrBuild(rules)
			results[i] = rs
			errs[i] = err
		}(i)
	}

	wg.Wait()

	for i := 0; i < workers; i++ {
		if errs[i] != nil {
			t.Fatalf("worker %d got err: %v", i, errs[i])
		}
		if results[i] == nil {
			t.Fatalf("worker %d got nil ruleset", i)
		}
	}

	// all pointers must match the first
	first := results[0]
	for i := 1; i < workers; i++ {
		if results[i] != first {
			t.Fatalf("expected same cached pointer across goroutines; got %p vs %p", first, results[i])
		}
	}
}

func TestRegistryRuleSetCache_GetOrBuild_ConcurrentPointersAndFlags(t *testing.T) {
	c := NewRegistryRuleSetCache()

	rules := []api.OCIRegistry{
		{Registry: "harbor/.*", Validation: []api.RegistryValidationTarget{api.ValidateImages}},
	}

	const workers = 32
	var wg sync.WaitGroup
	wg.Add(workers)

	results := make([]*RuleSet, workers)
	flags := make([]bool, workers)
	errs := make([]error, workers)

	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			rs, fromCache, err := c.GetOrBuild(rules)
			results[i] = rs
			flags[i] = fromCache
			errs[i] = err
		}(i)
	}
	wg.Wait()

	for i := 0; i < workers; i++ {
		if errs[i] != nil {
			t.Fatalf("worker %d err: %v", i, errs[i])
		}
		if results[i] == nil {
			t.Fatalf("worker %d got nil ruleset", i)
		}
	}

	first := results[0]
	for i := 1; i < workers; i++ {
		if results[i] != first {
			t.Fatalf("expected same cached pointer across goroutines; got %p vs %p", first, results[i])
		}
	}

	seenFalse := false
	seenTrue := false
	for i := 0; i < workers; i++ {
		if flags[i] {
			seenTrue = true
		} else {
			seenFalse = true
		}
	}

	if !seenFalse {
		t.Fatalf("expected at least one fromCache=false (builder), got none")
	}

	if !seenTrue {
		t.Fatalf("expected at least one fromCache=true (builder), got none")
	}
}

func TestRegistryRuleSetCache_InsertForTest_ThenHasAndLen(t *testing.T) {
	c := NewRegistryRuleSetCache()

	if got := c.Stats(); got != 0 {
		t.Fatalf("expected Len()=0, got %d", got)
	}
	if c.Has("x") {
		t.Fatalf("expected Has(x)=false on empty cache")
	}

	c.insertForTest("x")

	if !c.Has("x") {
		t.Fatalf("expected Has(x)=true after insert")
	}
	if got := c.Stats(); got != 1 {
		t.Fatalf("expected Len()=1 after insert, got %d", got)
	}
}

func TestRegistryRuleSetCache_InsertForTest_DuplicateDoesNotIncreaseLen(t *testing.T) {
	c := NewRegistryRuleSetCache()

	c.insertForTest("x")
	if got := c.Stats(); got != 1 {
		t.Fatalf("expected Len()=1 after first insert, got %d", got)
	}

	c.insertForTest("x")
	if got := c.Stats(); got != 1 {
		t.Fatalf("expected Len() to remain 1 after duplicate insert, got %d", got)
	}

	if !c.Has("x") {
		t.Fatalf("expected Has(x)=true after duplicate insert")
	}
}

func TestRegistryRuleSetCache_HasFalseForMissingKey(t *testing.T) {
	c := NewRegistryRuleSetCache()

	c.insertForTest("a")
	if c.Has("b") {
		t.Fatalf("expected Has(b)=false when only a exists")
	}
}

func TestRegistryRuleSetCache_PruneActive_RemovesOnlyInactive(t *testing.T) {
	c := NewRegistryRuleSetCache()
	c.insertForTest("a")
	c.insertForTest("b")
	c.insertForTest("c")

	removed := c.PruneActive(set("b"))

	if removed != 2 {
		t.Fatalf("expected removed=2, got %d", removed)
	}
	if got := c.Stats(); got != 1 {
		t.Fatalf("expected Len()=1 after prune, got %d", got)
	}

	if !c.Has("b") {
		t.Fatalf("expected b to remain")
	}
	if c.Has("a") || c.Has("c") {
		t.Fatalf("expected a and c to be removed")
	}
}

func TestRegistryRuleSetCache_PruneActive_AllActiveNoChange(t *testing.T) {
	c := NewRegistryRuleSetCache()
	c.insertForTest("a")
	c.insertForTest("b")

	removed := c.PruneActive(set("a", "b"))

	if removed != 0 {
		t.Fatalf("expected removed=0, got %d", removed)
	}
	if got := c.Stats(); got != 2 {
		t.Fatalf("expected Len()=2, got %d", got)
	}
	if !c.Has("a") || !c.Has("b") {
		t.Fatalf("expected both a and b to remain")
	}
}

func TestRegistryRuleSetCache_PruneActive_EmptyActivePrunesAll(t *testing.T) {
	c := NewRegistryRuleSetCache()
	c.insertForTest("a")
	c.insertForTest("b")

	removed := c.PruneActive(set())

	if removed != 2 {
		t.Fatalf("expected removed=2, got %d", removed)
	}
	if got := c.Stats(); got != 0 {
		t.Fatalf("expected Len()=0 after prune all, got %d", got)
	}
	if c.Has("a") || c.Has("b") {
		t.Fatalf("expected cache to be empty after prune all")
	}
}

func TestRegistryRuleSetCache_PruneActive_NilActivePrunesAll(t *testing.T) {
	c := NewRegistryRuleSetCache()
	c.insertForTest("a")

	removed := c.PruneActive(nil)

	if removed != 1 {
		t.Fatalf("expected removed=1, got %d", removed)
	}
	if got := c.Stats(); got != 0 {
		t.Fatalf("expected Len()=0 after prune, got %d", got)
	}
	if c.Has("a") {
		t.Fatalf("expected a to be removed")
	}
}

func TestRegistryRuleSetCache_PruneActive_EmptyCacheNoop(t *testing.T) {
	c := NewRegistryRuleSetCache()

	removed := c.PruneActive(set("a"))

	if removed != 0 {
		t.Fatalf("expected removed=0 on empty cache, got %d", removed)
	}
	if got := c.Stats(); got != 0 {
		t.Fatalf("expected Len()=0, got %d", got)
	}
}

func TestRegistryRuleSetCache_PruneActive_Idempotent(t *testing.T) {
	c := NewRegistryRuleSetCache()
	c.insertForTest("a")
	c.insertForTest("b")
	c.insertForTest("c")

	active := set("a")

	removed1 := c.PruneActive(active)
	if removed1 != 2 {
		t.Fatalf("expected first prune removed=2, got %d", removed1)
	}
	if got := c.Stats(); got != 1 {
		t.Fatalf("expected Len()=1 after first prune, got %d", got)
	}
	if !c.Has("a") {
		t.Fatalf("expected a to remain after first prune")
	}

	removed2 := c.PruneActive(active)
	if removed2 != 0 {
		t.Fatalf("expected second prune removed=0, got %d", removed2)
	}
	if got := c.Stats(); got != 1 {
		t.Fatalf("expected Len()=1 after second prune, got %d", got)
	}
}

func TestRegistryRuleSetCache_PruneActive_RemovesCorrectCountWithLargerSet(t *testing.T) {
	c := NewRegistryRuleSetCache()

	// Insert 10 IDs: id0..id9
	for i := 0; i < 10; i++ {
		c.insertForTest("id" + itoa(i))
	}

	// Keep 3: id0,id4,id9
	removed := c.PruneActive(set("id0", "id4", "id9"))

	if removed != 7 {
		t.Fatalf("expected removed=7, got %d", removed)
	}
	if got := c.Stats(); got != 3 {
		t.Fatalf("expected Len()=3, got %d", got)
	}
	if !c.Has("id0") || !c.Has("id4") || !c.Has("id9") {
		t.Fatalf("expected id0,id4,id9 to remain")
	}
}

// tiny int->string without fmt (faster, no allocations beyond result)
func itoa(i int) string {
	// Enough for small test numbers
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	n := len(buf)
	for i > 0 {
		n--
		buf[n] = byte('0' + (i % 10))
		i /= 10
	}
	return string(buf[n:])
}
