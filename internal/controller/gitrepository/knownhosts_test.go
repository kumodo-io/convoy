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

package gitrepository

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	xssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func testPubKey(t *testing.T) xssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	sshPub, err := xssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("ssh public key: %v", err)
	}
	return sshPub
}

func TestKnownHostsCallback_AcceptAndReject(t *testing.T) {
	good := testPubKey(t)
	other := testPubKey(t)
	line := knownhosts.Line([]string{"github.com"}, good)

	cb, err := knownHostsCallback([]byte(line + "\n"))
	if err != nil {
		t.Fatalf("knownHostsCallback: %v", err)
	}

	// Matching host + key → accepted.
	if err := cb("github.com:22", nil, good); err != nil {
		t.Errorf("expected accept for known host+key, got %v", err)
	}
	// Known host, wrong key → mismatch (possible MITM).
	if err := cb("github.com:22", nil, other); err == nil {
		t.Error("expected host key mismatch, got accept")
	}
	// Unknown host → rejected.
	if err := cb("gitlab.com:22", nil, good); err == nil {
		t.Error("expected reject for unknown host, got accept")
	}
}

func TestKnownHostsCallback_HashedRejected(t *testing.T) {
	pub := testPubKey(t)
	hashed := knownhosts.HashHostname("github.com")
	line := hashed + " " + string(xssh.MarshalAuthorizedKey(pub))
	if _, err := knownHostsCallback([]byte(line)); err == nil {
		t.Error("expected hashed known_hosts to be rejected, got nil")
	}
}

func TestKnownHostsCallback_Empty(t *testing.T) {
	if _, err := knownHostsCallback([]byte("# just a comment\n")); err == nil {
		t.Error("expected error for known_hosts with no usable entries")
	}
}
