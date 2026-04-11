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

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	taskrunv1alpha1 "github.com/davidkenelm/taskrun/api/v1alpha1"
)

// TaskRunReconciler reconciles a TaskRun object
type TaskRunReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=taskrun.io,resources=taskruns,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=taskrun.io,resources=taskruns/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=taskrun.io,resources=taskruns/finalizers,verbs=update
// +kubebuilder:rbac:groups=taskrun.io,resources=stepdefinitions;clusterstepdefinitions,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=jobs;cronjobs,verbs=create;get;list;watch;delete;patch
// +kubebuilder:rbac:groups="",resources=secrets;configmaps,verbs=get;create;patch
// +kubebuilder:rbac:groups=apps,resources=deployments;statefulsets;daemonsets,verbs=get;patch
// +kubebuilder:rbac:groups="",resources=pods;pods/log,verbs=get;list;watch

func (r *TaskRunReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// 1. Fetch TaskRun
	var taskRun taskrunv1alpha1.TaskRun
	if err := r.Get(ctx, req.NamespacedName, &taskRun); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling TaskRun", "name", taskRun.Name, "phase", taskRun.Status.Phase)

	// 2. Resolve StepDefinitions for each step
	// TODO: Look up StepDefinition (namespace-local first) then ClusterStepDefinition for each step action

	// 3. Validate params against StepDefinition schemas (reconcile-time validation)
	// TODO: Validate each step's params against the resolved StepDefinition's JSON Schema

	// 4. Partition steps: runner-based vs API-native
	// TODO: Separate steps into those needing runner containers and those executed by the controller

	// 5. Generate Job/CronJob spec for runner steps
	// TODO: Build Job spec with auth init container, runner init containers, shared emptyDir

	// 6. Create/update Job or CronJob
	// TODO: If schedule is set, create CronJob; otherwise create Job

	// 7. Watch Job completion
	// TODO: Check Job status, requeue if still running

	// 8. Execute API-native steps in-controller
	// TODO: After runner pod succeeds, execute api-native steps (secret-update, rollout-restart, etc.)

	// 9. Collect runner logs → status.steps[].logs
	// TODO: Read last N lines from runner pod logs and write to step status

	// 10. Write status
	// TODO: Update TaskRun.Status with phase, step results, conditions

	return ctrl.Result{}, fmt.Errorf("reconciliation not yet implemented")
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
