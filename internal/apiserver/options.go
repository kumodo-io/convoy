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
	"context"
	"fmt"
	"net"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	openapinamer "k8s.io/apiserver/pkg/endpoints/openapi"
	genericapiserver "k8s.io/apiserver/pkg/server"
	genericoptions "k8s.io/apiserver/pkg/server/options"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	generatedopenapi "github.com/kumodo-io/convoy/api/generated/openapi"
)

const defaultEtcdPathPrefix = "/registry/convoy.kumodo.io"

// Options bundles everything needed to start the convoy API server. The etcd
// connection is supplied at runtime via the standard --etcd-* flags; convoy does
// not embed or assume any particular etcd.
type Options struct {
	RecommendedOptions *genericoptions.RecommendedOptions
}

// NewOptions returns Options with sane defaults.
func NewOptions() *Options {
	o := &Options{
		RecommendedOptions: genericoptions.NewRecommendedOptions(
			defaultEtcdPathPrefix,
			Codecs.LegacyCodec(corev1alpha1.SchemeGroupVersion),
		),
	}
	// Store objects in etcd as the v1alpha1 external version.
	o.RecommendedOptions.Etcd.StorageConfig.EncodeVersioner = runtime.NewMultiGroupVersioner(
		corev1alpha1.SchemeGroupVersion,
		schema.GroupKind{Group: corev1alpha1.SchemeGroupVersion.Group},
	)
	return o
}

// Complete fills in any defaults.
func (o *Options) Complete() error { return nil }

// Validate validates the options.
func (o *Options) Validate() error {
	return utilerrors.NewAggregate(o.RecommendedOptions.Validate())
}

// Config builds the apiserver Config from the options.
func (o *Options) Config() (*Config, error) {
	if err := o.RecommendedOptions.SecureServing.MaybeDefaultWithSelfSignedCerts("localhost", nil, []net.IP{net.ParseIP("127.0.0.1")}); err != nil {
		return nil, fmt.Errorf("error creating self-signed certificates: %w", err)
	}

	serverConfig := genericapiserver.NewRecommendedConfig(Codecs)

	serverConfig.OpenAPIConfig = genericapiserver.DefaultOpenAPIConfig(generatedopenapi.GetOpenAPIDefinitions, openapinamer.NewDefinitionNamer(Scheme))
	serverConfig.OpenAPIConfig.Info.Title = "convoy"
	serverConfig.OpenAPIConfig.Info.Version = "0.1"

	serverConfig.OpenAPIV3Config = genericapiserver.DefaultOpenAPIV3Config(generatedopenapi.GetOpenAPIDefinitions, openapinamer.NewDefinitionNamer(Scheme))
	serverConfig.OpenAPIV3Config.Info.Title = "convoy"
	serverConfig.OpenAPIV3Config.Info.Version = "0.1"

	if err := o.RecommendedOptions.ApplyTo(serverConfig); err != nil {
		return nil, err
	}

	return &Config{
		GenericConfig: serverConfig,
		ExtraConfig:   ExtraConfig{},
	}, nil
}

// Run builds and runs the convoy API server until ctx is done.
func (o *Options) Run(ctx context.Context) error {
	config, err := o.Config()
	if err != nil {
		return err
	}

	server, err := config.Complete().New()
	if err != nil {
		return err
	}

	return server.GenericAPIServer.PrepareRun().RunWithContext(ctx)
}

// NewCommand builds the cobra command that starts the convoy API server.
func NewCommand(ctx context.Context) *cobra.Command {
	o := NewOptions()

	cmd := &cobra.Command{
		Use:   "convoy-apiserver",
		Short: "Launch the convoy aggregated API server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := o.Complete(); err != nil {
				return err
			}
			if err := o.Validate(); err != nil {
				return err
			}
			return o.Run(cmd.Context())
		},
	}
	cmd.SetContext(ctx)

	o.RecommendedOptions.AddFlags(cmd.Flags())

	return cmd
}
