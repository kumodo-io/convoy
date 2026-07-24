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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	xssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	authv1 "github.com/kumodo-io/convoy/api/auth/v1alpha1"
	sourcev1alpha1 "github.com/kumodo-io/convoy/api/source/v1alpha1"
	"github.com/kumodo-io/convoy/internal/auth"
)

// tokenAuthUsername is the placeholder username used for token auth over HTTPS:
// the token is sent as the basic-auth password, which GitHub/GitLab PATs accept
// (a Bearer header is rejected by GitHub's git endpoint). Any non-empty username
// works with a PAT.
const tokenAuthUsername = "git"

// authMethod builds the go-git transport auth for a repository from its auth
// spec. "none" returns nil (anonymous); unsupported types return ErrUnsupported.
func authMethod(ctx context.Context, resolver auth.SecretResolver, repo *sourcev1alpha1.GitRepository) (transport.AuthMethod, error) {
	spec := repo.Spec.Auth
	ns := repo.Namespace
	switch spec.Type {
	case sourcev1alpha1.GitRepositoryAuthTypeNone:
		return nil, nil

	case sourcev1alpha1.GitRepositoryAuthTypeBasic:
		if spec.Basic == nil {
			return nil, errors.Join(ErrUnsupported, fmt.Errorf("basic auth selected but not configured"))
		}
		pw, err := auth.ResolveSecretKey(ctx, resolver, ns, spec.Basic.PasswordRef)
		if err != nil {
			return nil, err
		}
		return &githttp.BasicAuth{Username: spec.Basic.Username, Password: string(pw)}, nil

	case sourcev1alpha1.GitRepositoryAuthTypeToken:
		if spec.Token == nil {
			return nil, errors.Join(ErrUnsupported, fmt.Errorf("token auth selected but not configured"))
		}
		tok, err := auth.ResolveSecretKey(ctx, resolver, ns, spec.Token.TokenRef)
		if err != nil {
			return nil, err
		}
		return &githttp.BasicAuth{Username: tokenAuthUsername, Password: string(tok)}, nil

	case sourcev1alpha1.GitRepositoryAuthTypeSSHKey:
		if spec.SSHKey == nil {
			return nil, errors.Join(ErrUnsupported, fmt.Errorf("sshKey auth selected but not configured"))
		}
		return sshAuthMethod(ctx, resolver, ns, spec.SSHKey)

	default:
		return nil, errors.Join(ErrUnsupported, fmt.Errorf("auth type %q is not supported", spec.Type))
	}
}

// sshAuthMethod builds SSH public-key auth with host-key verification. A
// known-hosts reference is required — there is no insecure "ignore host key"
// mode — so a man-in-the-middle cannot impersonate the server.
func sshAuthMethod(ctx context.Context, resolver auth.SecretResolver, namespace string, spec *authv1.SSHKeyAuthSpec) (transport.AuthMethod, error) {
	key, err := auth.ResolveSecretKey(ctx, resolver, namespace, spec.SSHKeyRef)
	if err != nil {
		return nil, err
	}
	pk, err := gitssh.NewPublicKeys(gitssh.DefaultUsername, key, "")
	if err != nil {
		return nil, errors.Join(ErrUnsupported, fmt.Errorf("parsing ssh key: %w", err))
	}
	if spec.KnownHostsRef.Name == "" {
		return nil, errors.Join(ErrUnsupported, fmt.Errorf("sshKey auth requires knownHostsRef for host-key verification"))
	}
	knownHosts, err := auth.ResolveConfigMapKey(ctx, resolver, namespace, spec.KnownHostsRef)
	if err != nil {
		return nil, err
	}
	cb, err := knownHostsCallback(knownHosts)
	if err != nil {
		return nil, err
	}
	pk.HostKeyCallback = cb
	return pk, nil
}

// hostKeyEntry is one parsed known_hosts line: its normalized host patterns, the
// public key, and any marker ("revoked").
type hostKeyEntry struct {
	marker string
	hosts  []string
	key    xssh.PublicKey
}

// knownHostsCallback builds a host-key verification callback from known_hosts
// content held in memory (no file is written). It supports plaintext host entries
// (the common git case), multiple key types per host, and @revoked entries.
// Hashed entries (|1|…) are rejected — provide plaintext hosts — and
// @cert-authority entries are skipped (CA-based verification is unsupported).
func knownHostsCallback(content []byte) (xssh.HostKeyCallback, error) {
	var entries []hostKeyEntry
	rest := content
	for {
		marker, hosts, key, _, remainder, err := xssh.ParseKnownHosts(rest)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errors.Join(ErrUnsupported, fmt.Errorf("parsing known_hosts: %w", err))
		}
		rest = remainder
		if marker == "cert-authority" {
			continue // CA-based host verification is not supported
		}
		norm := make([]string, 0, len(hosts))
		for _, h := range hosts {
			if strings.HasPrefix(h, "|") {
				return nil, errors.Join(ErrUnsupported, fmt.Errorf("hashed known_hosts entries are not supported; provide plaintext host entries"))
			}
			norm = append(norm, knownhosts.Normalize(h))
		}
		entries = append(entries, hostKeyEntry{marker: marker, hosts: norm, key: key})
	}
	if len(entries) == 0 {
		return nil, errors.Join(ErrUnsupported, fmt.Errorf("known_hosts has no usable host entries"))
	}

	return func(hostname string, _ net.Addr, key xssh.PublicKey) error {
		want := knownhosts.Normalize(hostname)
		presented := key.Marshal()
		hostSeen := false
		for _, e := range entries {
			matched := false
			for _, h := range e.hosts {
				if h == want {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			hostSeen = true
			if bytes.Equal(e.key.Marshal(), presented) {
				if e.marker == "revoked" {
					return fmt.Errorf("host key for %s is revoked", hostname)
				}
				return nil
			}
		}
		if hostSeen {
			return fmt.Errorf("host key mismatch for %s (possible man-in-the-middle)", hostname)
		}
		return fmt.Errorf("no known_hosts entry for %s", hostname)
	}, nil
}
