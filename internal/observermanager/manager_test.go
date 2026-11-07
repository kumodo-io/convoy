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

package observermanager

import "testing"

const (
	testPodIP     = "10.0.0.1"
	testServeAddr = ":8080"
)

func TestResolveAdvertiseURL(t *testing.T) {
	for _, tc := range []struct {
		name, configured, serveAddr, podIP, want string
	}{
		{name: "configured wins", configured: "http://obs:9000", serveAddr: testServeAddr, podIP: testPodIP, want: "http://obs:9000"},
		{name: "pod IP and serve port", serveAddr: testServeAddr, podIP: testPodIP, want: "http://10.0.0.1:8080"},
		{name: "IPv6 pod IP", serveAddr: "0.0.0.0:8081", podIP: "fd00::1", want: "http://[fd00::1]:8081"},
		{name: "no pod IP", serveAddr: testServeAddr},
		{name: "no serve port", serveAddr: "bad", podIP: testPodIP},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("POD_IP", tc.podIP)
			if got := resolveAdvertiseURL(tc.configured, tc.serveAddr); got != tc.want {
				t.Errorf("resolveAdvertiseURL(%q, %q) = %q, want %q", tc.configured, tc.serveAddr, got, tc.want)
			}
		})
	}
}
