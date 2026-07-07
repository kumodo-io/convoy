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
	"errors"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

// fakeClient records the calls the command logic makes.
type fakeClient struct {
	rel *corev1alpha1.Release

	getErr error
	pinErr error

	pinnedRef string
	pinCalled bool
	unpinned  bool
	synced    bool
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
