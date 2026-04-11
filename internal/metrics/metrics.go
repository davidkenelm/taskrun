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

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// TaskRunTotal counts the total number of TaskRun executions, labeled by namespace and final phase.
	TaskRunTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "taskrun_total",
			Help: "Total number of TaskRun executions by final phase",
		},
		[]string{"namespace", "phase"},
	)

	// TaskRunDuration tracks the wall-clock duration of TaskRun executions in seconds.
	TaskRunDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "taskrun_duration_seconds",
			Help:    "Duration of TaskRun executions in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"namespace"},
	)

	// StepDuration tracks the wall-clock duration of individual step executions in seconds.
	StepDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "taskrun_step_duration_seconds",
			Help:    "Duration of individual step executions in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"namespace", "action"},
	)

	// StepTotal counts step executions labeled by namespace, action type, and phase.
	StepTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "taskrun_step_total",
			Help: "Total number of step executions by action and phase",
		},
		[]string{"namespace", "action", "phase"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		TaskRunTotal,
		TaskRunDuration,
		StepDuration,
		StepTotal,
	)
}
