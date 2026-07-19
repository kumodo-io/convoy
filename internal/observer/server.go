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

package observer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"

	"github.com/kumodo-io/convoy/internal/healthresult"
)

// Serve runs the observer's HTTP server until ctx is cancelled. The apiserver
// proxies the Release `/preview` (and, later, `/diff`) subresources to it — this
// is the on-demand counterpart to the periodic drift loop, sharing the same
// render path. There is no inter-component auth yet (in-cluster, cluster-local).
func (c *Controller) Serve(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	// Render-only preview: "what would be applied for this Release right now".
	mux.HandleFunc("GET /preview/{namespace}/{name}", c.handlePreview)
	// Render + compare-to-live: the structured per-object drift.
	mux.HandleFunc("GET /diff/{namespace}/{name}", c.handleDiff)
	// Per-object health from the health loop's store.
	mux.HandleFunc("GET /health/{namespace}/{name}", c.handleHealth)

	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		<-ctx.Done()
		// Best-effort graceful shutdown on cancellation.
		_ = srv.Shutdown(context.Background())
	}()

	klog.InfoS("observer HTTP server listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// handlePreview renders the desired objects for a Release and writes them as a
// multi-document YAML stream. It reports 404 for a missing Release and 422 for a
// render/fetch failure (an expected, often self-resolving condition), so the
// proxying apiserver surfaces a meaningful status rather than a 500.
func (c *Controller) handlePreview(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	name := r.PathValue("name")

	rel, err := c.releaseLister.Releases(namespace).Get(name)
	if apierrors.IsNotFound(err) {
		http.Error(w, fmt.Sprintf("release %s/%s not found", namespace, name), http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	objects, err := c.renderDesired(r.Context(), rel)
	if err != nil {
		// Terminal config problems and transient fetch failures both mean "can't
		// render this right now" — a 422 the caller can read, not a server fault.
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	out, err := marshalYAMLStream(objects)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(out)
}

// handleDiff computes the structured per-object drift for a Release and writes
// it as JSON. 404 for a missing Release; 422 when the diff can't be produced
// (source/cluster absent or unreachable) — an expected, often self-resolving
// condition the caller can read, not a server fault.
func (c *Controller) handleDiff(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	name := r.PathValue("name")

	rel, err := c.releaseLister.Releases(namespace).Get(name)
	if apierrors.IsNotFound(err) {
		http.Error(w, fmt.Sprintf("release %s/%s not found", namespace, name), http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	result, err := c.diff(r.Context(), rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	out, err := json.Marshal(result)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

// handleHealth returns the last assessed per-object health for a Release as JSON,
// from the health loop's in-mem store. A Release that exists but has not been
// assessed yet returns an empty result (200); a missing Release returns 404.
func (c *Controller) handleHealth(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	name := r.PathValue("name")

	if _, err := c.releaseLister.Releases(namespace).Get(name); apierrors.IsNotFound(err) {
		http.Error(w, fmt.Sprintf("release %s/%s not found", namespace, name), http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	result := &healthresult.Result{Objects: []healthresult.ObjectHealth{}}
	if c.healthResults != nil {
		if stored := c.healthResults(namespace + "/" + name); stored != nil {
			result = stored
		}
	}

	out, err := json.Marshal(result)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

// marshalYAMLStream renders objects as a multi-document YAML stream (documents
// separated by `---`), in the deterministic order render produced them.
func marshalYAMLStream(objects []*unstructured.Unstructured) ([]byte, error) {
	var buf bytes.Buffer
	for i, obj := range objects {
		b, err := yaml.Marshal(obj.Object)
		if err != nil {
			return nil, fmt.Errorf("marshaling %s %q: %w", obj.GetKind(), obj.GetName(), err)
		}
		if i > 0 {
			buf.WriteString("---\n")
		}
		buf.Write(b)
	}
	return buf.Bytes(), nil
}
