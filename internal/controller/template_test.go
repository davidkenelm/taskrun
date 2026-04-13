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

	taskrunv1alpha1 "github.com/davidkenelm/taskrun/api/v1alpha1"
)

func TestResolveTemplates_SimpleSubstitution(t *testing.T) {
	statuses := []taskrunv1alpha1.StepStatus{
		{
			Name:    "fetch",
			Outputs: map[string]string{"body": "hello-world", "statusCode": "200"},
		},
	}

	params := map[string]string{
		"value": "{{ steps.fetch.outputs.body }}",
	}

	resolved, err := ResolveTemplates(params, statuses)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved["value"] != "hello-world" {
		t.Errorf("expected 'hello-world', got %q", resolved["value"])
	}
}

func TestResolveTemplates_MultipleRefsInOneValue(t *testing.T) {
	statuses := []taskrunv1alpha1.StepStatus{
		{
			Name:    "fetch",
			Outputs: map[string]string{"host": "example.com", "port": "8080"},
		},
	}

	params := map[string]string{
		"url": "https://{{ steps.fetch.outputs.host }}:{{ steps.fetch.outputs.port }}/api",
	}

	resolved, err := ResolveTemplates(params, statuses)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "https://example.com:8080/api"
	if resolved["url"] != expected {
		t.Errorf("expected %q, got %q", expected, resolved["url"])
	}
}

func TestResolveTemplates_WhitespaceVariants(t *testing.T) {
	statuses := []taskrunv1alpha1.StepStatus{
		{Name: "step1", Outputs: map[string]string{"val": "ok"}},
	}

	variants := []string{
		"{{steps.step1.outputs.val}}",
		"{{ steps.step1.outputs.val }}",
		"{{  steps.step1.outputs.val  }}",
	}

	for _, v := range variants {
		params := map[string]string{"key": v}
		resolved, err := ResolveTemplates(params, statuses)
		if err != nil {
			t.Errorf("variant %q: unexpected error: %v", v, err)
			continue
		}
		if resolved["key"] != "ok" {
			t.Errorf("variant %q: expected 'ok', got %q", v, resolved["key"])
		}
	}
}

func TestResolveTemplates_MissingRef(t *testing.T) {
	statuses := []taskrunv1alpha1.StepStatus{
		{Name: "fetch", Outputs: map[string]string{"body": "data"}},
	}

	params := map[string]string{
		"value": "{{ steps.fetch.outputs.nonexistent }}",
	}

	_, err := ResolveTemplates(params, statuses)
	if err == nil {
		t.Fatal("expected error for missing output reference")
	}
}

func TestResolveTemplates_MissingStep(t *testing.T) {
	statuses := []taskrunv1alpha1.StepStatus{}

	params := map[string]string{
		"value": "{{ steps.ghost.outputs.val }}",
	}

	_, err := ResolveTemplates(params, statuses)
	if err == nil {
		t.Fatal("expected error for missing step reference")
	}
}

func TestResolveTemplates_NoTemplates(t *testing.T) {
	params := map[string]string{
		"url":    "https://example.com",
		"method": "GET",
	}

	resolved, err := ResolveTemplates(params, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved["url"] != "https://example.com" {
		t.Errorf("literal value was modified: %q", resolved["url"])
	}
}

func TestResolveTemplates_NilParams(t *testing.T) {
	resolved, err := ResolveTemplates(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved != nil {
		t.Errorf("expected nil, got %v", resolved)
	}
}

func TestResolveTemplates_MultipleSteps(t *testing.T) {
	statuses := []taskrunv1alpha1.StepStatus{
		{Name: "step-a", Outputs: map[string]string{"token": "abc123"}},
		{Name: "step-b", Outputs: map[string]string{"url": "https://api.example.com"}},
	}

	params := map[string]string{
		"authorization": "Bearer {{ steps.step-a.outputs.token }}",
		"endpoint":      "{{ steps.step-b.outputs.url }}/v1/data",
	}

	resolved, err := ResolveTemplates(params, statuses)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved["authorization"] != "Bearer abc123" {
		t.Errorf("unexpected authorization: %q", resolved["authorization"])
	}
	if resolved["endpoint"] != "https://api.example.com/v1/data" {
		t.Errorf("unexpected endpoint: %q", resolved["endpoint"])
	}
}
