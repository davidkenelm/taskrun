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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	taskrunv1alpha1 "github.com/davidkenelm/taskrun/api/v1alpha1"
)

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = taskrunv1alpha1.AddToScheme(s)
	return s
}

func TestResolver_NamespacedFirst(t *testing.T) {
	schema := &runtime.RawExtension{Raw: []byte(`{"type":"object"}`)}

	nsDef := &taskrunv1alpha1.StepDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "http-request", Namespace: "default"},
		Spec: taskrunv1alpha1.StepDefinitionSpec{
			Schema: schema,
			Runner: &taskrunv1alpha1.RunnerSpec{Image: "ns-image:latest"},
		},
	}
	clusterDef := &taskrunv1alpha1.ClusterStepDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "http-request"},
		Spec: taskrunv1alpha1.StepDefinitionSpec{
			Schema: schema,
			Runner: &taskrunv1alpha1.RunnerSpec{Image: "cluster-image:latest"},
		},
	}

	c := fake.NewClientBuilder().WithScheme(newTestScheme()).
		WithObjects(nsDef, clusterDef).Build()

	resolver := NewStepResolver(c, "default")
	steps := []taskrunv1alpha1.StepSpec{
		{Name: "call-api", Action: "http-request"},
	}

	resolved, err := resolver.ResolveAll(context.Background(), steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved step, got %d", len(resolved))
	}
	if resolved[0].Definition.Runner.Image != "ns-image:latest" {
		t.Errorf("expected namespace-scoped image, got %q", resolved[0].Definition.Runner.Image)
	}
}

func TestResolver_FallbackToCluster(t *testing.T) {
	schema := &runtime.RawExtension{Raw: []byte(`{"type":"object"}`)}

	clusterDef := &taskrunv1alpha1.ClusterStepDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "http-request"},
		Spec: taskrunv1alpha1.StepDefinitionSpec{
			Schema: schema,
			Runner: &taskrunv1alpha1.RunnerSpec{Image: "cluster-image:latest"},
		},
	}

	c := fake.NewClientBuilder().WithScheme(newTestScheme()).
		WithObjects(clusterDef).Build()

	resolver := NewStepResolver(c, "default")
	steps := []taskrunv1alpha1.StepSpec{
		{Name: "call-api", Action: "http-request"},
	}

	resolved, err := resolver.ResolveAll(context.Background(), steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved[0].Definition.Runner.Image != "cluster-image:latest" {
		t.Errorf("expected cluster-scoped image, got %q", resolved[0].Definition.Runner.Image)
	}
}

func TestResolver_NotFound(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()

	resolver := NewStepResolver(c, "default")
	steps := []taskrunv1alpha1.StepSpec{
		{Name: "missing-step", Action: "nonexistent"},
	}

	_, err := resolver.ResolveAll(context.Background(), steps)
	if err == nil {
		t.Fatal("expected error for unresolvable action")
	}
}

func TestResolver_CachesResults(t *testing.T) {
	schema := &runtime.RawExtension{Raw: []byte(`{"type":"object"}`)}

	clusterDef := &taskrunv1alpha1.ClusterStepDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "http-request"},
		Spec: taskrunv1alpha1.StepDefinitionSpec{
			Schema: schema,
			Runner: &taskrunv1alpha1.RunnerSpec{Image: "image:v1"},
		},
	}

	c := fake.NewClientBuilder().WithScheme(newTestScheme()).
		WithObjects(clusterDef).Build()

	resolver := NewStepResolver(c, "default")
	steps := []taskrunv1alpha1.StepSpec{
		{Name: "step1", Action: "http-request"},
		{Name: "step2", Action: "http-request"},
	}

	resolved, err := resolver.ResolveAll(context.Background(), steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("expected 2 resolved steps, got %d", len(resolved))
	}
	// Both should resolve to the same definition.
	if resolved[0].Definition.Runner.Image != resolved[1].Definition.Runner.Image {
		t.Error("cached resolution returned different results")
	}
}

func TestResolver_MultipleActions(t *testing.T) {
	schema := &runtime.RawExtension{Raw: []byte(`{"type":"object"}`)}

	c := fake.NewClientBuilder().WithScheme(newTestScheme()).
		WithObjects(
			&taskrunv1alpha1.ClusterStepDefinition{
				ObjectMeta: metav1.ObjectMeta{Name: "http-request"},
				Spec:       taskrunv1alpha1.StepDefinitionSpec{Schema: schema, Runner: &taskrunv1alpha1.RunnerSpec{Image: "http:v1"}},
			},
			&taskrunv1alpha1.ClusterStepDefinition{
				ObjectMeta: metav1.ObjectMeta{Name: "secret-update"},
				Spec:       taskrunv1alpha1.StepDefinitionSpec{Schema: schema},
			},
		).Build()

	resolver := NewStepResolver(c, "default")
	steps := []taskrunv1alpha1.StepSpec{
		{Name: "fetch", Action: "http-request"},
		{Name: "store", Action: "secret-update"},
		{Name: "missing", Action: "nonexistent"},
	}

	_, err := resolver.ResolveAll(context.Background(), steps)
	if err == nil {
		t.Fatal("expected error when one action is unresolvable")
	}
}
