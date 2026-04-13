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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	taskrunv1alpha1 "github.com/davidkenelm/taskrun/api/v1alpha1"
)

func testTaskRun(name string) *taskrunv1alpha1.TaskRun {
	return &taskrunv1alpha1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			UID:       "test-uid-123",
		},
		Spec: taskrunv1alpha1.TaskRunSpec{
			Steps: []taskrunv1alpha1.StepSpec{
				{Name: "fetch", Action: "http-request", Params: map[string]string{"url": "https://example.com", "method": "GET"}},
			},
		},
	}
}

func testRunnerSteps() []ResolvedStep {
	return []ResolvedStep{
		{
			Step: taskrunv1alpha1.StepSpec{
				Name:   "fetch",
				Action: "http-request",
				Params: map[string]string{"url": "https://example.com", "method": "GET"},
			},
			Definition: taskrunv1alpha1.StepDefinitionSpec{
				Schema: &runtime.RawExtension{Raw: []byte(`{"type":"object"}`)},
				Runner: &taskrunv1alpha1.RunnerSpec{Image: "ghcr.io/davidkenelm/taskrun-runners/http:0.1.0"},
			},
		},
	}
}

func TestJobBuilder_Build_BasicJob(t *testing.T) {
	scheme := newTestScheme()
	builder := NewJobBuilder(scheme)
	tr := testTaskRun("my-task")

	job, err := builder.Build(tr, testRunnerSteps())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if job.Name != "my-task-runner" {
		t.Errorf("expected job name 'my-task-runner', got %q", job.Name)
	}
	if job.Namespace != "default" {
		t.Errorf("expected namespace 'default', got %q", job.Namespace)
	}

	// Should have 1 init container (the runner step) and 1 main container (pause).
	pod := job.Spec.Template.Spec
	if len(pod.InitContainers) != 1 {
		t.Fatalf("expected 1 init container, got %d", len(pod.InitContainers))
	}
	if len(pod.Containers) != 1 {
		t.Fatalf("expected 1 main container, got %d", len(pod.Containers))
	}

	init := pod.InitContainers[0]
	if init.Name != "step-0-fetch" {
		t.Errorf("expected init container name 'step-0-fetch', got %q", init.Name)
	}
	if init.Image != "ghcr.io/davidkenelm/taskrun-runners/http:0.1.0" {
		t.Errorf("unexpected image: %q", init.Image)
	}
}

func TestJobBuilder_Build_WithAuth(t *testing.T) {
	scheme := newTestScheme()
	builder := NewJobBuilder(scheme)
	tr := testTaskRun("auth-task")
	tr.Spec.Auth = &taskrunv1alpha1.AuthSpec{
		Type:          taskrunv1alpha1.AuthTypeOIDC,
		TokenEndpoint: "https://auth.example.com/token",
		CredentialsFrom: &taskrunv1alpha1.CredentialsSource{
			SecretRef: taskrunv1alpha1.SecretKeySelector{Name: "my-secret"},
		},
	}

	job, err := builder.Build(tr, testRunnerSteps())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pod := job.Spec.Template.Spec
	// Auth init + 1 runner step = 2 init containers.
	if len(pod.InitContainers) != 2 {
		t.Fatalf("expected 2 init containers (auth + step), got %d", len(pod.InitContainers))
	}
	if pod.InitContainers[0].Name != "auth" {
		t.Errorf("expected first init container to be 'auth', got %q", pod.InitContainers[0].Name)
	}
}

func TestJobBuilder_Build_NoRunnerSteps(t *testing.T) {
	scheme := newTestScheme()
	builder := NewJobBuilder(scheme)
	tr := testTaskRun("no-runner")

	job, err := builder.Build(tr, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job != nil {
		t.Error("expected nil job when no runner steps")
	}
}

func TestJobBuilder_Build_SharedVolume(t *testing.T) {
	scheme := newTestScheme()
	builder := NewJobBuilder(scheme)
	tr := testTaskRun("vol-task")

	job, err := builder.Build(tr, testRunnerSteps())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pod := job.Spec.Template.Spec
	if len(pod.Volumes) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(pod.Volumes))
	}
	if pod.Volumes[0].Name != "step-data" {
		t.Errorf("expected volume name 'step-data', got %q", pod.Volumes[0].Name)
	}
	if pod.Volumes[0].EmptyDir == nil {
		t.Error("expected emptyDir volume source")
	}

	// Init container should mount the volume.
	if len(pod.InitContainers[0].VolumeMounts) != 1 {
		t.Fatalf("expected 1 volume mount, got %d", len(pod.InitContainers[0].VolumeMounts))
	}
	if pod.InitContainers[0].VolumeMounts[0].MountPath != "/etc/step" {
		t.Errorf("expected mount path '/etc/step', got %q", pod.InitContainers[0].VolumeMounts[0].MountPath)
	}
}

func TestJobBuilder_Build_Labels(t *testing.T) {
	scheme := newTestScheme()
	builder := NewJobBuilder(scheme)
	tr := testTaskRun("labeled")

	job, err := builder.Build(tr, testRunnerSteps())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if job.Labels["taskrun.io/taskrun"] != "labeled" {
		t.Errorf("missing or wrong taskrun label: %v", job.Labels)
	}
	if job.Labels["app.kubernetes.io/managed-by"] != "taskrun-controller" {
		t.Errorf("missing managed-by label: %v", job.Labels)
	}
}

func TestJobBuilder_Build_OwnerRef(t *testing.T) {
	scheme := newTestScheme()
	builder := NewJobBuilder(scheme)
	tr := testTaskRun("owned")

	job, err := builder.Build(tr, testRunnerSteps())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(job.OwnerReferences) != 1 {
		t.Fatalf("expected 1 owner reference, got %d", len(job.OwnerReferences))
	}
	if job.OwnerReferences[0].Name != "owned" {
		t.Errorf("expected owner ref name 'owned', got %q", job.OwnerReferences[0].Name)
	}
}

func TestJobBuilder_BuildCronJob(t *testing.T) {
	scheme := newTestScheme()
	builder := NewJobBuilder(scheme)
	tr := testTaskRun("scheduled")
	tr.Spec.Schedule = "*/5 * * * *"
	tr.Spec.ConcurrencyPolicy = taskrunv1alpha1.ConcurrencyPolicyForbid

	cronJob, err := builder.BuildCronJob(tr, testRunnerSteps())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cronJob.Spec.Schedule != "*/5 * * * *" {
		t.Errorf("expected schedule '*/5 * * * *', got %q", cronJob.Spec.Schedule)
	}
	if cronJob.Spec.ConcurrencyPolicy != "Forbid" {
		t.Errorf("expected Forbid concurrency, got %q", cronJob.Spec.ConcurrencyPolicy)
	}
}

func TestJobBuilder_MultipleRunnerSteps(t *testing.T) {
	scheme := newTestScheme()
	builder := NewJobBuilder(scheme)
	tr := testTaskRun("multi")

	steps := []ResolvedStep{
		{
			Step:       taskrunv1alpha1.StepSpec{Name: "step1", Action: "http-request"},
			Definition: taskrunv1alpha1.StepDefinitionSpec{Runner: &taskrunv1alpha1.RunnerSpec{Image: "img1:v1"}},
		},
		{
			Step:       taskrunv1alpha1.StepSpec{Name: "step2", Action: "db-query"},
			Definition: taskrunv1alpha1.StepDefinitionSpec{Runner: &taskrunv1alpha1.RunnerSpec{Image: "img2:v1"}},
		},
	}

	job, err := builder.Build(tr, steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pod := job.Spec.Template.Spec
	if len(pod.InitContainers) != 2 {
		t.Fatalf("expected 2 init containers, got %d", len(pod.InitContainers))
	}
	if pod.InitContainers[0].Name != "step-0-step1" {
		t.Errorf("unexpected first init container name: %q", pod.InitContainers[0].Name)
	}
	if pod.InitContainers[1].Name != "step-1-step2" {
		t.Errorf("unexpected second init container name: %q", pod.InitContainers[1].Name)
	}
}
