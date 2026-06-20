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

package auth

// RegistryCredential is the convoy-neutral result of authenticating to an OCI /
// Helm registry. The source controller adapts this to whatever registry client
// it uses; keeping it neutral avoids leaking a client choice into the auth package.
type RegistryCredential struct {
	// Username and Password are the basic registry credentials (for ECR this is
	// "AWS" plus the authorization token, for example).
	Username string
	Password string
	// IdentityToken is an OAuth2 refresh/identity token for registries that use
	// token-based auth instead of basic credentials.
	IdentityToken string
}
