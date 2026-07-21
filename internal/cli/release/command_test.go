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

package release

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/diffresult"
	"github.com/kumodo-io/convoy/internal/healthresult"
)

const ConfigMapKind = "ConfigMap"

// fakeClient records the calls the command logic makes.
type fakeClient struct {
	rel *corev1alpha1.Release

	getErr error
	pinErr error

	pinnedRef      string
	pinCalled      bool
	unpinned       bool
	synced         bool
	previewCalled  bool
	previewPayload []byte
	previewErr     error
	diffCalled     bool
	diffPayload    []byte
	diffErr        error
	healthCalled   bool
	healthPayload  []byte
	healthErr      error
	setMutations   []corev1alpha1.ReleaseMutation
}

func (f *fakeClient) GetRelease(_ context.Context, _, _ string) (*corev1alpha1.Release, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.rel, nil
}

func (f *fakeClient) Pin(_ context.Context, _, _, renderRef string) (*corev1alpha1.Release, error) {
	f.pinCalled = true
	f.pinnedRef = renderRef
	if f.pinErr != nil {
		return nil, f.pinErr
	}
	r := f.rel.DeepCopy()
	r.Pin = &corev1alpha1.ReleasePin{RenderRef: renderRef}
	return r, nil
}

func (f *fakeClient) Unpin(_ context.Context, _, _ string) (*corev1alpha1.Release, error) {
	f.unpinned = true
	r := f.rel.DeepCopy()
	r.Pin = nil
	return r, nil
}

func (f *fakeClient) Sync(_ context.Context, _, _ string) (*corev1alpha1.Release, error) {
	f.synced = true
	return f.rel, nil
}

func (f *fakeClient) Preview(_ context.Context, _, _ string) ([]byte, error) {
	f.previewCalled = true
	if f.previewErr != nil {
		return nil, f.previewErr
	}
	return f.previewPayload, nil
}

func (f *fakeClient) Diff(_ context.Context, _, _ string) ([]byte, error) {
	f.diffCalled = true
	if f.diffErr != nil {
		return nil, f.diffErr
	}
	return f.diffPayload, nil
}

func (f *fakeClient) Health(_ context.Context, _, _ string) ([]byte, error) {
	f.healthCalled = true
	if f.healthErr != nil {
		return nil, f.healthErr
	}
	return f.healthPayload, nil
}

func (f *fakeClient) SetMutations(_ context.Context, _, _ string, mutations []corev1alpha1.ReleaseMutation) (*corev1alpha1.Release, error) {
	f.setMutations = mutations
	r := f.rel.DeepCopy()
	r.Mutations = mutations
	return r, nil
}

func releaseWithRender(renderRef string) *corev1alpha1.Release {
	rel := &corev1alpha1.Release{
		ObjectMeta: metav1.ObjectMeta{Name: "r1", Namespace: "ns"},
	}
	if renderRef != "" {
		rel.Status.LastSuccessful = &corev1alpha1.ReleaseAttemptedRenderStatus{RenderRef: renderRef}
	}
	return rel
}

func TestRunPin_DefaultsToCurrentRender(t *testing.T) {
	f := &fakeClient{rel: releaseWithRender("sha256:cur")}
	var out bytes.Buffer

	if err := runPin(context.Background(), &out, f, "ns", "r1", ""); err != nil {
		t.Fatalf("runPin returned error: %v", err)
	}
	if !f.pinCalled || f.pinnedRef != "sha256:cur" {
		t.Errorf("pinned ref = %q (called=%v), want sha256:cur", f.pinnedRef, f.pinCalled)
	}
	if !strings.Contains(out.String(), "sha256:cur") {
		t.Errorf("output = %q, want it to mention the render ref", out.String())
	}
}

func TestRunPin_ExplicitRefSkipsDefault(t *testing.T) {
	f := &fakeClient{rel: releaseWithRender("sha256:cur")}
	var out bytes.Buffer

	if err := runPin(context.Background(), &out, f, "ns", "r1", "sha256:explicit"); err != nil {
		t.Fatalf("runPin returned error: %v", err)
	}
	if f.pinnedRef != "sha256:explicit" {
		t.Errorf("pinned ref = %q, want sha256:explicit", f.pinnedRef)
	}
}

func TestRunPin_NoSuccessfulRenderErrors(t *testing.T) {
	f := &fakeClient{rel: releaseWithRender("")} // no LastSuccessful
	var out bytes.Buffer

	err := runPin(context.Background(), &out, f, "ns", "r1", "")
	if err == nil {
		t.Fatal("runPin returned nil, want an error when there is no render to freeze")
	}
	if f.pinCalled {
		t.Error("Pin was called despite there being no render to default to")
	}
}

func TestRunPin_GetErrorPropagates(t *testing.T) {
	f := &fakeClient{rel: releaseWithRender("sha256:cur"), getErr: errors.New("boom")}
	var out bytes.Buffer

	if err := runPin(context.Background(), &out, f, "ns", "r1", ""); err == nil {
		t.Fatal("runPin returned nil, want the GetRelease error propagated")
	}
}

func TestRunUnpin(t *testing.T) {
	f := &fakeClient{rel: releaseWithRender("sha256:cur")}
	var out bytes.Buffer

	if err := runUnpin(context.Background(), &out, f, "ns", "r1"); err != nil {
		t.Fatalf("runUnpin returned error: %v", err)
	}
	if !f.unpinned {
		t.Error("Unpin was not called")
	}
	if !strings.Contains(out.String(), "unpinned") {
		t.Errorf("output = %q, want it to confirm the unpin", out.String())
	}
}

func TestRunSync(t *testing.T) {
	f := &fakeClient{rel: releaseWithRender("sha256:cur")}
	var out bytes.Buffer

	if err := runSync(context.Background(), &out, f, "ns", "r1"); err != nil {
		t.Fatalf("runSync returned error: %v", err)
	}
	if !f.synced {
		t.Error("Sync was not called")
	}
	if !strings.Contains(out.String(), "requested sync") {
		t.Errorf("output = %q, want it to confirm the sync request", out.String())
	}
}

func TestRunPreview(t *testing.T) {
	manifests := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm\n"
	f := &fakeClient{rel: releaseWithRender("sha256:cur"), previewPayload: []byte(manifests)}
	var out bytes.Buffer

	if err := runPreview(context.Background(), &out, f, "ns", "r1"); err != nil {
		t.Fatalf("runPreview returned error: %v", err)
	}
	if !f.previewCalled {
		t.Error("Preview was not called")
	}
	if out.String() != manifests {
		t.Errorf("output = %q, want the raw manifests %q", out.String(), manifests)
	}
}

func TestRunPreview_ErrorPropagates(t *testing.T) {
	f := &fakeClient{rel: releaseWithRender("sha256:cur"), previewErr: errors.New("observer unavailable")}
	var out bytes.Buffer

	if err := runPreview(context.Background(), &out, f, "ns", "r1"); err == nil {
		t.Fatal("runPreview returned nil, want the preview error propagated")
	}
}

func diffPayload(t *testing.T, r diffresult.Result) []byte {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshaling diff payload: %v", err)
	}
	return b
}

func TestRunDiff_HumanReadable(t *testing.T) {
	result := diffresult.Result{Objects: []diffresult.ObjectDiff{
		{Kind: ConfigMapKind, Namespace: "ns", Name: "ok", Status: diffresult.StatusInSync},
		{Kind: "Deployment", Namespace: "ns", Name: "app", Status: diffresult.StatusModified, Changes: []diffresult.FieldChange{
			{Path: "/spec/replicas", Op: "replace", Live: float64(1), Desired: float64(3)},
			{Path: "/metadata/labels/new", Op: "add", Desired: "x"},
		}},
		{Kind: "Service", Namespace: "ns", Name: "gone", Status: diffresult.StatusMissing},
	}}
	f := &fakeClient{rel: releaseWithRender("sha256:cur"), diffPayload: diffPayload(t, result)}
	var out bytes.Buffer

	if err := runDiff(context.Background(), &out, f, "ns", "r1", ""); err != nil {
		t.Fatalf("runDiff returned error: %v", err)
	}
	s := out.String()
	// Modified + Missing shown; InSync omitted; replace renders live -> desired.
	if !strings.Contains(s, "Deployment ns/app: Modified") || !strings.Contains(s, "/spec/replicas: 1 -> 3") {
		t.Errorf("output missing modified detail:\n%s", s)
	}
	if !strings.Contains(s, "+ /metadata/labels/new: x") {
		t.Errorf("output missing added field:\n%s", s)
	}
	if !strings.Contains(s, "Service ns/gone: Missing") {
		t.Errorf("output missing the Missing object:\n%s", s)
	}
	if strings.Contains(s, "ok") {
		t.Errorf("in-sync object should be omitted from human output:\n%s", s)
	}
}

func TestRunDiff_InSync(t *testing.T) {
	result := diffresult.Result{Objects: []diffresult.ObjectDiff{
		{Kind: ConfigMapKind, Namespace: "ns", Name: "ok", Status: diffresult.StatusInSync},
	}}
	f := &fakeClient{rel: releaseWithRender("sha256:cur"), diffPayload: diffPayload(t, result)}
	var out bytes.Buffer

	if err := runDiff(context.Background(), &out, f, "ns", "r1", ""); err != nil {
		t.Fatalf("runDiff returned error: %v", err)
	}
	if !strings.Contains(out.String(), "in sync") {
		t.Errorf("output = %q, want it to report in sync", out.String())
	}
}

func TestRunHealth_HumanReadable(t *testing.T) {
	result := healthresult.Result{Objects: []healthresult.ObjectHealth{
		{Kind: "Deployment", Namespace: "ns", Name: "app", State: healthresult.StateDegraded, Message: "0/3 available"},
		{Kind: "Service", Namespace: "ns", Name: "svc", State: healthresult.StateHealthy},
	}}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	f := &fakeClient{rel: releaseWithRender("sha256:cur"), healthPayload: raw}
	var out bytes.Buffer

	if err := runHealth(context.Background(), &out, f, "ns", "r1", ""); err != nil {
		t.Fatalf("runHealth: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "Deployment ns/app: Degraded  (0/3 available)") {
		t.Errorf("output missing degraded detail:\n%s", s)
	}
	if !strings.Contains(s, "Service ns/svc: Healthy") {
		t.Errorf("output missing healthy line:\n%s", s)
	}
}

func TestRunHealth_NotAssessed(t *testing.T) {
	raw, _ := json.Marshal(healthresult.Result{Objects: []healthresult.ObjectHealth{}})
	f := &fakeClient{rel: releaseWithRender("sha256:cur"), healthPayload: raw}
	var out bytes.Buffer

	if err := runHealth(context.Background(), &out, f, "ns", "r1", ""); err != nil {
		t.Fatalf("runHealth: %v", err)
	}
	if !strings.Contains(out.String(), "not assessed") {
		t.Errorf("output = %q, want 'not assessed'", out.String())
	}
}

func TestRunDiff_JSONPassthrough(t *testing.T) {
	raw := diffPayload(t, diffresult.Result{Objects: []diffresult.ObjectDiff{
		{Kind: ConfigMapKind, Namespace: "ns", Name: "ok", Status: diffresult.StatusInSync},
	}})
	f := &fakeClient{rel: releaseWithRender("sha256:cur"), diffPayload: raw}
	var out bytes.Buffer

	if err := runDiff(context.Background(), &out, f, "ns", "r1", "json"); err != nil {
		t.Fatalf("runDiff returned error: %v", err)
	}
	if out.String() != string(raw) {
		t.Errorf("json output = %q, want the raw payload %q", out.String(), raw)
	}
}
