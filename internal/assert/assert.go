package assert

import (
	"github.com/kumodo-io/convoy/internal/auth"
	"github.com/kumodo-io/convoy/internal/controller/cluster"
)

var _ cluster.Authenticator = (*auth.KubeconfigAuthenticator)(nil)
