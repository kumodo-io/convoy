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

// ---------- KindHealthRule (cluster-scoped) ----------

// ValidateKindHealthRule validates a KindHealthRule on creation.
func ValidateKindHealthRule(rule *KindHealthRule) field.ErrorList {
	allErrs := apivalidation.ValidateObjectMeta(&rule.ObjectMeta, false, apivalidation.NameIsDNSSubdomain, field.NewPath("metadata"))
	allErrs = append(allErrs, validateKindTarget(&rule.Spec.Target, field.NewPath("spec", "target"))...)
	allErrs = append(allErrs, validateHealthRule(&rule.Spec.HealthRule, field.NewPath("spec"))...)
	return allErrs
}

// ValidateKindHealthRuleUpdate validates a KindHealthRule spec/metadata update.
func ValidateKindHealthRuleUpdate(rule, old *KindHealthRule) field.ErrorList {
	allErrs := apivalidation.ValidateObjectMetaUpdate(&rule.ObjectMeta, &old.ObjectMeta, field.NewPath("metadata"))
	allErrs = append(allErrs, validateKindTarget(&rule.Spec.Target, field.NewPath("spec", "target"))...)
	allErrs = append(allErrs, validateHealthRule(&rule.Spec.HealthRule, field.NewPath("spec"))...)
	return allErrs
}

func validateKindTarget(target *KindTarget, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	// Group may be empty (the core group).
	if target.Kind == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("kind"), ""))
	}
	if target.Version == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("version"), ""))
	}
	return allErrs
}

// ---------- ResourceHealthRule (namespaced) ----------

// ValidateResourceHealthRule validates a ResourceHealthRule on creation.
func ValidateResourceHealthRule(rule *ResourceHealthRule) field.ErrorList {
	allErrs := apivalidation.ValidateObjectMeta(&rule.ObjectMeta, true, apivalidation.NameIsDNSSubdomain, field.NewPath("metadata"))
	allErrs = append(allErrs, validateResourceTarget(&rule.Spec.Target, field.NewPath("spec", "target"))...)
	allErrs = append(allErrs, validateHealthRule(&rule.Spec.HealthRule, field.NewPath("spec"))...)
	return allErrs
}

// ValidateResourceHealthRuleUpdate validates a ResourceHealthRule spec/metadata update.
func ValidateResourceHealthRuleUpdate(rule, old *ResourceHealthRule) field.ErrorList {
	allErrs := apivalidation.ValidateObjectMetaUpdate(&rule.ObjectMeta, &old.ObjectMeta, field.NewPath("metadata"))
	allErrs = append(allErrs, validateResourceTarget(&rule.Spec.Target, field.NewPath("spec", "target"))...)
	allErrs = append(allErrs, validateHealthRule(&rule.Spec.HealthRule, field.NewPath("spec"))...)
	return allErrs
}

func validateResourceTarget(target *ResourceTarget, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	// Group may be empty (the core group); Namespace may be empty for cluster-scoped targets.
	if target.Kind == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("kind"), ""))
	}
	if target.Version == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("version"), ""))
	}
	if target.Name == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("name"), ""))
	}
	return allErrs
}

func validateHealthRule(rule *HealthRule, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if rule.Expression == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("expression"), "a CEL health expression is required"))
	}
	return allErrs
}
