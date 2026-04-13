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

	"k8s.io/apimachinery/pkg/runtime"

	taskrunv1alpha1 "github.com/davidkenelm/taskrun/api/v1alpha1"
)

func makeResolvedStep(name, action string, schemaJSON string, params map[string]string) ResolvedStep {
	var schema *runtime.RawExtension
	if schemaJSON != "" {
		schema = &runtime.RawExtension{Raw: []byte(schemaJSON)}
	}
	return ResolvedStep{
		Step: taskrunv1alpha1.StepSpec{
			Name:   name,
			Action: action,
			Params: params,
		},
		Definition: taskrunv1alpha1.StepDefinitionSpec{
			Schema: schema,
		},
	}
}

func TestValidateStepParams_ValidParams(t *testing.T) {
	schema := `{
		"type": "object",
		"required": ["url", "method"],
		"properties": {
			"url": {"type": "string"},
			"method": {"type": "string"}
		}
	}`

	steps := []ResolvedStep{
		makeResolvedStep("fetch", "http-request", schema, map[string]string{
			"url":    "https://example.com",
			"method": "GET",
		}),
	}

	if err := ValidateStepParams(steps); err != nil {
		t.Fatalf("expected valid params, got error: %v", err)
	}
}

func TestValidateStepParams_MissingRequired(t *testing.T) {
	schema := `{
		"type": "object",
		"required": ["url", "method"],
		"properties": {
			"url": {"type": "string"},
			"method": {"type": "string"}
		}
	}`

	steps := []ResolvedStep{
		makeResolvedStep("fetch", "http-request", schema, map[string]string{
			"url": "https://example.com",
			// method is missing
		}),
	}

	if err := ValidateStepParams(steps); err == nil {
		t.Fatal("expected error for missing required param")
	}
}

func TestValidateStepParams_NoSchema(t *testing.T) {
	steps := []ResolvedStep{
		makeResolvedStep("wait", "wait", "", map[string]string{
			"duration": "30s",
		}),
	}

	if err := ValidateStepParams(steps); err != nil {
		t.Fatalf("expected no error when schema is nil, got: %v", err)
	}
}

func TestValidateStepParams_EmptyParams(t *testing.T) {
	schema := `{
		"type": "object",
		"properties": {
			"url": {"type": "string"}
		}
	}`

	steps := []ResolvedStep{
		makeResolvedStep("step1", "http-request", schema, nil),
	}

	if err := ValidateStepParams(steps); err != nil {
		t.Fatalf("expected no error for empty params with no required fields, got: %v", err)
	}
}

func TestValidateStepParams_InvalidSchemaJSON(t *testing.T) {
	steps := []ResolvedStep{
		makeResolvedStep("bad", "bad-action", `{not valid json}`, map[string]string{"key": "val"}),
	}

	if err := ValidateStepParams(steps); err == nil {
		t.Fatal("expected error for invalid schema JSON")
	}
}

func TestValidateStepParams_MultipleSteps_OneInvalid(t *testing.T) {
	schema := `{
		"type": "object",
		"required": ["key"],
		"properties": {
			"key": {"type": "string"}
		}
	}`

	steps := []ResolvedStep{
		makeResolvedStep("good", "action1", schema, map[string]string{"key": "val"}),
		makeResolvedStep("bad", "action2", schema, map[string]string{}), // missing required
	}

	if err := ValidateStepParams(steps); err == nil {
		t.Fatal("expected error when one step has invalid params")
	}
}
