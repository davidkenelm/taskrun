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
	"encoding/json"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	taskrunv1alpha1 "github.com/davidkenelm/taskrun/api/v1alpha1"
)

const (
	stepVolumeName = "step-data"
	stepMountPath  = "/etc/step"
	authMountPath  = "/etc/step/auth"
	maxLogLines    = 100
)

// PartitionedSteps separates resolved steps into runner-based and API-native.
type PartitionedSteps struct {
	Runner    []ResolvedStep
	APINative []ResolvedStep
	// Order preserves the original step ordering with a flag for type.
	Order []StepOrder
}

// StepOrder records a step's position and whether it's runner-based.
type StepOrder struct {
	Name     string
	IsRunner bool
}

// PartitionSteps separates steps by whether they have a runner image.
func PartitionSteps(steps []ResolvedStep) PartitionedSteps {
	p := PartitionedSteps{}
	for _, rs := range steps {
		isRunner := rs.Definition.Runner != nil
		p.Order = append(p.Order, StepOrder{Name: rs.Step.Name, IsRunner: isRunner})
		if isRunner {
			p.Runner = append(p.Runner, rs)
		} else {
			p.APINative = append(p.APINative, rs)
		}
	}
	return p
}

// JobBuilder constructs Kubernetes Jobs and CronJobs from TaskRun specs.
type JobBuilder struct {
	scheme *runtime.Scheme
}

// NewJobBuilder creates a new JobBuilder.
func NewJobBuilder(scheme *runtime.Scheme) *JobBuilder {
	return &JobBuilder{scheme: scheme}
}

// Build creates a Job spec for the runner-based steps. Returns nil if there are no runner steps.
func (b *JobBuilder) Build(taskRun *taskrunv1alpha1.TaskRun, runnerSteps []ResolvedStep) (*batchv1.Job, error) {
	if len(runnerSteps) == 0 {
		return nil, nil
	}

	initContainers, err := b.buildInitContainers(taskRun, runnerSteps)
	if err != nil {
		return nil, err
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName(taskRun),
			Namespace: taskRun.Namespace,
			Labels:    jobLabels(taskRun),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: int32Ptr(0),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: jobLabels(taskRun),
				},
				Spec: corev1.PodSpec{
					RestartPolicy:  corev1.RestartPolicyNever,
					InitContainers: initContainers,
					Containers: []corev1.Container{
						{
							Name:    "complete",
							Image:   "gcr.io/google_containers/pause:3.2",
							Command: []string{"/pause"},
						},
					},
					Volumes: b.buildVolumes(taskRun),
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(taskRun, job, b.scheme); err != nil {
		return nil, fmt.Errorf("setting owner reference: %w", err)
	}
	return job, nil
}

// BuildCronJob wraps a Job template in a CronJob for scheduled TaskRuns.
func (b *JobBuilder) BuildCronJob(taskRun *taskrunv1alpha1.TaskRun, runnerSteps []ResolvedStep) (*batchv1.CronJob, error) {
	job, err := b.Build(taskRun, runnerSteps)
	if err != nil {
		return nil, err
	}

	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName(taskRun),
			Namespace: taskRun.Namespace,
			Labels:    jobLabels(taskRun),
		},
		Spec: batchv1.CronJobSpec{
			Schedule:          taskRun.Spec.Schedule,
			ConcurrencyPolicy: mapConcurrencyPolicy(taskRun.Spec.ConcurrencyPolicy),
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: job.ObjectMeta,
				Spec:       job.Spec,
			},
		},
	}

	if err := controllerutil.SetControllerReference(taskRun, cronJob, b.scheme); err != nil {
		return nil, fmt.Errorf("setting owner reference: %w", err)
	}
	return cronJob, nil
}

func (b *JobBuilder) buildInitContainers(taskRun *taskrunv1alpha1.TaskRun, steps []ResolvedStep) ([]corev1.Container, error) {
	var containers []corev1.Container

	// Auth init container if auth block is present.
	if taskRun.Spec.Auth != nil {
		containers = append(containers, b.authInitContainer(taskRun.Spec.Auth))
	}

	// One init container per runner step, in order.
	for i, rs := range steps {
		paramsJSON, err := json.Marshal(rs.Step.Params)
		if err != nil {
			return nil, fmt.Errorf("marshalling params for step %q: %w", rs.Step.Name, err)
		}

		c := corev1.Container{
			Name:  fmt.Sprintf("step-%d-%s", i, rs.Step.Name),
			Image: rs.Definition.Runner.Image,
			Command: []string{
				"/runner",
				"--step-name", rs.Step.Name,
				"--params", string(paramsJSON),
			},
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      stepVolumeName,
					MountPath: stepMountPath,
				},
			},
		}
		containers = append(containers, c)
	}
	return containers, nil
}

func (b *JobBuilder) authInitContainer(auth *taskrunv1alpha1.AuthSpec) corev1.Container {
	c := corev1.Container{
		Name:    "auth",
		Image:   "ghcr.io/davidkenelm/taskrun-runners/auth:0.1.0",
		Command: []string{"/auth-init", "--type", string(auth.Type)},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      stepVolumeName,
				MountPath: stepMountPath,
			},
		},
	}

	if auth.TokenEndpoint != "" {
		c.Env = append(c.Env, corev1.EnvVar{
			Name:  "TOKEN_ENDPOINT",
			Value: auth.TokenEndpoint,
		})
	}

	if auth.CredentialsFrom != nil {
		ref := auth.CredentialsFrom.SecretRef
		c.EnvFrom = append(c.EnvFrom, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: ref.Name,
				},
			},
		})
	}
	return c
}

func (b *JobBuilder) buildVolumes(_ *taskrunv1alpha1.TaskRun) []corev1.Volume {
	volumes := []corev1.Volume{
		{
			Name: stepVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}
	return volumes
}

func jobName(taskRun *taskrunv1alpha1.TaskRun) string {
	return taskRun.Name + "-runner"
}

func jobLabels(taskRun *taskrunv1alpha1.TaskRun) map[string]string {
	return map[string]string{
		"taskrun.io/taskrun":           taskRun.Name,
		"app.kubernetes.io/managed-by": "taskrun-controller",
	}
}

func mapConcurrencyPolicy(cp taskrunv1alpha1.ConcurrencyPolicy) batchv1.ConcurrencyPolicy {
	switch cp {
	case taskrunv1alpha1.ConcurrencyPolicyAllow:
		return batchv1.AllowConcurrent
	case taskrunv1alpha1.ConcurrencyPolicyReplace:
		return batchv1.ReplaceConcurrent
	default:
		return batchv1.ForbidConcurrent
	}
}

func int32Ptr(i int32) *int32 { return &i }
