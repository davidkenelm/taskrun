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
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	taskrunv1alpha1 "github.com/davidkenelm/taskrun/api/v1alpha1"
)

// TaskRunReconciler reconciles a TaskRun object
type TaskRunReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Clientset kubernetes.Interface
}

// +kubebuilder:rbac:groups=taskrun.io,resources=taskruns,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=taskrun.io,resources=taskruns/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=taskrun.io,resources=taskruns/finalizers,verbs=update
// +kubebuilder:rbac:groups=taskrun.io,resources=stepdefinitions;clusterstepdefinitions,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=jobs;cronjobs,verbs=create;get;list;watch;delete;patch
// +kubebuilder:rbac:groups="",resources=secrets;configmaps,verbs=get;create;patch;update
// +kubebuilder:rbac:groups=apps,resources=deployments;statefulsets;daemonsets,verbs=get;patch;update
// +kubebuilder:rbac:groups="",resources=pods;pods/log,verbs=get;list;watch

func (r *TaskRunReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// 1. Fetch TaskRun
	var taskRun taskrunv1alpha1.TaskRun
	if err := r.Get(ctx, req.NamespacedName, &taskRun); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling TaskRun", "name", taskRun.Name, "phase", taskRun.Status.Phase)

	// Skip if already terminal.
	if taskRun.Status.Phase == taskrunv1alpha1.TaskRunPhaseSucceeded ||
		taskRun.Status.Phase == taskrunv1alpha1.TaskRunPhaseFailed {
		return ctrl.Result{}, nil
	}

	statusMgr := NewStatusManager(r.Client)

	// 2. Resolve StepDefinitions for each step
	resolver := NewStepResolver(r.Client, taskRun.Namespace)
	resolved, err := resolver.ResolveAll(ctx, taskRun.Spec.Steps)
	if err != nil {
		return ctrl.Result{}, statusMgr.MarkFailed(ctx, &taskRun, "ResolutionFailed", err.Error())
	}

	// 3. Validate params against StepDefinition schemas
	if err := ValidateStepParams(resolved); err != nil {
		return ctrl.Result{}, statusMgr.MarkFailed(ctx, &taskRun, "ValidationFailed", err.Error())
	}

	// 4. Validate step ordering: all runner steps must precede all API-native steps.
	if err := ValidateStepOrdering(resolved); err != nil {
		return ctrl.Result{}, statusMgr.MarkFailed(ctx, &taskRun, "InvalidStepOrdering", err.Error())
	}

	// 5. Partition steps: runner-based vs API-native
	partitioned := PartitionSteps(resolved)

	// Initialize step statuses on first reconcile.
	if taskRun.Status.Phase == "" || taskRun.Status.Phase == taskrunv1alpha1.TaskRunPhasePending {
		statusMgr.InitStepStatuses(&taskRun, resolved)
		if err := statusMgr.SetPhase(ctx, &taskRun, taskrunv1alpha1.TaskRunPhaseRunning); err != nil {
			return ctrl.Result{}, fmt.Errorf("setting Running phase: %w", err)
		}
	}

	// 6-8. Handle runner steps via Job/CronJob
	if len(partitioned.Runner) > 0 {
		// Scheduled TaskRuns use CronJobs.
		if taskRun.Spec.Schedule != "" {
			return r.reconcileCronJob(ctx, &taskRun, partitioned, statusMgr)
		}
		return r.reconcileJob(ctx, &taskRun, partitioned, statusMgr)
	}

	// No runner steps — execute API-native steps directly.
	return r.executeAPINativeSteps(ctx, &taskRun, resolved, statusMgr)
}

func (r *TaskRunReconciler) reconcileJob(ctx context.Context, taskRun *taskrunv1alpha1.TaskRun, partitioned PartitionedSteps, statusMgr *StatusManager) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	builder := NewJobBuilder(r.Scheme)

	// Check if Job already exists.
	var existingJob batchv1.Job
	jobKey := types.NamespacedName{Name: taskRun.Name + "-runner", Namespace: taskRun.Namespace}
	err := r.Get(ctx, jobKey, &existingJob)

	if apierrors.IsNotFound(err) {
		// Write step params to a ConfigMap so runner containers can read them from a file.
		if err := r.ensureParamsConfigMap(ctx, taskRun, partitioned.Runner); err != nil {
			return ctrl.Result{}, statusMgr.MarkFailed(ctx, taskRun, "ParamsConfigMapFailed", err.Error())
		}
		// Create the Job.
		job, err := builder.Build(taskRun, partitioned.Runner)
		if err != nil {
			return ctrl.Result{}, statusMgr.MarkFailed(ctx, taskRun, "JobBuildFailed", err.Error())
		}
		if err := r.Create(ctx, job); err != nil {
			return ctrl.Result{}, fmt.Errorf("creating Job: %w", err)
		}
		log.Info("Created runner Job", "job", job.Name)
		// Mark runner steps as Running.
		for _, rs := range partitioned.Runner {
			statusMgr.SetStepStatus(taskRun, taskrunv1alpha1.StepStatus{
				Name:  rs.Step.Name,
				Phase: taskrunv1alpha1.StepPhaseRunning,
			})
		}
		if err := r.Status().Update(ctx, taskRun); err != nil {
			return ctrl.Result{}, fmt.Errorf("updating step statuses: %w", err)
		}
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("getting Job: %w", err)
	}

	// Job exists — check completion.
	if existingJob.Status.Succeeded > 0 {
		log.Info("Runner Job completed successfully")
		r.collectRunnerLogs(ctx, taskRun, partitioned.Runner)
		stepOutputs := r.collectRunnerOutputs(ctx, taskRun)
		for _, rs := range partitioned.Runner {
			var outputs map[string]string
			if stepOutputs != nil {
				outputs = stepOutputs[rs.Step.Name]
			}
			statusMgr.MarkStepSucceeded(taskRun, rs.Step.Name, jobDuration(&existingJob), outputs, rs.Step.Action)
		}

		// Now execute API-native steps.
		return r.executeAPINativeStepsFromPartitioned(ctx, taskRun, partitioned, statusMgr)
	}

	if existingJob.Status.Failed > 0 {
		log.Info("Runner Job failed")
		r.collectRunnerLogs(ctx, taskRun, partitioned.Runner)
		for _, rs := range partitioned.Runner {
			statusMgr.MarkStepFailed(taskRun, rs.Step.Name, jobDuration(&existingJob), "runner job failed", rs.Step.Action)
		}
		return ctrl.Result{}, statusMgr.MarkFailed(ctx, taskRun, "RunnerFailed", "runner Job failed")
	}

	// Still running — requeue.
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

func (r *TaskRunReconciler) reconcileCronJob(ctx context.Context, taskRun *taskrunv1alpha1.TaskRun, partitioned PartitionedSteps, statusMgr *StatusManager) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	builder := NewJobBuilder(r.Scheme)

	var existingCronJob batchv1.CronJob
	cronJobKey := types.NamespacedName{Name: taskRun.Name + "-runner", Namespace: taskRun.Namespace}
	err := r.Get(ctx, cronJobKey, &existingCronJob)

	if apierrors.IsNotFound(err) {
		cronJob, err := builder.BuildCronJob(taskRun, partitioned.Runner)
		if err != nil {
			return ctrl.Result{}, statusMgr.MarkFailed(ctx, taskRun, "CronJobBuildFailed", err.Error())
		}
		if err := r.Create(ctx, cronJob); err != nil {
			return ctrl.Result{}, fmt.Errorf("creating CronJob: %w", err)
		}
		log.Info("Created runner CronJob", "cronjob", cronJob.Name, "schedule", taskRun.Spec.Schedule)
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("getting CronJob: %w", err)
	}

	// CronJob exists — reconciliation for scheduled runs is handled by
	// individual Job completions triggering re-reconcile via Owns().
	return ctrl.Result{}, nil
}

func (r *TaskRunReconciler) executeAPINativeSteps(ctx context.Context, taskRun *taskrunv1alpha1.TaskRun, resolved []ResolvedStep, statusMgr *StatusManager) (ctrl.Result, error) {
	executor := NewStepExecutor(r.Client, taskRun.Namespace)

	for _, rs := range resolved {
		if rs.Definition.Runner != nil {
			continue // Skip runner steps.
		}
		if err := r.executeOneStep(ctx, taskRun, rs, executor, statusMgr); err != nil {
			return ctrl.Result{}, statusMgr.MarkFailed(ctx, taskRun, "StepFailed", err.Error())
		}
	}

	return ctrl.Result{}, statusMgr.MarkSucceeded(ctx, taskRun)
}

func (r *TaskRunReconciler) executeAPINativeStepsFromPartitioned(ctx context.Context, taskRun *taskrunv1alpha1.TaskRun, partitioned PartitionedSteps, statusMgr *StatusManager) (ctrl.Result, error) {
	if len(partitioned.APINative) == 0 {
		return ctrl.Result{}, statusMgr.MarkSucceeded(ctx, taskRun)
	}

	executor := NewStepExecutor(r.Client, taskRun.Namespace)

	for _, rs := range partitioned.APINative {
		if err := r.executeOneStep(ctx, taskRun, rs, executor, statusMgr); err != nil {
			return ctrl.Result{}, statusMgr.MarkFailed(ctx, taskRun, "StepFailed", err.Error())
		}
	}

	return ctrl.Result{}, statusMgr.MarkSucceeded(ctx, taskRun)
}

func (r *TaskRunReconciler) executeOneStep(ctx context.Context, taskRun *taskrunv1alpha1.TaskRun, rs ResolvedStep, executor *StepExecutor, statusMgr *StatusManager) error {
	log := logf.FromContext(ctx)

	// Resolve template expressions in params.
	resolvedParams, err := ResolveTemplates(rs.Step.Params, taskRun.Status.Steps)
	if err != nil {
		statusMgr.MarkStepFailed(taskRun, rs.Step.Name, 0, err.Error(), rs.Step.Action)
		return fmt.Errorf("step %q template resolution: %w", rs.Step.Name, err)
	}

	// Mark step as running.
	statusMgr.SetStepStatus(taskRun, taskrunv1alpha1.StepStatus{
		Name:  rs.Step.Name,
		Phase: taskrunv1alpha1.StepPhaseRunning,
	})

	start := time.Now()
	outputs, err := executor.Execute(ctx, rs.Step.Action, resolvedParams)
	duration := time.Since(start)

	if err != nil {
		log.Error(err, "Step failed", "step", rs.Step.Name, "action", rs.Step.Action)
		statusMgr.MarkStepFailed(taskRun, rs.Step.Name, duration, err.Error(), rs.Step.Action)
		return fmt.Errorf("step %q (%s) failed: %w", rs.Step.Name, rs.Step.Action, err)
	}

	log.Info("Step succeeded", "step", rs.Step.Name, "action", rs.Step.Action, "duration", duration)
	statusMgr.MarkStepSucceeded(taskRun, rs.Step.Name, duration, outputs, rs.Step.Action)
	return nil
}

// ensureParamsConfigMap creates or updates a ConfigMap containing JSON params for each
// runner step. The ConfigMap is mounted read-only at /etc/step/params/ in the Job pod,
// so each runner container reads its params from /etc/step/params/<step-name>.json.
func (r *TaskRunReconciler) ensureParamsConfigMap(ctx context.Context, taskRun *taskrunv1alpha1.TaskRun, runnerSteps []ResolvedStep) error {
	data := make(map[string]string, len(runnerSteps))
	for _, rs := range runnerSteps {
		paramsJSON, err := json.Marshal(rs.Step.Params)
		if err != nil {
			return fmt.Errorf("marshalling params for step %q: %w", rs.Step.Name, err)
		}
		data[rs.Step.Name+".json"] = string(paramsJSON)
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      taskRun.Name + "-params",
			Namespace: taskRun.Namespace,
		},
		Data: data,
	}
	if err := controllerutil.SetControllerReference(taskRun, cm, r.Scheme); err != nil {
		return fmt.Errorf("setting owner reference on params ConfigMap: %w", err)
	}

	var existing corev1.ConfigMap
	err := r.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, &existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, cm)
	}
	if err != nil {
		return err
	}
	existing.Data = data
	return r.Update(ctx, &existing)
}

// collectRunnerOutputs reads the collect-outputs container's stdout from the runner pod
// and parses the TASKRUN_OUTPUTS= line written by the collector binary.
// Returns a map of step-name → output-key → value, or nil if unavailable.
func (r *TaskRunReconciler) collectRunnerOutputs(ctx context.Context, taskRun *taskrunv1alpha1.TaskRun) map[string]map[string]string {
	if r.Clientset == nil {
		return nil
	}
	log := logf.FromContext(ctx)

	var podList corev1.PodList
	if err := r.List(ctx, &podList,
		client.InNamespace(taskRun.Namespace),
		client.MatchingLabels{"taskrun.io/taskrun": taskRun.Name},
	); err != nil || len(podList.Items) == 0 {
		log.Error(err, "Failed to find runner pod for output collection")
		return nil
	}

	pod := podList.Items[0]
	req := r.Clientset.CoreV1().Pods(taskRun.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
		Container: "collect-outputs",
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		log.Error(err, "Failed to get collector container logs")
		return nil
	}
	logBytes, err := io.ReadAll(stream)
	_ = stream.Close()
	if err != nil {
		return nil
	}

	for line := range strings.SplitSeq(string(logBytes), "\n") {
		if !strings.HasPrefix(line, outputsLogPrefix) {
			continue
		}
		var allOutputs map[string]map[string]string
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, outputsLogPrefix)), &allOutputs); err != nil {
			log.Error(err, "Failed to parse collector output line")
			return nil
		}
		return allOutputs
	}
	return nil
}

// collectRunnerLogs reads the last N lines of logs from each runner init container.
func (r *TaskRunReconciler) collectRunnerLogs(ctx context.Context, taskRun *taskrunv1alpha1.TaskRun, runnerSteps []ResolvedStep) {
	if r.Clientset == nil {
		return
	}
	log := logf.FromContext(ctx)

	// Find the pod owned by the Job.
	var podList corev1.PodList
	if err := r.List(ctx, &podList,
		client.InNamespace(taskRun.Namespace),
		client.MatchingLabels{"taskrun.io/taskrun": taskRun.Name},
	); err != nil {
		log.Error(err, "Failed to list runner pods for log collection")
		return
	}
	if len(podList.Items) == 0 {
		return
	}

	pod := podList.Items[0]
	tailLines := int64(maxLogLines)

	for i, rs := range runnerSteps {
		containerName := fmt.Sprintf("step-%d-%s", i, rs.Step.Name)
		req := r.Clientset.CoreV1().Pods(taskRun.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
			Container: containerName,
			TailLines: &tailLines,
		})
		stream, err := req.Stream(ctx)
		if err != nil {
			log.Error(err, "Failed to get logs for container", "container", containerName)
			continue
		}
		logBytes, err := io.ReadAll(stream)
		_ = stream.Close()
		if err != nil {
			log.Error(err, "Failed to read logs for container", "container", containerName)
			continue
		}

		// Update the step status with logs.
		for j := range taskRun.Status.Steps {
			if taskRun.Status.Steps[j].Name == rs.Step.Name {
				taskRun.Status.Steps[j].Logs = truncateLines(string(logBytes), maxLogLines)
				break
			}
		}
	}
}

func truncateLines(s string, maxLines int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, "\n")
}

func jobDuration(job *batchv1.Job) time.Duration {
	if job.Status.StartTime == nil || job.Status.CompletionTime == nil {
		return 0
	}
	return job.Status.CompletionTime.Sub(job.Status.StartTime.Time)
}

// SetupWithManager sets up the controller with the Manager.
func (r *TaskRunReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&taskrunv1alpha1.TaskRun{}).
		Owns(&batchv1.Job{}).
		Owns(&batchv1.CronJob{}).
		Named("taskrun").
		Complete(r)
}
