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

// Package apiserver assembles the convoy aggregated API server.
package apiserver

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apiserver/pkg/registry/rest"
	genericapiserver "k8s.io/apiserver/pkg/server"

	"github.com/kumodo-io/convoy/api/core"
	coreinstall "github.com/kumodo-io/convoy/api/core/install"
	clusterstorage "github.com/kumodo-io/convoy/internal/registry/core/cluster"
)

var (
	// Scheme is the runtime scheme with all served API groups installed.
	Scheme = runtime.NewScheme()
	// Codecs provides serialization for the served types.
	Codecs = serializer.NewCodecFactory(Scheme)
)

func init() {
	coreinstall.Install(Scheme)

	// Expose metav1 types (ListOptions, DeleteOptions, etc.) and register the
	// standard unversioned types so discovery and request decoding work.
	metav1.AddToGroupVersion(Scheme, schema.GroupVersion{Version: "v1"})
	unversioned := schema.GroupVersion{Group: "", Version: "v1"}
	Scheme.AddUnversionedTypes(unversioned,
		&metav1.Status{},
		&metav1.APIVersions{},
		&metav1.APIGroupList{},
		&metav1.APIGroup{},
		&metav1.APIResourceList{},
	)
}

// ExtraConfig holds convoy-specific apiserver configuration.
type ExtraConfig struct{}

// Config wraps the generic recommended config plus convoy extras.
type Config struct {
	GenericConfig *genericapiserver.RecommendedConfig
	ExtraConfig   ExtraConfig
}

// ConvoyServer is the convoy aggregated API server.
type ConvoyServer struct {
	GenericAPIServer *genericapiserver.GenericAPIServer
}

type completedConfig struct {
	GenericConfig genericapiserver.CompletedConfig
	ExtraConfig   *ExtraConfig
}

// CompletedConfig is a fully-initialized Config ready to build a server.
type CompletedConfig struct {
	*completedConfig
}

// Complete fills in defaults and returns a CompletedConfig.
func (c *Config) Complete() CompletedConfig {
	completed := completedConfig{
		c.GenericConfig.Complete(),
		&c.ExtraConfig,
	}
	return CompletedConfig{&completed}
}

// New builds the convoy API server and installs its API groups.
func (c completedConfig) New() (*ConvoyServer, error) {
	genericServer, err := c.GenericConfig.New("convoy-apiserver", genericapiserver.NewEmptyDelegate())
	if err != nil {
		return nil, err
	}

	s := &ConvoyServer{GenericAPIServer: genericServer}

	apiGroupInfo := genericapiserver.NewDefaultAPIGroupInfo(core.GroupName, Scheme, metav1.ParameterCodec, Codecs)

	clusterREST, clusterStatusREST, err := clusterstorage.NewREST(Scheme, c.GenericConfig.RESTOptionsGetter)
	if err != nil {
		return nil, err
	}

	v1alpha1storage := map[string]rest.Storage{
		"clusters":        clusterREST,
		"clusters/status": clusterStatusREST,
	}
	apiGroupInfo.VersionedResourcesStorageMap["v1alpha1"] = v1alpha1storage

	if err := s.GenericAPIServer.InstallAPIGroup(&apiGroupInfo); err != nil {
		return nil, err
	}

	return s, nil
}
