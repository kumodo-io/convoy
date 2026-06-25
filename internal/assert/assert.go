package assert

import (
	"github.com/kumodo-io/convoy/internal/auth"
	"github.com/kumodo-io/convoy/internal/clusterconn"
)

var _ clusterconn.Authenticator = (*auth.KubeconfigAuthenticator)(nil)
