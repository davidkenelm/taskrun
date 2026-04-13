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

var _ = Describe("TaskRun Controller", func() {
	const (
		timeout  = 10 * time.Second
		interval = 250 * time.Millisecond
	)

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
			Expect(result.Requeue).To(BeFalse())

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
