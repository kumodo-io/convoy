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

package apiserver

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/endpoints/handlers/negotiation"

	"github.com/kumodo-io/convoy/api/source"
	sourcev1alpha1 "github.com/kumodo-io/convoy/api/source/v1alpha1"
)

func TestAPIGroupInfo_DoesNotAdvertiseProtobuf(t *testing.T) {
	info := newAPIGroupInfo(source.GroupName)
	for _, si := range info.NegotiatedSerializer.SupportedMediaTypes() {
		if si.MediaType == runtime.ContentTypeProtobuf {
			t.Fatalf("group serializer advertises %s", runtime.ContentTypeProtobuf)
		}
	}
}

func TestAPIGroupInfo_ProtobufPreferringClientGetsJSON(t *testing.T) {
	info := newAPIGroupInfo(source.GroupName)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "application/vnd.kubernetes.protobuf,application/json")

	mt, si, err := negotiation.NegotiateOutputMediaType(req, info.NegotiatedSerializer, negotiation.DefaultEndpointRestrictions)
	if err != nil {
		t.Fatalf("negotiate: %v", err)
	}
	if mt.Accepted.MediaType != runtime.ContentTypeJSON {
		t.Fatalf("negotiated %s, want %s", mt.Accepted.MediaType, runtime.ContentTypeJSON)
	}

	list := &sourcev1alpha1.HelmRegistryList{Items: []sourcev1alpha1.HelmRegistry{{}}}
	list.SetGroupVersionKind(sourcev1alpha1.SchemeGroupVersion.WithKind("HelmRegistryList"))
	if err := si.Serializer.Encode(list, &bytes.Buffer{}); err != nil {
		t.Fatalf("encode: %v", err)
	}
}
