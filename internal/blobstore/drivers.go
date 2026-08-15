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

package blobstore

// Registered gocloud blob drivers. Each blank import registers a URL scheme with
// gocloud and pulls in that backend's SDK, so Open accepts:
//
//	s3://…      (and any S3-compatible store via query params)
//	gs://…      (Google Cloud Storage, native)
//	azblob://…  (Azure Blob Storage, native)
//	file://…    (local filesystem — dev / single-node / PVC)
//	mem://      (in-process — tests)
import (
	_ "gocloud.dev/blob/azureblob"
	_ "gocloud.dev/blob/fileblob"
	_ "gocloud.dev/blob/gcsblob"
	_ "gocloud.dev/blob/memblob"
	_ "gocloud.dev/blob/s3blob"
)
