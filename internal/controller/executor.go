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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// StepExecutor executes API-native steps in-controller.
type StepExecutor struct {
	client    client.Client
	namespace string
}

// NewStepExecutor creates an executor scoped to a namespace.
func NewStepExecutor(c client.Client, namespace string) *StepExecutor {
	return &StepExecutor{client: c, namespace: namespace}
}

// Execute runs an API-native step and returns its outputs.
func (e *StepExecutor) Execute(ctx context.Context, action string, params map[string]string) (map[string]string, error) {
	switch action {
	case "secret-update":
		return e.secretUpdate(ctx, params)
	case "secret-read":
		return e.secretRead(ctx, params)
	case "rollout-restart":
		return e.rolloutRestart(ctx, params)
	case "configmap-update":
		return e.configmapUpdate(ctx, params)
	case "wait":
		return e.wait(ctx, params)
	case "assert":
		return e.assert(ctx, params)
	default:
		return nil, fmt.Errorf("unknown API-native action: %q", action)
	}
}

func (e *StepExecutor) secretUpdate(ctx context.Context, params map[string]string) (map[string]string, error) {
	log := logf.FromContext(ctx)
	secretName := params["secretName"]
	key := params["key"]

	value := params["value"]
	if v, ok := params["valueFrom"]; ok && v != "" {
		value = v // already resolved by template engine
	}

	ns := e.resolveNamespace(params)

	var secret corev1.Secret
	err := e.client.Get(ctx, types.NamespacedName{Name: secretName, Namespace: ns}, &secret)
	if apierrors.IsNotFound(err) {
		secret = corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: ns,
			},
			Data: map[string][]byte{
				key: []byte(value),
			},
		}
		if err := e.client.Create(ctx, &secret); err != nil {
			return nil, fmt.Errorf("creating secret %s/%s: %w", ns, secretName, err)
		}
		log.Info("Created secret", "name", secretName, "key", key)
		return map[string]string{"updated": "created"}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting secret %s/%s: %w", ns, secretName, err)
	}

	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}
	secret.Data[key] = []byte(value)
	if err := e.client.Update(ctx, &secret); err != nil {
		return nil, fmt.Errorf("updating secret %s/%s: %w", ns, secretName, err)
	}
	log.Info("Updated secret", "name", secretName, "key", key)
	return map[string]string{"updated": "patched"}, nil
}

func (e *StepExecutor) secretRead(ctx context.Context, params map[string]string) (map[string]string, error) {
	secretName := params["secretName"]
	key := params["key"]
	ns := e.resolveNamespace(params)

	var secret corev1.Secret
	if err := e.client.Get(ctx, types.NamespacedName{Name: secretName, Namespace: ns}, &secret); err != nil {
		return nil, fmt.Errorf("reading secret %s/%s: %w", ns, secretName, err)
	}

	val, ok := secret.Data[key]
	if !ok {
		return nil, fmt.Errorf("key %q not found in secret %s/%s", key, ns, secretName)
	}
	return map[string]string{"value": string(val)}, nil
}

func (e *StepExecutor) rolloutRestart(ctx context.Context, params map[string]string) (map[string]string, error) {
	log := logf.FromContext(ctx)
	kind := params["kind"]
	name := params["name"]
	ns := e.resolveNamespace(params)

	restartAnnotation := map[string]string{
		"kubectl.kubernetes.io/restartedAt": time.Now().UTC().Format(time.RFC3339),
	}

	switch kind {
	case "Deployment":
		var dep appsv1.Deployment
		if err := e.client.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, &dep); err != nil {
			return nil, fmt.Errorf("getting Deployment %s/%s: %w", ns, name, err)
		}
		if dep.Spec.Template.Annotations == nil {
			dep.Spec.Template.Annotations = make(map[string]string)
		}
		for k, v := range restartAnnotation {
			dep.Spec.Template.Annotations[k] = v
		}
		if err := e.client.Update(ctx, &dep); err != nil {
			return nil, fmt.Errorf("restarting Deployment %s/%s: %w", ns, name, err)
		}

	case "StatefulSet":
		var sts appsv1.StatefulSet
		if err := e.client.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, &sts); err != nil {
			return nil, fmt.Errorf("getting StatefulSet %s/%s: %w", ns, name, err)
		}
		if sts.Spec.Template.Annotations == nil {
			sts.Spec.Template.Annotations = make(map[string]string)
		}
		for k, v := range restartAnnotation {
			sts.Spec.Template.Annotations[k] = v
		}
		if err := e.client.Update(ctx, &sts); err != nil {
			return nil, fmt.Errorf("restarting StatefulSet %s/%s: %w", ns, name, err)
		}

	case "DaemonSet":
		var ds appsv1.DaemonSet
		if err := e.client.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, &ds); err != nil {
			return nil, fmt.Errorf("getting DaemonSet %s/%s: %w", ns, name, err)
		}
		if ds.Spec.Template.Annotations == nil {
			ds.Spec.Template.Annotations = make(map[string]string)
		}
		for k, v := range restartAnnotation {
			ds.Spec.Template.Annotations[k] = v
		}
		if err := e.client.Update(ctx, &ds); err != nil {
			return nil, fmt.Errorf("restarting DaemonSet %s/%s: %w", ns, name, err)
		}

	default:
		return nil, fmt.Errorf("unsupported kind for rollout-restart: %q", kind)
	}

	log.Info("Triggered rollout restart", "kind", kind, "name", name, "namespace", ns)
	return map[string]string{"restarted": "true"}, nil
}

func (e *StepExecutor) configmapUpdate(ctx context.Context, params map[string]string) (map[string]string, error) {
	log := logf.FromContext(ctx)
	cmName := params["configMapName"]
	key := params["key"]
	ns := e.resolveNamespace(params)

	value := params["value"]
	if v, ok := params["valueFrom"]; ok && v != "" {
		value = v
	}

	var cm corev1.ConfigMap
	err := e.client.Get(ctx, types.NamespacedName{Name: cmName, Namespace: ns}, &cm)
	if apierrors.IsNotFound(err) {
		cm = corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cmName,
				Namespace: ns,
			},
			Data: map[string]string{
				key: value,
			},
		}
		if err := e.client.Create(ctx, &cm); err != nil {
			return nil, fmt.Errorf("creating ConfigMap %s/%s: %w", ns, cmName, err)
		}
		log.Info("Created ConfigMap", "name", cmName, "key", key)
		return map[string]string{"updated": "created"}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting ConfigMap %s/%s: %w", ns, cmName, err)
	}

	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	cm.Data[key] = value
	if err := e.client.Update(ctx, &cm); err != nil {
		return nil, fmt.Errorf("updating ConfigMap %s/%s: %w", ns, cmName, err)
	}
	log.Info("Updated ConfigMap", "name", cmName, "key", key)
	return map[string]string{"updated": "patched"}, nil
}

func (e *StepExecutor) wait(ctx context.Context, params map[string]string) (map[string]string, error) {
	log := logf.FromContext(ctx)
	durationStr := params["duration"]
	if durationStr == "" {
		return nil, nil
	}

	d, err := time.ParseDuration(durationStr)
	if err != nil {
		return nil, fmt.Errorf("invalid duration %q: %w", durationStr, err)
	}

	timeoutStr := params["timeout"]
	if timeoutStr != "" {
		timeout, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return nil, fmt.Errorf("invalid timeout %q: %w", timeoutStr, err)
		}
		if d > timeout {
			d = timeout
		}
	}

	log.Info("Wait step sleeping", "duration", d)
	select {
	case <-time.After(d):
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (e *StepExecutor) assert(ctx context.Context, params map[string]string) (map[string]string, error) {
	expression := params["expression"]
	message := params["message"]
	if message == "" {
		message = fmt.Sprintf("assertion failed: %s", expression)
	}

	// CEL evaluation — for v1alpha1, support simple equality checks.
	// Full CEL integration is deferred to Phase 3+.
	// TODO: Integrate google/cel-go for full CEL expression evaluation.
	return nil, fmt.Errorf("assert step not yet implemented: expression=%q message=%q", expression, message)
}

func (e *StepExecutor) resolveNamespace(params map[string]string) string {
	if ns, ok := params["namespace"]; ok && ns != "" {
		return ns
	}
	return e.namespace
}
