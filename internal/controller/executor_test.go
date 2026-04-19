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
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestExecutor_SecretUpdate_Create(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	executor := NewStepExecutor(c, "default")

	outputs, err := executor.Execute(context.Background(), "secret-update", map[string]string{
		"secretName": "my-secret",
		"key":        "token",
		"value":      "abc123",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outputs["updated"] != "created" {
		t.Errorf("expected 'created', got %q", outputs["updated"])
	}

	// Verify secret was created.
	var secret corev1.Secret
	if err := c.Get(context.Background(), types.NamespacedName{Name: "my-secret", Namespace: "default"}, &secret); err != nil {
		t.Fatalf("secret not found: %v", err)
	}
	if string(secret.Data["token"]) != "abc123" {
		t.Errorf("unexpected secret value: %q", string(secret.Data["token"]))
	}
}

func TestExecutor_SecretUpdate_Patch(t *testing.T) {
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-secret", Namespace: "default"},
		Data:       map[string][]byte{"existing": []byte("val")},
	}
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(existing).Build()
	executor := NewStepExecutor(c, "default")

	outputs, err := executor.Execute(context.Background(), "secret-update", map[string]string{
		"secretName": "my-secret",
		"key":        "newkey",
		"value":      "newval",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outputs["updated"] != "patched" {
		t.Errorf("expected 'patched', got %q", outputs["updated"])
	}

	var secret corev1.Secret
	_ = c.Get(context.Background(), types.NamespacedName{Name: "my-secret", Namespace: "default"}, &secret)
	if string(secret.Data["newkey"]) != "newval" {
		t.Errorf("new key not set: %v", secret.Data)
	}
	if string(secret.Data["existing"]) != "val" {
		t.Errorf("existing key was lost: %v", secret.Data)
	}
}

func TestExecutor_SecretRead(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: "default"},
		Data:       map[string][]byte{"password": []byte("s3cret")},
	}
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(secret).Build()
	executor := NewStepExecutor(c, "default")

	outputs, err := executor.Execute(context.Background(), "secret-read", map[string]string{
		"secretName": "creds",
		"key":        "password",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outputs["value"] != "s3cret" {
		t.Errorf("expected 's3cret', got %q", outputs["value"])
	}
}

func TestExecutor_SecretRead_MissingKey(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: "default"},
		Data:       map[string][]byte{"password": []byte("s3cret")},
	}
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(secret).Build()
	executor := NewStepExecutor(c, "default")

	_, err := executor.Execute(context.Background(), "secret-read", map[string]string{
		"secretName": "creds",
		"key":        "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestExecutor_SecretRead_MissingSecret(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	executor := NewStepExecutor(c, "default")

	_, err := executor.Execute(context.Background(), "secret-read", map[string]string{
		"secretName": "ghost",
		"key":        "val",
	})
	if err == nil {
		t.Fatal("expected error for missing secret")
	}
}

func TestExecutor_RolloutRestart_Deployment(t *testing.T) {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "my-app", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "my-app"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "my-app"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "img:v1"}}},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dep).Build()
	executor := NewStepExecutor(c, "default")

	outputs, err := executor.Execute(context.Background(), "rollout-restart", map[string]string{
		"kind": "Deployment",
		"name": "my-app",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outputs["restarted"] != "true" {
		t.Errorf("expected restarted=true, got %q", outputs["restarted"])
	}

	// Verify annotation was set.
	var updated appsv1.Deployment
	_ = c.Get(context.Background(), types.NamespacedName{Name: "my-app", Namespace: "default"}, &updated)
	if _, ok := updated.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"]; !ok {
		t.Error("restart annotation not set")
	}
}

func TestExecutor_RolloutRestart_UnsupportedKind(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	executor := NewStepExecutor(c, "default")

	_, err := executor.Execute(context.Background(), "rollout-restart", map[string]string{
		"kind": "CronJob",
		"name": "something",
	})
	if err == nil {
		t.Fatal("expected error for unsupported kind")
	}
}

func TestExecutor_ConfigMapUpdate_Create(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	executor := NewStepExecutor(c, "default")

	outputs, err := executor.Execute(context.Background(), "configmap-update", map[string]string{
		"configMapName": "my-config",
		"key":           "setting",
		"value":         "enabled",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outputs["updated"] != "created" {
		t.Errorf("expected 'created', got %q", outputs["updated"])
	}

	var cm corev1.ConfigMap
	_ = c.Get(context.Background(), types.NamespacedName{Name: "my-config", Namespace: "default"}, &cm)
	if cm.Data["setting"] != "enabled" {
		t.Errorf("unexpected configmap value: %q", cm.Data["setting"])
	}
}

func TestExecutor_Wait(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	executor := NewStepExecutor(c, "default")

	outputs, err := executor.Execute(context.Background(), "wait", map[string]string{
		"duration": "1ms",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outputs != nil {
		t.Errorf("expected nil outputs from wait, got %v", outputs)
	}
}

func TestExecutor_Wait_InvalidDuration(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	executor := NewStepExecutor(c, "default")

	_, err := executor.Execute(context.Background(), "wait", map[string]string{
		"duration": "not-a-duration",
	})
	if err == nil {
		t.Fatal("expected error for invalid duration")
	}
}

func TestExecutor_Wait_ContextCancelled(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	executor := NewStepExecutor(c, "default")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, err := executor.Execute(ctx, "wait", map[string]string{
		"duration": "10s",
	})
	if err == nil {
		t.Fatal("expected error when context is cancelled")
	}
}

func TestExecutor_UnknownAction(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	executor := NewStepExecutor(c, "default")

	_, err := executor.Execute(context.Background(), "unknown-action", map[string]string{})
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
}

func TestExecutor_RolloutRestart_StatefulSet(t *testing.T) {
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "my-db", Namespace: "default"},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "my-db"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "my-db"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "db", Image: "postgres:16"}}},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(sts).Build()
	executor := NewStepExecutor(c, "default")

	outputs, err := executor.Execute(context.Background(), "rollout-restart", map[string]string{
		"kind": "StatefulSet",
		"name": "my-db",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outputs["restarted"] != "true" {
		t.Errorf("expected restarted=true, got %q", outputs["restarted"])
	}

	var updated appsv1.StatefulSet
	_ = c.Get(context.Background(), types.NamespacedName{Name: "my-db", Namespace: "default"}, &updated)
	if _, ok := updated.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"]; !ok {
		t.Error("restart annotation not set on StatefulSet")
	}
}

func TestExecutor_RolloutRestart_DaemonSet(t *testing.T) {
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "my-agent", Namespace: "default"},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "agent"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "agent"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "agent", Image: "agent:v1"}}},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(ds).Build()
	executor := NewStepExecutor(c, "default")

	outputs, err := executor.Execute(context.Background(), "rollout-restart", map[string]string{
		"kind": "DaemonSet",
		"name": "my-agent",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outputs["restarted"] != "true" {
		t.Errorf("expected restarted=true, got %q", outputs["restarted"])
	}

	var updated appsv1.DaemonSet
	_ = c.Get(context.Background(), types.NamespacedName{Name: "my-agent", Namespace: "default"}, &updated)
	if _, ok := updated.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"]; !ok {
		t.Error("restart annotation not set on DaemonSet")
	}
}

func TestExecutor_RolloutRestart_NotFound(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	executor := NewStepExecutor(c, "default")

	_, err := executor.Execute(context.Background(), "rollout-restart", map[string]string{
		"kind": "Deployment",
		"name": "ghost",
	})
	if err == nil {
		t.Fatal("expected error for non-existent Deployment")
	}
}

func TestExecutor_ConfigMapUpdate_Patch(t *testing.T) {
	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "my-config", Namespace: "default"},
		Data:       map[string]string{"existing-key": "existing-val"},
	}
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(existing).Build()
	executor := NewStepExecutor(c, "default")

	outputs, err := executor.Execute(context.Background(), "configmap-update", map[string]string{
		"configMapName": "my-config",
		"key":           "new-key",
		"value":         "new-val",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outputs["updated"] != "patched" {
		t.Errorf("expected 'patched', got %q", outputs["updated"])
	}

	var cm corev1.ConfigMap
	_ = c.Get(context.Background(), types.NamespacedName{Name: "my-config", Namespace: "default"}, &cm)
	if cm.Data["new-key"] != "new-val" {
		t.Errorf("new key not set: %v", cm.Data)
	}
	if cm.Data["existing-key"] != "existing-val" {
		t.Errorf("existing key was lost: %v", cm.Data)
	}
}

func TestExecutor_NamespaceOverride(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "cross-ns", Namespace: "other"},
		Data:       map[string][]byte{"key": []byte("val")},
	}
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(secret).Build()
	executor := NewStepExecutor(c, "default")

	outputs, err := executor.Execute(context.Background(), "secret-read", map[string]string{
		"secretName": "cross-ns",
		"key":        "key",
		"namespace":  "other",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outputs["value"] != "val" {
		t.Errorf("expected 'val', got %q", outputs["value"])
	}
}
