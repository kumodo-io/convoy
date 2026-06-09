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

// Package install registers the extensions API group (internal + versioned types
// and conversions) into a runtime.Scheme. It lives in its own package to avoid an
// import cycle: the v1alpha1 conversions import the internal extensions package.
package install

import (
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	"github.com/kumodo-io/convoy/api/extensions"
	extensionsv1alpha1 "github.com/kumodo-io/convoy/api/extensions/v1alpha1"
)

// Install registers the extensions group's internal and external types into
// scheme, then sets v1alpha1 as the preferred external version.
func Install(scheme *runtime.Scheme) {
	utilruntime.Must(extensions.AddToScheme(scheme))
	utilruntime.Must(extensionsv1alpha1.AddToScheme(scheme))
	utilruntime.Must(scheme.SetVersionPriority(extensionsv1alpha1.SchemeGroupVersion))
}
