/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	taskrunv1alpha1 "github.com/davidkenelm/taskrun/api/v1alpha1"
)

// ResolvedStep pairs a StepSpec with its resolved StepDefinitionSpec.
type ResolvedStep struct {
	Step       taskrunv1alpha1.StepSpec
	Definition taskrunv1alpha1.StepDefinitionSpec
}

// StepResolver resolves step actions to their StepDefinition or ClusterStepDefinition specs.
type StepResolver struct {
	client    client.Reader
	namespace string
	cache     map[string]taskrunv1alpha1.StepDefinitionSpec
}

// NewStepResolver creates a resolver scoped to a namespace.
func NewStepResolver(c client.Reader, namespace string) *StepResolver {
	return &StepResolver{
		client:    c,
		namespace: namespace,
		cache:     make(map[string]taskrunv1alpha1.StepDefinitionSpec),
	}
}

// ResolveAll resolves every step in the list, returning an error listing all unresolvable actions.
func (r *StepResolver) ResolveAll(ctx context.Context, steps []taskrunv1alpha1.StepSpec) ([]ResolvedStep, error) {
	resolved := make([]ResolvedStep, 0, len(steps))
	var unresolved []string

	for _, step := range steps {
		def, err := r.resolve(ctx, step.Action)
		if err != nil {
			unresolved = append(unresolved, step.Action)
			continue
		}
		resolved = append(resolved, ResolvedStep{Step: step, Definition: def})
	}

	if len(unresolved) > 0 {
		return nil, fmt.Errorf("unresolvable step actions: %v", unresolved)
	}
	return resolved, nil
}

// resolve looks up a single action: namespace-local StepDefinition first, then ClusterStepDefinition.
func (r *StepResolver) resolve(ctx context.Context, action string) (taskrunv1alpha1.StepDefinitionSpec, error) {
	if def, ok := r.cache[action]; ok {
		return def, nil
	}

	// Try namespace-scoped StepDefinition first.
	var sd taskrunv1alpha1.StepDefinition
	err := r.client.Get(ctx, types.NamespacedName{Name: action, Namespace: r.namespace}, &sd)
	if err == nil {
		r.cache[action] = sd.Spec
		return sd.Spec, nil
	}
	if client.IgnoreNotFound(err) != nil {
		return taskrunv1alpha1.StepDefinitionSpec{}, fmt.Errorf("looking up StepDefinition %s/%s: %w", r.namespace, action, err)
	}

	// Fall back to ClusterStepDefinition.
	var csd taskrunv1alpha1.ClusterStepDefinition
	err = r.client.Get(ctx, types.NamespacedName{Name: action}, &csd)
	if err == nil {
		r.cache[action] = csd.Spec
		return csd.Spec, nil
	}
	if client.IgnoreNotFound(err) != nil {
		return taskrunv1alpha1.StepDefinitionSpec{}, fmt.Errorf("looking up ClusterStepDefinition %s: %w", action, err)
	}
	return taskrunv1alpha1.StepDefinitionSpec{}, fmt.Errorf("step action %q not found as StepDefinition or ClusterStepDefinition", action)
}
