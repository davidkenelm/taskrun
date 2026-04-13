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
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	taskrunv1alpha1 "github.com/davidkenelm/taskrun/api/v1alpha1"
	"github.com/davidkenelm/taskrun/internal/metrics"
)

// StatusManager handles TaskRun status transitions, conditions, and metrics.
type StatusManager struct {
	client client.StatusClient
}

// NewStatusManager creates a new StatusManager.
func NewStatusManager(c client.StatusClient) *StatusManager {
	return &StatusManager{client: c}
}

// SetPhase transitions the TaskRun to a new phase, updating timestamps as appropriate.
func (m *StatusManager) SetPhase(ctx context.Context, taskRun *taskrunv1alpha1.TaskRun, phase taskrunv1alpha1.TaskRunPhase) error {
	now := metav1.Now()

	taskRun.Status.Phase = phase

	switch phase {
	case taskrunv1alpha1.TaskRunPhaseRunning:
		if taskRun.Status.StartTime == nil {
			taskRun.Status.StartTime = &now
		}
	case taskrunv1alpha1.TaskRunPhaseSucceeded, taskrunv1alpha1.TaskRunPhaseFailed:
		taskRun.Status.CompletionTime = &now
		m.recordTaskMetrics(taskRun)
	}

	return m.client.Status().Update(ctx, taskRun)
}

// SetStepStatus updates or appends the status for a specific step.
func (m *StatusManager) SetStepStatus(taskRun *taskrunv1alpha1.TaskRun, stepStatus taskrunv1alpha1.StepStatus) {
	for i, s := range taskRun.Status.Steps {
		if s.Name == stepStatus.Name {
			taskRun.Status.Steps[i] = stepStatus
			return
		}
	}
	taskRun.Status.Steps = append(taskRun.Status.Steps, stepStatus)
}

// InitStepStatuses creates Pending status entries for all steps.
func (m *StatusManager) InitStepStatuses(taskRun *taskrunv1alpha1.TaskRun, steps []ResolvedStep) {
	taskRun.Status.Steps = make([]taskrunv1alpha1.StepStatus, len(steps))
	for i, rs := range steps {
		taskRun.Status.Steps[i] = taskrunv1alpha1.StepStatus{
			Name:  rs.Step.Name,
			Phase: taskrunv1alpha1.StepPhasePending,
		}
	}
}

// SetCondition sets a named condition on the TaskRun.
func (m *StatusManager) SetCondition(taskRun *taskrunv1alpha1.TaskRun, condType string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&taskRun.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		ObservedGeneration: taskRun.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	})
}

// MarkStepSucceeded records a step as succeeded with duration and outputs.
func (m *StatusManager) MarkStepSucceeded(taskRun *taskrunv1alpha1.TaskRun, stepName string, duration time.Duration, outputs map[string]string, action string) {
	m.SetStepStatus(taskRun, taskrunv1alpha1.StepStatus{
		Name:     stepName,
		Phase:    taskrunv1alpha1.StepPhaseSucceeded,
		Duration: duration.Round(time.Millisecond).String(),
		Outputs:  outputs,
	})
	metrics.StepTotal.WithLabelValues(taskRun.Namespace, action, string(taskrunv1alpha1.StepPhaseSucceeded)).Inc()
	metrics.StepDuration.WithLabelValues(taskRun.Namespace, action).Observe(duration.Seconds())
}

// MarkStepFailed records a step as failed.
func (m *StatusManager) MarkStepFailed(taskRun *taskrunv1alpha1.TaskRun, stepName string, duration time.Duration, errMsg string, action string) {
	m.SetStepStatus(taskRun, taskrunv1alpha1.StepStatus{
		Name:     stepName,
		Phase:    taskrunv1alpha1.StepPhaseFailed,
		Duration: duration.Round(time.Millisecond).String(),
		Logs:     errMsg,
	})
	metrics.StepTotal.WithLabelValues(taskRun.Namespace, action, string(taskrunv1alpha1.StepPhaseFailed)).Inc()
	metrics.StepDuration.WithLabelValues(taskRun.Namespace, action).Observe(duration.Seconds())
}

// MarkFailed transitions the TaskRun to Failed with a reason and message.
func (m *StatusManager) MarkFailed(ctx context.Context, taskRun *taskrunv1alpha1.TaskRun, reason, message string) error {
	m.SetCondition(taskRun, "Failed", metav1.ConditionTrue, reason, message)
	return m.SetPhase(ctx, taskRun, taskrunv1alpha1.TaskRunPhaseFailed)
}

// MarkSucceeded transitions the TaskRun to Succeeded.
func (m *StatusManager) MarkSucceeded(ctx context.Context, taskRun *taskrunv1alpha1.TaskRun) error {
	m.SetCondition(taskRun, "Complete", metav1.ConditionTrue, "AllStepsSucceeded", fmt.Sprintf("%d steps completed", len(taskRun.Status.Steps)))
	return m.SetPhase(ctx, taskRun, taskrunv1alpha1.TaskRunPhaseSucceeded)
}

func (m *StatusManager) recordTaskMetrics(taskRun *taskrunv1alpha1.TaskRun) {
	metrics.TaskRunTotal.WithLabelValues(taskRun.Namespace, string(taskRun.Status.Phase)).Inc()
	if taskRun.Status.StartTime != nil && taskRun.Status.CompletionTime != nil {
		duration := taskRun.Status.CompletionTime.Sub(taskRun.Status.StartTime.Time)
		metrics.TaskRunDuration.WithLabelValues(taskRun.Namespace).Observe(duration.Seconds())
	}
}
