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

package health

import (
	"slices"

	extensionsv1alpha1 "github.com/kumodo-io/convoy/api/extensions/v1alpha1"
)

// ruleApplies reports whether a rule targets an object of the given API version
// and resource set. A rule matches when its ApiVersion equals the object's
// apiVersion and at least one of the object's resources is listed in the rule.
// Matching by resource (not name) is what makes health assessment survive
// rendering-time renaming (Helm/kustomize/mutations).
func ruleApplies(rule extensionsv1alpha1.RuleSpec, apiVersion string, resources []string) bool {
	if rule.ApiVersion != apiVersion {
		return false
	}
	for _, r := range rule.Resources {
		if slices.Contains(resources, r) {
			return true
		}
	}
	return false
}
