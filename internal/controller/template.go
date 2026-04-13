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
	"fmt"
	"regexp"
	"strings"

	taskrunv1alpha1 "github.com/davidkenelm/taskrun/api/v1alpha1"
)

// templatePattern matches {{ steps.<stepName>.outputs.<outputName> }} with optional whitespace.
var templatePattern = regexp.MustCompile(`\{\{\s*steps\.([a-z0-9][a-z0-9\-]*[a-z0-9]?)\.outputs\.([a-zA-Z0-9_\-]+)\s*\}\}`)

// ResolveTemplates replaces template expressions in step params with actual output values
// from previously completed steps. Returns a new params map — does not mutate the original.
func ResolveTemplates(params map[string]string, stepStatuses []taskrunv1alpha1.StepStatus) (map[string]string, error) {
	if len(params) == 0 {
		return params, nil
	}

	outputs := buildOutputIndex(stepStatuses)
	resolved := make(map[string]string, len(params))

	for key, val := range params {
		result, err := resolveValue(val, outputs)
		if err != nil {
			return nil, fmt.Errorf("param %q: %w", key, err)
		}
		resolved[key] = result
	}
	return resolved, nil
}

// buildOutputIndex flattens step statuses into a "stepName.outputName" → value map.
func buildOutputIndex(statuses []taskrunv1alpha1.StepStatus) map[string]string {
	idx := make(map[string]string)
	for _, s := range statuses {
		for k, v := range s.Outputs {
			idx[s.Name+"."+k] = v
		}
	}
	return idx
}

func resolveValue(val string, outputs map[string]string) (string, error) {
	var missing []string

	result := templatePattern.ReplaceAllStringFunc(val, func(match string) string {
		parts := templatePattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			missing = append(missing, match)
			return match
		}
		key := parts[1] + "." + parts[2]
		v, ok := outputs[key]
		if !ok {
			missing = append(missing, match)
			return match
		}
		return v
	})

	if len(missing) > 0 {
		return "", fmt.Errorf("unresolved template references: %s", strings.Join(missing, ", "))
	}
	return result, nil
}
