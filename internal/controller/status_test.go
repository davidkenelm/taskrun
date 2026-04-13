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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	taskrunv1alpha1 "github.com/davidkenelm/taskrun/api/v1alpha1"
)

func TestStatusManager_InitStepStatuses(t *testing.T) {
	tr := &taskrunv1alpha1.TaskRun{}
	mgr := &StatusManager{}

	steps := []ResolvedStep{
		{Step: taskrunv1alpha1.StepSpec{Name: "step1"}},
		{Step: taskrunv1alpha1.StepSpec{Name: "step2"}},
		{Step: taskrunv1alpha1.StepSpec{Name: "step3"}},
	}

	mgr.InitStepStatuses(tr, steps)

	if len(tr.Status.Steps) != 3 {
		t.Fatalf("expected 3 step statuses, got %d", len(tr.Status.Steps))
	}
	for _, s := range tr.Status.Steps {
		if s.Phase != taskrunv1alpha1.StepPhasePending {
			t.Errorf("step %q should be Pending, got %q", s.Name, s.Phase)
		}
	}
}

func TestStatusManager_SetStepStatus_Update(t *testing.T) {
	tr := &taskrunv1alpha1.TaskRun{
		Status: taskrunv1alpha1.TaskRunStatus{
			Steps: []taskrunv1alpha1.StepStatus{
				{Name: "step1", Phase: taskrunv1alpha1.StepPhasePending},
				{Name: "step2", Phase: taskrunv1alpha1.StepPhasePending},
			},
		},
	}
	mgr := &StatusManager{}

	mgr.SetStepStatus(tr, taskrunv1alpha1.StepStatus{
		Name:  "step1",
		Phase: taskrunv1alpha1.StepPhaseRunning,
	})

	if tr.Status.Steps[0].Phase != taskrunv1alpha1.StepPhaseRunning {
		t.Errorf("expected Running, got %q", tr.Status.Steps[0].Phase)
	}
	if tr.Status.Steps[1].Phase != taskrunv1alpha1.StepPhasePending {
		t.Errorf("step2 should be unchanged, got %q", tr.Status.Steps[1].Phase)
	}
}

func TestStatusManager_SetStepStatus_Append(t *testing.T) {
	tr := &taskrunv1alpha1.TaskRun{}
	mgr := &StatusManager{}

	mgr.SetStepStatus(tr, taskrunv1alpha1.StepStatus{
		Name:  "new-step",
		Phase: taskrunv1alpha1.StepPhaseSucceeded,
	})

	if len(tr.Status.Steps) != 1 {
		t.Fatalf("expected 1 step status, got %d", len(tr.Status.Steps))
	}
	if tr.Status.Steps[0].Name != "new-step" {
		t.Errorf("unexpected step name: %q", tr.Status.Steps[0].Name)
	}
}

func TestStatusManager_MarkStepSucceeded(t *testing.T) {
	tr := &taskrunv1alpha1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
		Status: taskrunv1alpha1.TaskRunStatus{
			Steps: []taskrunv1alpha1.StepStatus{
				{Name: "fetch", Phase: taskrunv1alpha1.StepPhaseRunning},
			},
		},
	}
	mgr := &StatusManager{}

	outputs := map[string]string{"body": "hello"}
	mgr.MarkStepSucceeded(tr, "fetch", 2*time.Second, outputs, "http-request")

	step := tr.Status.Steps[0]
	if step.Phase != taskrunv1alpha1.StepPhaseSucceeded {
		t.Errorf("expected Succeeded, got %q", step.Phase)
	}
	if step.Outputs["body"] != "hello" {
		t.Errorf("expected output 'hello', got %q", step.Outputs["body"])
	}
	if step.Duration == "" {
		t.Error("duration should be set")
	}
}

func TestStatusManager_MarkStepFailed(t *testing.T) {
	tr := &taskrunv1alpha1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
		Status: taskrunv1alpha1.TaskRunStatus{
			Steps: []taskrunv1alpha1.StepStatus{
				{Name: "fetch", Phase: taskrunv1alpha1.StepPhaseRunning},
			},
		},
	}
	mgr := &StatusManager{}

	mgr.MarkStepFailed(tr, "fetch", 500*time.Millisecond, "connection refused", "http-request")

	step := tr.Status.Steps[0]
	if step.Phase != taskrunv1alpha1.StepPhaseFailed {
		t.Errorf("expected Failed, got %q", step.Phase)
	}
	if step.Logs != "connection refused" {
		t.Errorf("expected error in logs, got %q", step.Logs)
	}
}

func TestStatusManager_SetCondition(t *testing.T) {
	tr := &taskrunv1alpha1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{Generation: 1},
	}
	mgr := &StatusManager{}

	mgr.SetCondition(tr, "Complete", metav1.ConditionTrue, "AllDone", "all steps done")

	if len(tr.Status.Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(tr.Status.Conditions))
	}
	cond := tr.Status.Conditions[0]
	if cond.Type != "Complete" {
		t.Errorf("expected type 'Complete', got %q", cond.Type)
	}
	if cond.Status != metav1.ConditionTrue {
		t.Errorf("expected True, got %q", cond.Status)
	}
	if cond.Reason != "AllDone" {
		t.Errorf("expected reason 'AllDone', got %q", cond.Reason)
	}
	if cond.ObservedGeneration != 1 {
		t.Errorf("expected generation 1, got %d", cond.ObservedGeneration)
	}
}

func TestStatusManager_SetCondition_OverwriteExisting(t *testing.T) {
	tr := &taskrunv1alpha1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{Generation: 2},
		Status: taskrunv1alpha1.TaskRunStatus{
			Conditions: []metav1.Condition{
				{
					Type:   "Complete",
					Status: metav1.ConditionFalse,
					Reason: "InProgress",
				},
			},
		},
	}
	mgr := &StatusManager{}

	mgr.SetCondition(tr, "Complete", metav1.ConditionTrue, "Done", "finished")

	if len(tr.Status.Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(tr.Status.Conditions))
	}
	if tr.Status.Conditions[0].Status != metav1.ConditionTrue {
		t.Errorf("expected True after overwrite, got %q", tr.Status.Conditions[0].Status)
	}
}

func TestPartitionSteps(t *testing.T) {
	steps := []ResolvedStep{
		{
			Step:       taskrunv1alpha1.StepSpec{Name: "fetch"},
			Definition: taskrunv1alpha1.StepDefinitionSpec{Runner: &taskrunv1alpha1.RunnerSpec{Image: "img:v1"}},
		},
		{
			Step:       taskrunv1alpha1.StepSpec{Name: "store"},
			Definition: taskrunv1alpha1.StepDefinitionSpec{Schema: &runtime.RawExtension{Raw: []byte(`{}`)}},
		},
		{
			Step:       taskrunv1alpha1.StepSpec{Name: "query"},
			Definition: taskrunv1alpha1.StepDefinitionSpec{Runner: &taskrunv1alpha1.RunnerSpec{Image: "img:v2"}},
		},
	}

	p := PartitionSteps(steps)

	if len(p.Runner) != 2 {
		t.Errorf("expected 2 runner steps, got %d", len(p.Runner))
	}
	if len(p.APINative) != 1 {
		t.Errorf("expected 1 API-native step, got %d", len(p.APINative))
	}
	if len(p.Order) != 3 {
		t.Errorf("expected 3 ordered steps, got %d", len(p.Order))
	}
	if !p.Order[0].IsRunner || p.Order[1].IsRunner || !p.Order[2].IsRunner {
		t.Error("order flags are wrong")
	}
}
