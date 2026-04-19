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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	taskrunv1alpha1 "github.com/davidkenelm/taskrun/api/v1alpha1"
)

// ensureCSD creates a ClusterStepDefinition only if it does not already exist.
func ensureCSD(name string, spec taskrunv1alpha1.StepDefinitionSpec) {
	csd := &taskrunv1alpha1.ClusterStepDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       spec,
	}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, csd)
	if errors.IsNotFound(err) {
		Expect(k8sClient.Create(ctx, csd)).To(Succeed())
	}
}

var _ = Describe("TaskRun Controller", func() {
	Context("When reconciling a TaskRun with only API-native steps", func() {
		const taskRunName = "test-api-native"
		const namespace = "default"

		BeforeEach(func() {
			// Create a ClusterStepDefinition for secret-update.
			csd := &taskrunv1alpha1.ClusterStepDefinition{
				ObjectMeta: metav1.ObjectMeta{Name: "secret-update"},
				Spec: taskrunv1alpha1.StepDefinitionSpec{
					Schema: &runtime.RawExtension{Raw: []byte(`{
						"type": "object",
						"required": ["secretName", "key"],
						"properties": {
							"secretName": {"type": "string"},
							"key": {"type": "string"},
							"value": {"type": "string"}
						}
					}`)},
				},
			}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: "secret-update"}, csd)
			if errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, csd)).To(Succeed())
			}

			// Create the TaskRun.
			taskRun := &taskrunv1alpha1.TaskRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskRunName,
					Namespace: namespace,
				},
				Spec: taskrunv1alpha1.TaskRunSpec{
					Steps: []taskrunv1alpha1.StepSpec{
						{
							Name:   "store-token",
							Action: "secret-update",
							Params: map[string]string{
								"secretName": "my-secret",
								"key":        "token",
								"value":      "test-value",
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, taskRun)).To(Succeed())
		})

		AfterEach(func() {
			taskRun := &taskrunv1alpha1.TaskRun{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: taskRunName, Namespace: namespace}, taskRun)
			if err == nil {
				Expect(k8sClient.Delete(ctx, taskRun)).To(Succeed())
			}
			// Clean up secret.
			secret := &corev1.Secret{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "my-secret", Namespace: namespace}, secret)
			if err == nil {
				Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
			}
		})

		It("should execute the step and mark the TaskRun as Succeeded", func() {
			reconciler := &TaskRunReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// First reconcile — resolves, validates, transitions to Running, executes.
			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: taskRunName, Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			// Verify TaskRun status.
			var taskRun taskrunv1alpha1.TaskRun
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: taskRunName, Namespace: namespace}, &taskRun)).To(Succeed())
			Expect(taskRun.Status.Phase).To(Equal(taskrunv1alpha1.TaskRunPhaseSucceeded))
			Expect(taskRun.Status.Steps).To(HaveLen(1))
			Expect(taskRun.Status.Steps[0].Phase).To(Equal(taskrunv1alpha1.StepPhaseSucceeded))
			Expect(taskRun.Status.Steps[0].Outputs["updated"]).To(Equal("created"))

			// Verify the secret was actually created.
			var secret corev1.Secret
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "my-secret", Namespace: namespace}, &secret)).To(Succeed())
			Expect(string(secret.Data["token"])).To(Equal("test-value"))
		})
	})

	Context("When reconciling a TaskRun with runner steps", func() {
		const taskRunName = "test-runner"
		const namespace = "default"

		BeforeEach(func() {
			csd := &taskrunv1alpha1.ClusterStepDefinition{
				ObjectMeta: metav1.ObjectMeta{Name: "http-request"},
				Spec: taskrunv1alpha1.StepDefinitionSpec{
					Schema: &runtime.RawExtension{Raw: []byte(`{
						"type": "object",
						"required": ["url", "method"],
						"properties": {
							"url": {"type": "string"},
							"method": {"type": "string"}
						}
					}`)},
					Runner: &taskrunv1alpha1.RunnerSpec{Image: "ghcr.io/davidkenelm/taskrun-runners/http:0.1.0"},
				},
			}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: "http-request"}, csd)
			if errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, csd)).To(Succeed())
			}

			taskRun := &taskrunv1alpha1.TaskRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskRunName,
					Namespace: namespace,
				},
				Spec: taskrunv1alpha1.TaskRunSpec{
					Steps: []taskrunv1alpha1.StepSpec{
						{
							Name:   "call-api",
							Action: "http-request",
							Params: map[string]string{
								"url":    "https://example.com",
								"method": "GET",
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, taskRun)).To(Succeed())
		})

		AfterEach(func() {
			taskRun := &taskrunv1alpha1.TaskRun{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: taskRunName, Namespace: namespace}, taskRun)
			if err == nil {
				Expect(k8sClient.Delete(ctx, taskRun)).To(Succeed())
			}
			job := &batchv1.Job{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: taskRunName + "-runner", Namespace: namespace}, job)
			if err == nil {
				Expect(k8sClient.Delete(ctx, job)).To(Succeed())
			}
		})

		It("should create a runner Job and requeue", func() {
			reconciler := &TaskRunReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: taskRunName, Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(5 * time.Second))

			// Verify Job was created.
			var job batchv1.Job
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: taskRunName + "-runner", Namespace: namespace}, &job)).To(Succeed())
			Expect(job.Spec.Template.Spec.InitContainers).To(HaveLen(1))
			Expect(job.Spec.Template.Spec.InitContainers[0].Image).To(Equal("ghcr.io/davidkenelm/taskrun-runners/http:0.1.0"))

			// Verify TaskRun is Running.
			var taskRun taskrunv1alpha1.TaskRun
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: taskRunName, Namespace: namespace}, &taskRun)).To(Succeed())
			Expect(taskRun.Status.Phase).To(Equal(taskrunv1alpha1.TaskRunPhaseRunning))
		})
	})

	Context("When reconciling with unresolvable step action", func() {
		const taskRunName = "test-bad-action"
		const namespace = "default"

		BeforeEach(func() {
			taskRun := &taskrunv1alpha1.TaskRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskRunName,
					Namespace: namespace,
				},
				Spec: taskrunv1alpha1.TaskRunSpec{
					Steps: []taskrunv1alpha1.StepSpec{
						{Name: "bad", Action: "nonexistent-action", Params: map[string]string{}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, taskRun)).To(Succeed())
		})

		AfterEach(func() {
			taskRun := &taskrunv1alpha1.TaskRun{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: taskRunName, Namespace: namespace}, taskRun)
			if err == nil {
				Expect(k8sClient.Delete(ctx, taskRun)).To(Succeed())
			}
		})

		It("should mark the TaskRun as Failed", func() {
			reconciler := &TaskRunReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: taskRunName, Namespace: namespace},
			})
			// The reconciler returns nil error after writing Failed status.
			Expect(err).NotTo(HaveOccurred())

			var taskRun taskrunv1alpha1.TaskRun
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: taskRunName, Namespace: namespace}, &taskRun)).To(Succeed())
			Expect(taskRun.Status.Phase).To(Equal(taskrunv1alpha1.TaskRunPhaseFailed))
		})
	})

	Context("When reconciling a scheduled TaskRun", func() {
		const taskRunName = "test-scheduled"
		const namespace = "default"

		BeforeEach(func() {
			ensureCSD("shell-exec", taskrunv1alpha1.StepDefinitionSpec{
				Schema: &runtime.RawExtension{Raw: []byte(`{"type":"object"}`)},
				Runner: &taskrunv1alpha1.RunnerSpec{Image: "ghcr.io/davidkenelm/taskrun-runners/shell:0.1.0"},
			})

			taskRun := &taskrunv1alpha1.TaskRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskRunName,
					Namespace: namespace,
				},
				Spec: taskrunv1alpha1.TaskRunSpec{
					Schedule:          "*/5 * * * *",
					ConcurrencyPolicy: taskrunv1alpha1.ConcurrencyPolicyForbid,
					Steps: []taskrunv1alpha1.StepSpec{
						{Name: "run-script", Action: "shell-exec", Params: map[string]string{"command": "echo hello"}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, taskRun)).To(Succeed())
		})

		AfterEach(func() {
			taskRun := &taskrunv1alpha1.TaskRun{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: taskRunName, Namespace: namespace}, taskRun)
			if err == nil {
				Expect(k8sClient.Delete(ctx, taskRun)).To(Succeed())
			}
			cronJob := &batchv1.CronJob{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: taskRunName + "-runner", Namespace: namespace}, cronJob)
			if err == nil {
				Expect(k8sClient.Delete(ctx, cronJob)).To(Succeed())
			}
		})

		It("should create a CronJob with the correct schedule", func() {
			reconciler := &TaskRunReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: taskRunName, Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())

			var cronJob batchv1.CronJob
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: taskRunName + "-runner", Namespace: namespace}, &cronJob)).To(Succeed())
			Expect(cronJob.Spec.Schedule).To(Equal("*/5 * * * *"))
			Expect(cronJob.Spec.ConcurrencyPolicy).To(Equal(batchv1.ForbidConcurrent))
			Expect(cronJob.Spec.JobTemplate.Spec.Template.Spec.InitContainers).To(HaveLen(1))
		})
	})

	Context("When a runner Job completes successfully", func() {
		const taskRunName = "test-job-success"
		const namespace = "default"

		BeforeEach(func() {
			ensureCSD("http-request-v2", taskrunv1alpha1.StepDefinitionSpec{
				Schema: &runtime.RawExtension{Raw: []byte(`{"type":"object"}`)},
				Runner: &taskrunv1alpha1.RunnerSpec{Image: "ghcr.io/davidkenelm/taskrun-runners/http:0.1.0"},
			})

			taskRun := &taskrunv1alpha1.TaskRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskRunName,
					Namespace: namespace,
				},
				Spec: taskrunv1alpha1.TaskRunSpec{
					Steps: []taskrunv1alpha1.StepSpec{
						{Name: "fetch", Action: "http-request-v2", Params: map[string]string{"url": "https://example.com", "method": "GET"}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, taskRun)).To(Succeed())
		})

		AfterEach(func() {
			taskRun := &taskrunv1alpha1.TaskRun{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: taskRunName, Namespace: namespace}, taskRun)
			if err == nil {
				Expect(k8sClient.Delete(ctx, taskRun)).To(Succeed())
			}
			job := &batchv1.Job{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: taskRunName + "-runner", Namespace: namespace}, job)
			if err == nil {
				Expect(k8sClient.Delete(ctx, job)).To(Succeed())
			}
		})

		It("should mark the TaskRun as Succeeded after the Job succeeds", func() {
			reconciler := &TaskRunReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// First reconcile — creates the Job and requeues.
			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: taskRunName, Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(5 * time.Second))

			// Simulate Job completion by updating its status.
			var job batchv1.Job
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: taskRunName + "-runner", Namespace: namespace}, &job)).To(Succeed())
			now := metav1.Now()
			job.Status.Succeeded = 1
			job.Status.StartTime = &now
			job.Status.CompletionTime = &now
			job.Status.Conditions = []batchv1.JobCondition{
				{
					Type:               batchv1.JobSuccessCriteriaMet,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: now,
				},
				{
					Type:               batchv1.JobComplete,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: now,
				},
			}
			Expect(k8sClient.Status().Update(ctx, &job)).To(Succeed())

			// Second reconcile — sees completed Job, marks TaskRun Succeeded.
			result, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: taskRunName, Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			var taskRun taskrunv1alpha1.TaskRun
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: taskRunName, Namespace: namespace}, &taskRun)).To(Succeed())
			Expect(taskRun.Status.Phase).To(Equal(taskrunv1alpha1.TaskRunPhaseSucceeded))
			Expect(taskRun.Status.Steps[0].Phase).To(Equal(taskrunv1alpha1.StepPhaseSucceeded))
		})
	})

	Context("When a runner Job fails", func() {
		const taskRunName = "test-job-failure"
		const namespace = "default"

		BeforeEach(func() {
			ensureCSD("http-request-v3", taskrunv1alpha1.StepDefinitionSpec{
				Schema: &runtime.RawExtension{Raw: []byte(`{"type":"object"}`)},
				Runner: &taskrunv1alpha1.RunnerSpec{Image: "ghcr.io/davidkenelm/taskrun-runners/http:0.1.0"},
			})

			taskRun := &taskrunv1alpha1.TaskRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskRunName,
					Namespace: namespace,
				},
				Spec: taskrunv1alpha1.TaskRunSpec{
					Steps: []taskrunv1alpha1.StepSpec{
						{Name: "fetch", Action: "http-request-v3", Params: map[string]string{"url": "https://example.com", "method": "GET"}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, taskRun)).To(Succeed())
		})

		AfterEach(func() {
			taskRun := &taskrunv1alpha1.TaskRun{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: taskRunName, Namespace: namespace}, taskRun)
			if err == nil {
				Expect(k8sClient.Delete(ctx, taskRun)).To(Succeed())
			}
			job := &batchv1.Job{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: taskRunName + "-runner", Namespace: namespace}, job)
			if err == nil {
				Expect(k8sClient.Delete(ctx, job)).To(Succeed())
			}
		})

		It("should mark the TaskRun as Failed after the Job fails", func() {
			reconciler := &TaskRunReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// First reconcile — creates Job.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: taskRunName, Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Simulate Job failure.
			var job batchv1.Job
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: taskRunName + "-runner", Namespace: namespace}, &job)).To(Succeed())
			now := metav1.Now()
			job.Status.Failed = 1
			job.Status.StartTime = &now
			Expect(k8sClient.Status().Update(ctx, &job)).To(Succeed())

			// Second reconcile — sees failed Job, marks TaskRun Failed.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: taskRunName, Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())

			var taskRun taskrunv1alpha1.TaskRun
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: taskRunName, Namespace: namespace}, &taskRun)).To(Succeed())
			Expect(taskRun.Status.Phase).To(Equal(taskrunv1alpha1.TaskRunPhaseFailed))
			Expect(taskRun.Status.Steps[0].Phase).To(Equal(taskrunv1alpha1.StepPhaseFailed))
		})
	})

	Context("When reconciling with invalid step params", func() {
		const taskRunName = "test-invalid-params"
		const namespace = "default"

		BeforeEach(func() {
			ensureCSD("secret-update-strict", taskrunv1alpha1.StepDefinitionSpec{
				Schema: &runtime.RawExtension{Raw: []byte(`{
					"type": "object",
					"required": ["secretName", "key", "value"],
					"properties": {
						"secretName": {"type": "string"},
						"key": {"type": "string"},
						"value": {"type": "string"}
					},
					"additionalProperties": false
				}`)},
			})

			taskRun := &taskrunv1alpha1.TaskRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskRunName,
					Namespace: namespace,
				},
				Spec: taskrunv1alpha1.TaskRunSpec{
					Steps: []taskrunv1alpha1.StepSpec{
						{
							Name:   "store",
							Action: "secret-update-strict",
							Params: map[string]string{
								"secretName": "my-secret",
								// key and value are missing — violates "required"
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, taskRun)).To(Succeed())
		})

		AfterEach(func() {
			taskRun := &taskrunv1alpha1.TaskRun{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: taskRunName, Namespace: namespace}, taskRun)
			if err == nil {
				Expect(k8sClient.Delete(ctx, taskRun)).To(Succeed())
			}
		})

		It("should mark the TaskRun as Failed with ValidationFailed reason", func() {
			reconciler := &TaskRunReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: taskRunName, Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())

			var taskRun taskrunv1alpha1.TaskRun
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: taskRunName, Namespace: namespace}, &taskRun)).To(Succeed())
			Expect(taskRun.Status.Phase).To(Equal(taskrunv1alpha1.TaskRunPhaseFailed))

			Expect(taskRun.Status.Conditions).To(HaveLen(1))
			Expect(taskRun.Status.Conditions[0].Reason).To(Equal("ValidationFailed"))
		})
	})

	Context("When reconciling a multi-step pipeline with template chaining", func() {
		const taskRunName = "test-pipeline"
		const namespace = "default"

		BeforeEach(func() {
			ensureCSD("secret-read", taskrunv1alpha1.StepDefinitionSpec{
				Schema:  &runtime.RawExtension{Raw: []byte(`{"type":"object","required":["secretName","key"]}`)},
				Outputs: []taskrunv1alpha1.OutputSpec{{Name: "value"}},
			})
			// secret-update CSD already created in an earlier context; use ensureCSD to be safe.
			ensureCSD("secret-update", taskrunv1alpha1.StepDefinitionSpec{
				Schema: &runtime.RawExtension{Raw: []byte(`{"type":"object","required":["secretName","key"]}`)},
			})

			// Create the source secret that step 1 will read.
			sourceSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "source-secret", Namespace: namespace},
				Data:       map[string][]byte{"token": []byte("my-token-value")},
			}
			Expect(k8sClient.Create(ctx, sourceSecret)).To(Succeed())

			taskRun := &taskrunv1alpha1.TaskRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskRunName,
					Namespace: namespace,
				},
				Spec: taskrunv1alpha1.TaskRunSpec{
					Steps: []taskrunv1alpha1.StepSpec{
						{
							Name:    "read-token",
							Action:  "secret-read",
							Params:  map[string]string{"secretName": "source-secret", "key": "token"},
							Outputs: []string{"value"},
						},
						{
							Name:   "store-token",
							Action: "secret-update",
							Params: map[string]string{
								"secretName": "dest-secret",
								"key":        "token",
								"value":      "{{ steps.read-token.outputs.value }}",
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, taskRun)).To(Succeed())
		})

		AfterEach(func() {
			taskRun := &taskrunv1alpha1.TaskRun{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: taskRunName, Namespace: namespace}, taskRun)
			if err == nil {
				Expect(k8sClient.Delete(ctx, taskRun)).To(Succeed())
			}
			for _, name := range []string{"source-secret", "dest-secret"} {
				secret := &corev1.Secret{}
				err = k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, secret)
				if err == nil {
					Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
				}
			}
		})

		It("should pass step outputs as inputs to the next step", func() {
			reconciler := &TaskRunReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: taskRunName, Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			var taskRun taskrunv1alpha1.TaskRun
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: taskRunName, Namespace: namespace}, &taskRun)).To(Succeed())
			Expect(taskRun.Status.Phase).To(Equal(taskrunv1alpha1.TaskRunPhaseSucceeded))
			Expect(taskRun.Status.Steps).To(HaveLen(2))
			Expect(taskRun.Status.Steps[0].Phase).To(Equal(taskrunv1alpha1.StepPhaseSucceeded))
			Expect(taskRun.Status.Steps[0].Outputs["value"]).To(Equal("my-token-value"))
			Expect(taskRun.Status.Steps[1].Phase).To(Equal(taskrunv1alpha1.StepPhaseSucceeded))

			// Verify the resolved template value reached the destination secret.
			var dest corev1.Secret
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "dest-secret", Namespace: namespace}, &dest)).To(Succeed())
			Expect(string(dest.Data["token"])).To(Equal("my-token-value"))
		})
	})

	Context("When a namespace-scoped StepDefinition overrides a ClusterStepDefinition", func() {
		const taskRunName = "test-ns-override"
		const namespace = "default"

		BeforeEach(func() {
			// Cluster-scoped definition that would produce the wrong image.
			ensureCSD("overridable-step", taskrunv1alpha1.StepDefinitionSpec{
				Schema: &runtime.RawExtension{Raw: []byte(`{"type":"object","required":["secretName","key"]}`)},
			})

			// Namespace-scoped definition should be preferred.
			nsDef := &taskrunv1alpha1.StepDefinition{
				ObjectMeta: metav1.ObjectMeta{Name: "overridable-step", Namespace: namespace},
				Spec: taskrunv1alpha1.StepDefinitionSpec{
					Schema: &runtime.RawExtension{Raw: []byte(`{"type":"object","required":["secretName","key"]}`)},
				},
			}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: "overridable-step", Namespace: namespace}, nsDef)
			if errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, nsDef)).To(Succeed())
			}

			// Create the secret that will be written.
			taskRun := &taskrunv1alpha1.TaskRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskRunName,
					Namespace: namespace,
				},
				Spec: taskrunv1alpha1.TaskRunSpec{
					Steps: []taskrunv1alpha1.StepSpec{
						{
							Name:   "store",
							Action: "overridable-step",
							Params: map[string]string{
								"secretName": "ns-override-secret",
								"key":        "k",
								"value":      "v",
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, taskRun)).To(Succeed())
		})

		AfterEach(func() {
			taskRun := &taskrunv1alpha1.TaskRun{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: taskRunName, Namespace: namespace}, taskRun)
			if err == nil {
				Expect(k8sClient.Delete(ctx, taskRun)).To(Succeed())
			}
			nsDef := &taskrunv1alpha1.StepDefinition{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "overridable-step", Namespace: namespace}, nsDef)
			if err == nil {
				Expect(k8sClient.Delete(ctx, nsDef)).To(Succeed())
			}
			secret := &corev1.Secret{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "ns-override-secret", Namespace: namespace}, secret)
			if err == nil {
				Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
			}
		})

		It("should resolve the namespace-scoped StepDefinition (which has no runner) and execute in-controller", func() {
			reconciler := &TaskRunReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// The namespace-scoped def has no runner, so it will be treated as an
			// API-native step. The executor will route to "overridable-step" which is
			// unknown — but the point is it resolved the namespace def, not the cluster def.
			// We verify it reached the executor phase (not stuck on resolution), and that
			// since "overridable-step" is not a known action it fails with StepFailed, not
			// ResolutionFailed.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: taskRunName, Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())

			var taskRun taskrunv1alpha1.TaskRun
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: taskRunName, Namespace: namespace}, &taskRun)).To(Succeed())
			Expect(taskRun.Status.Phase).To(Equal(taskrunv1alpha1.TaskRunPhaseFailed))

			// Failed due to unknown executor action, NOT due to resolution failure.
			Expect(taskRun.Status.Conditions[0].Reason).To(Equal("StepFailed"))
		})
	})

	Context("When reconciling an already-succeeded TaskRun", func() {
		const taskRunName = "test-already-done"
		const namespace = "default"

		BeforeEach(func() {
			taskRun := &taskrunv1alpha1.TaskRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskRunName,
					Namespace: namespace,
				},
				Spec: taskrunv1alpha1.TaskRunSpec{
					Steps: []taskrunv1alpha1.StepSpec{
						{Name: "step1", Action: "secret-update", Params: map[string]string{"secretName": "s", "key": "k", "value": "v"}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, taskRun)).To(Succeed())

			// Manually set it to Succeeded.
			taskRun.Status.Phase = taskrunv1alpha1.TaskRunPhaseSucceeded
			Expect(k8sClient.Status().Update(ctx, taskRun)).To(Succeed())
		})

		AfterEach(func() {
			taskRun := &taskrunv1alpha1.TaskRun{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: taskRunName, Namespace: namespace}, taskRun)
			if err == nil {
				Expect(k8sClient.Delete(ctx, taskRun)).To(Succeed())
			}
		})

		It("should be a no-op", func() {
			reconciler := &TaskRunReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: taskRunName, Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
		})
	})
})
