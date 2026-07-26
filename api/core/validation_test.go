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

package core

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

const (
	dummyReleaseNamespace = "team-a"
	dummyInfraNamespace   = "infra"
	deploymentKind        = "Deployment"
	unknownValue          = "bogus"
)

// validRelease builds a Release in namespace "team-a" with a git source and
// manifests content — the baseline the same-namespace tests then mutate.
func validRelease() *Release {
	return &Release{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: dummyReleaseNamespace},
		Spec: ReleaseSpec{
			Source: ReleaseSourceSpec{
				Type:          ReleaseSourceTypeGitRepository,
				GitRepository: &ReleaseGitRepositorySpec{Name: "repo"},
			},
			Content: ReleaseContentSpec{
				Type:      ReleaseContentTypeManifests,
				Manifests: &ReleaseContentManifestsSpec{Paths: []string{"."}},
			},
			ClusterRef: ReleaseClusterRefSpec{Name: "prod"},
		},
	}
}

// hasForbiddenOn reports whether errs contains a Forbidden error whose field
// path ends with the given suffix.
func hasForbiddenOn(errs field.ErrorList, pathSuffix string) bool {
	for _, e := range errs {
		if strings.HasSuffix(e.Field, pathSuffix) && strings.Contains(e.Error(), "cross-namespace") {
			return true
		}
	}
	return false
}

func TestValidateRelease_SameNamespaceRefsAllowed(t *testing.T) {
	// Empty ref namespaces (default to the Release's own) are allowed.
	if errs := ValidateRelease(validRelease()); len(errs) != 0 {
		t.Errorf("valid same-namespace Release rejected: %v", errs)
	}

	// Explicit ref namespaces equal to the Release namespace are allowed.
	rel := validRelease()
	rel.Spec.ClusterRef.Namespace = dummyReleaseNamespace
	rel.Spec.Source.GitRepository.Namespace = dummyReleaseNamespace
	if errs := ValidateRelease(rel); len(errs) != 0 {
		t.Errorf("explicit same-namespace refs rejected: %v", errs)
	}
}

func TestValidateRelease_ClusterWaveStrategy(t *testing.T) {
	// A known strategy on spec.syncPolicy.waveStrategy is accepted.
	rel := validRelease()
	rel.Spec.SyncPolicy.WaveStrategy.Strategy = ReleaseWaveStrategyWaitForHealthy
	if errs := ValidateRelease(rel); len(errs) != 0 {
		t.Errorf("known cluster-wave strategy rejected: %v", errs)
	}

	// An unknown strategy is rejected on the syncPolicy.waveStrategy.strategy path.
	rel.Spec.SyncPolicy.WaveStrategy.Strategy = unknownValue
	errs := ValidateRelease(rel)
	found := false
	for _, e := range errs {
		if strings.HasSuffix(e.Field, "syncPolicy.waveStrategy.strategy") {
			found = true
		}
	}
	if !found {
		t.Errorf("unknown cluster-wave strategy not rejected: %v", errs)
	}
}

func TestValidateRelease_CrossNamespaceClusterRefRejected(t *testing.T) {
	rel := validRelease()
	rel.Spec.ClusterRef.Namespace = dummyInfraNamespace
	errs := ValidateRelease(rel)
	if !hasForbiddenOn(errs, "clusterRef.namespace") {
		t.Errorf("cross-namespace clusterRef not rejected: %v", errs)
	}
}

func TestValidateRelease_CrossNamespaceSourceRejected(t *testing.T) {
	rel := validRelease()
	rel.Spec.Source.GitRepository.Namespace = dummyInfraNamespace
	errs := ValidateRelease(rel)
	if !hasForbiddenOn(errs, "source.gitRepository.namespace") {
		t.Errorf("cross-namespace source not rejected: %v", errs)
	}
}

func TestValidateRelease_CrossNamespaceValuesSourceRejected(t *testing.T) {
	rel := validRelease()
	rel.Spec.Content = ReleaseContentSpec{
		Type: ReleaseContentTypeHelm,
		Helm: &ReleaseContentHelmSpec{
			Path: "chart",
			Values: []ValuesSourceSpec{{
				Type: ValuesSourceTypeGitRepository,
				GitRepository: &ValuesSourceGitRepositorySpec{
					ReleaseGitRepositorySpec: &ReleaseGitRepositorySpec{Name: "vals", Namespace: dummyInfraNamespace},
					Paths:                    []string{"values.yaml"},
				},
			}},
		},
	}
	errs := ValidateRelease(rel)
	if !hasForbiddenOn(errs, "gitRepository.namespace") {
		t.Errorf("cross-namespace values source not rejected: %v", errs)
	}
}

func TestValidateReleaseUpdate_CrossNamespaceRejected(t *testing.T) {
	old := validRelease()
	updated := validRelease()
	updated.Spec.Source.GitRepository.Namespace = dummyInfraNamespace
	errs := ValidateReleaseUpdate(updated, old)
	if !hasForbiddenOn(errs, "source.gitRepository.namespace") {
		t.Errorf("cross-namespace ref not rejected on update: %v", errs)
	}
}

func jsonPatchMutation() ReleaseMutation {
	return ReleaseMutation{
		Type:    MutationTypeJsonPatch,
		Target:  ReleaseMutationTarget{Group: "apps", Version: "v1", Kind: deploymentKind},
		Patches: []ReleaseMutationPatchSpec{{Op: JSONPatchOperationReplace, Path: "/spec/replicas"}},
	}
}

func TestValidateReleaseMutations_Valid(t *testing.T) {
	rel := validRelease()
	rel.Mutations = []ReleaseMutation{jsonPatchMutation()}
	if errs := ValidateReleaseMutations(rel); len(errs) != 0 {
		t.Errorf("valid mutations rejected: %v", errs)
	}
}

func TestValidateReleaseMutations_JsonPatchRequiresPatches(t *testing.T) {
	rel := validRelease()
	m := jsonPatchMutation()
	m.Patches = nil
	rel.Mutations = []ReleaseMutation{m}
	errs := ValidateReleaseMutations(rel)
	if len(errs) == 0 {
		t.Error("jsonPatch with no patches not rejected")
	}
}

func TestValidateReleaseMutations_UnknownTypeRejected(t *testing.T) {
	rel := validRelease()
	rel.Mutations = []ReleaseMutation{{Type: unknownValue, Target: ReleaseMutationTarget{Kind: deploymentKind}}}
	errs := ValidateReleaseMutations(rel)
	if len(errs) == 0 {
		t.Error("unknown mutation type not rejected")
	}
}

func TestValidateReleaseMutations_SyncWaveValid(t *testing.T) {
	rel := validRelease()
	rel.Mutations = []ReleaseMutation{{
		Type:         MutationTypeSyncWave,
		Target:       ReleaseMutationTarget{Group: "apps", Version: "v1", Kind: deploymentKind},
		WaveStrategy: &ReleaseWaveStrategySpec{Wave: 1, Strategy: ReleaseWaveStrategyWaitForHealthy},
	}}
	if errs := ValidateReleaseMutations(rel); len(errs) != 0 {
		t.Errorf("valid syncWave rejected: %v", errs)
	}
}

func TestValidateReleaseMutations_SyncWaveRequiresWaveStrategy(t *testing.T) {
	rel := validRelease()
	rel.Mutations = []ReleaseMutation{{Type: MutationTypeSyncWave, Target: ReleaseMutationTarget{Kind: deploymentKind}}}
	if errs := ValidateReleaseMutations(rel); len(errs) == 0 {
		t.Error("syncWave without waveStrategy not rejected")
	}
}

func TestValidateReleaseMutations_SyncWaveUnknownStrategy(t *testing.T) {
	rel := validRelease()
	rel.Mutations = []ReleaseMutation{{
		Type:         MutationTypeSyncWave,
		Target:       ReleaseMutationTarget{Kind: deploymentKind},
		WaveStrategy: &ReleaseWaveStrategySpec{Strategy: unknownValue},
	}}
	if errs := ValidateReleaseMutations(rel); len(errs) == 0 {
		t.Error("syncWave with unknown strategy not rejected")
	}
}

func TestValidateReleaseMutations_CrossNamespaceFromRejected(t *testing.T) {
	rel := validRelease()
	rel.Mutations = []ReleaseMutation{{
		Type:   MutationTypeStrategicMergePatch,
		Target: ReleaseMutationTarget{Kind: deploymentKind},
		From:   &ReleaseMutationFromSpec{Kind: "ConfigMap", Name: "patches", Namespace: dummyInfraNamespace, Path: "patch.yaml"},
	}}
	errs := ValidateReleaseMutations(rel)
	if !hasForbiddenOn(errs, "from.namespace") {
		t.Errorf("cross-namespace mutation From not rejected: %v", errs)
	}
}
