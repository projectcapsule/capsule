// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"fmt"
	"sort"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

// These helpers read manager-cache snapshots without copying large Tenant status
// trees. Namespace ownership is resolved by the shared UID-checking helper; the
// borrowed objects remain read-only and live only for this planning pass.
type watchPlanningReader struct{ client.Reader }

func (r watchPlanningReader) Get(ctx context.Context, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
	return r.Reader.Get(ctx, key, object, append(options, client.UnsafeDisableDeepCopy)...)
}

type watchSourcePlan struct {
	order    []string
	selected []client.Object
	owners   map[types.UID]string
}

func (r *ResourceWatches) planSources(ctx context.Context, sources []client.Object) (*watchSourcePlan, int, error) {
	type ownership struct {
		group string
		err   error
	}

	namespaces := make(map[string]ownership)
	groups := make(map[string][]client.Object)
	plan := &watchSourcePlan{owners: make(map[types.UID]string, len(sources))}
	omitted := 0

	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return nil, omitted, err
		}

		group := "system"

		if namespace := source.GetNamespace(); namespace != "" {
			owner, found := namespaces[namespace]
			if !found {
				owner.group, owner.err = r.namespaceWatchGroup(ctx, namespace)

				namespaces[namespace] = owner

				if owner.err != nil {
					r.log.Error(owner.err, "cannot resolve resource watch ownership", "namespace", namespace)
				}
			}

			if owner.err != nil {
				omitted++

				continue
			}

			group = owner.group
		}

		groups[group] = append(groups[group], source)
		plan.owners[source.GetUID()] = group
	}

	plan.order = r.watchGroupOrder(groups)
	active := append([]string(nil), plan.order...)
	positions := make(map[string]int, len(active))
	conditions := 0

	for len(active) > 0 {
		next := active[:0]

		for _, group := range active {
			index := positions[group]
			source := groups[group][index]

			positions[group]++

			if index+1 < len(groups[group]) {
				next = append(next, group)
			}

			count := 0
			for _, trigger := range triggerSpec(source).Triggers {
				count += len(trigger.MatchConditions)
			}

			if len(plan.selected) >= cache.MaxPreparedReplications || conditions+count > cache.MaxPreparedTriggerConditions {
				omitted++

				continue
			}

			conditions += count

			plan.selected = append(plan.selected, source)
		}

		active = next
	}

	return plan, omitted, nil
}

func (r *ResourceWatches) namespaceWatchGroup(ctx context.Context, namespace string) (string, error) {
	object, snapshot, err := tenant.GetTenantByNamespaceWithSnapshot(ctx, watchPlanningReader{r.client}, namespace)
	if err != nil {
		return "", err
	}

	if snapshot == nil {
		return "", fmt.Errorf("source namespace %q is unavailable", namespace)
	}

	if object == nil {
		if _, referenced := tenant.GetTenantNameByOwnerreferences(snapshot.OwnerReferences); referenced {
			return "", fmt.Errorf("source namespace %q references an unavailable Tenant", namespace)
		}

		return "system", nil
	}

	if object.UID == "" {
		return "", fmt.Errorf("source namespace %q has no persisted Tenant identity", namespace)
	}

	return "tenant/" + string(object.UID), nil
}

func (r *ResourceWatches) watchGroupOrder(groups map[string][]client.Object) []string {
	clock := r.now
	if clock == nil {
		clock = time.Now
	}

	now := clock()

	r.mu.Lock()
	if r.rotationDue.IsZero() {
		r.rotationDue = now.Add(30 * time.Second)
	} else if !now.Before(r.rotationDue) {
		r.priorityGroup = r.nextPriorityGroup
		r.rotationDue = now.Add(30 * time.Second)
	}

	priority := r.priorityGroup
	r.mu.Unlock()

	order := make([]string, 0, len(groups))
	for group := range groups {
		order = append(order, group)
	}

	sort.Strings(order)

	start := sort.SearchStrings(order, priority)
	if start == len(order) {
		start = 0
	}

	return append(order[start:], order[:start]...)
}

func (r *ResourceWatches) nextWatchGroup(order []string, opportunity map[string]bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, group := range order {
		if !opportunity[group] {
			r.nextPriorityGroup = group

			return
		}
	}

	r.nextPriorityGroup = r.priorityGroup
}

// Keep only a group's first possible stream-budget worth of keys. This bounds
// temporary planning state even when many sources request disjoint resources.
type watchKeySet struct {
	keys     map[resourceWatchKey]struct{}
	last     resourceWatchKey
	overflow bool
}

func (s *watchKeySet) add(key resourceWatchKey) {
	if s.keys == nil {
		s.keys = make(map[resourceWatchKey]struct{})
	}

	if _, exists := s.keys[key]; exists {
		return
	}

	if len(s.keys) < maxResourceWatches {
		s.keys[key] = struct{}{}
		if len(s.keys) == 1 || watchKeyLess(s.last, key) {
			s.last = key
		}

		return
	}

	s.overflow = true

	if !watchKeyLess(key, s.last) {
		return
	}

	delete(s.keys, s.last)
	s.keys[key] = struct{}{}

	s.last = key

	for candidate := range s.keys {
		if watchKeyLess(s.last, candidate) {
			s.last = candidate
		}
	}
}

func watchKeyLess(a, b resourceWatchKey) bool {
	for _, pair := range [][2]string{{a.resource.Group, b.resource.Group}, {a.resource.Version, b.resource.Version}, {a.resource.Resource, b.resource.Resource}, {a.identity.account.Namespace, b.identity.account.Namespace}, {a.identity.account.Name, b.identity.account.Name}, {a.namespace, b.namespace}} {
		if pair[0] != pair[1] {
			return pair[0] < pair[1]
		}
	}

	return false
}

func (s *watchKeySet) ordered() []resourceWatchKey {
	keys := make([]resourceWatchKey, 0, len(s.keys))
	for key := range s.keys {
		keys = append(keys, key)
	}

	sort.Slice(keys, func(i, j int) bool { return watchKeyLess(keys[i], keys[j]) })

	return keys
}

func planWatchKeys(order []string, groups map[string]*watchKeySet, opportunity map[string]bool) (map[resourceWatchKey]struct{}, int) {
	global := make(map[resourceWatchKey]struct{})

	for _, group := range groups {
		for key := range group.keys {
			if key.namespace == "" && key.identity.account == (cache.Key{}) {
				global[key] = struct{}{}
			}
		}
	}

	queues := make(map[string][]resourceWatchKey, len(groups))

	active := make([]string, 0, len(groups))

	for _, owner := range order {
		if group := groups[owner]; group != nil && len(group.keys) > 0 {
			queues[owner] = group.ordered()

			active = append(active, owner)
		}
	}

	selected := make(map[resourceWatchKey]struct{})
	omitted := 0

	for len(active) > 0 {
		next := active[:0]

		for _, group := range active {
			key := queues[group][0]

			queues[group] = queues[group][1:]
			if len(queues[group]) > 0 {
				next = append(next, group)
			}

			if key.identity.account == (cache.Key{}) {
				candidate := key

				candidate.namespace = ""

				if _, shared := global[candidate]; shared {
					key = candidate
				}
			}

			if _, shared := selected[key]; shared || len(selected) < maxResourceWatches {
				selected[key] = struct{}{}
				opportunity[group] = true
			} else {
				omitted++
			}
		}

		active = next
	}

	for _, group := range groups {
		if group.overflow {
			omitted++
		}
	}

	return selected, omitted
}

func (r *ResourceWatches) discoverWatchResource(resource schema.GroupVersionResource) (schema.GroupVersionKind, error) {
	kind, err := r.mapper.KindFor(resource)
	if err != nil {
		return kind, err
	}

	mapping, err := r.mapper.RESTMapping(kind.GroupKind(), kind.Version)
	if err != nil {
		return kind, err
	}

	if mapping.Resource != resource {
		return kind, fmt.Errorf("watch requires the canonical plural resource name")
	}

	if mapping.Scope.Name() != apimeta.RESTScopeNameNamespace {
		return kind, fmt.Errorf("watch requires a namespaced resource")
	}

	return kind, nil
}
