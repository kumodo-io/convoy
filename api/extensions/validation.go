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

package extensions

import (
	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// ---------- GlobalHealthRuleSet (cluster-scoped) ----------

// ValidateGlobalHealthRuleSet validates a GlobalHealthRuleSet on creation.
func ValidateGlobalHealthRuleSet(rule *GlobalHealthRuleSet) field.ErrorList {
	allErrs := apivalidation.ValidateObjectMeta(&rule.ObjectMeta, false, apivalidation.NameIsDNSSubdomain, field.NewPath("metadata"))
	for _, item := range rule.Spec.Rules {
		allErrs = append(allErrs, validateHealthRule(item, field.NewPath("spec", "rules"))...)
	}
	return allErrs
}

// ValidateGlobalHealthRuleSetUpdate validates a GlobalHealthRuleSet spec/metadata update.
func ValidateGlobalHealthRuleSetUpdate(rule, old *GlobalHealthRuleSet) field.ErrorList {
	allErrs := apivalidation.ValidateObjectMetaUpdate(&rule.ObjectMeta, &old.ObjectMeta, field.NewPath("metadata"))
	for _, item := range rule.Spec.Rules {
		allErrs = append(allErrs, validateHealthRule(item, field.NewPath("spec", "rules"))...)
	}
	return allErrs
}

// ---------- HealthRuleSet (namespaced) ----------

// ValidateHealthRuleSet validates a HealthRuleSet on creation.
func ValidateHealthRuleSet(rule *HealthRuleSet) field.ErrorList {
	allErrs := apivalidation.ValidateObjectMeta(&rule.ObjectMeta, true, apivalidation.NameIsDNSSubdomain, field.NewPath("metadata"))
	for _, item := range rule.Spec.Rules {
		allErrs = append(allErrs, validateHealthRule(item, field.NewPath("spec", "rules"))...)
	}
	return allErrs
}

// ValidateHealthRuleSetUpdate validates a HealthRuleSet spec/metadata update.
func ValidateHealthRuleSetUpdate(rule, old *HealthRuleSet) field.ErrorList {
	allErrs := apivalidation.ValidateObjectMetaUpdate(&rule.ObjectMeta, &old.ObjectMeta, field.NewPath("metadata"))
	for _, item := range rule.Spec.Rules {
		allErrs = append(allErrs, validateHealthRule(item, field.NewPath("spec", "rules"))...)
	}
	return allErrs
}

func validateHealthRule(rule RuleSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if rule.ApiVersion == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("apiVersion"), "api version required"))
	}
	if rule.RuleExpression == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("ruleExpression"), "a CEL health expression is required"))
	}
	return allErrs
}
