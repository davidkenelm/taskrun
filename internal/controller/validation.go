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

	"github.com/santhosh-tekuri/jsonschema/v6"

	taskrunv1alpha1 "github.com/davidkenelm/taskrun/api/v1alpha1"
)

// ValidateStepParams validates each resolved step's params against its StepDefinition schema.
// Returns a combined error listing all validation failures.
func ValidateStepParams(steps []ResolvedStep) error {
	var errs []string
	for _, rs := range steps {
		if err := validateOneStep(rs); err != nil {
			errs = append(errs, fmt.Sprintf("step %q: %v", rs.Step.Name, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("param validation failed: %v", errs)
	}
	return nil
}

func validateOneStep(rs ResolvedStep) error {
	if rs.Definition.Schema == nil || rs.Definition.Schema.Raw == nil {
		// No schema defined — skip validation.
		return nil
	}

	schema, err := compileSchema(rs.Step.Action, rs.Definition)
	if err != nil {
		return fmt.Errorf("compiling schema: %w", err)
	}

	// Convert map[string]string params to map[string]any for JSON Schema validation.
	params := make(map[string]any, len(rs.Step.Params))
	for k, v := range rs.Step.Params {
		params[k] = v
	}

	if err := schema.Validate(params); err != nil {
		return fmt.Errorf("params invalid: %w", err)
	}
	return nil
}

func compileSchema(action string, def taskrunv1alpha1.StepDefinitionSpec) (*jsonschema.Schema, error) {
	var schemaDoc any
	if err := json.Unmarshal(def.Schema.Raw, &schemaDoc); err != nil {
		return nil, fmt.Errorf("unmarshalling schema JSON for %q: %w", action, err)
	}

	c := jsonschema.NewCompiler()
	if err := c.AddResource(action+".json", schemaDoc); err != nil {
		return nil, fmt.Errorf("adding schema resource: %w", err)
	}
	return c.Compile(action + ".json")
}
