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
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	genericapiserver "k8s.io/apiserver/pkg/server"
)

// Convoy types have no generated protobuf marshallers, so advertising protobuf makes
// the negotiator pick it for protobuf-preferring clients and then fail with 406
// instead of falling back to JSON.
type withoutProtobuf struct {
	runtime.NegotiatedSerializer
}

func (s withoutProtobuf) SupportedMediaTypes() []runtime.SerializerInfo {
	return slices.DeleteFunc(slices.Clone(s.NegotiatedSerializer.SupportedMediaTypes()), func(si runtime.SerializerInfo) bool {
		return si.MediaType == runtime.ContentTypeProtobuf
	})
}

func newAPIGroupInfo(group string) genericapiserver.APIGroupInfo {
	info := genericapiserver.NewDefaultAPIGroupInfo(group, Scheme, metav1.ParameterCodec, Codecs)
	info.NegotiatedSerializer = withoutProtobuf{info.NegotiatedSerializer}
	return info
}
