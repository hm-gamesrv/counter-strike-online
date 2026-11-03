// This module exists only to stop the parent module's "./..." patterns from
// walking into the archived probes. The files under review-* are overlay sources
// injected into internal/server or internal/store; they are not standalone
// packages and cannot compile on their own.
module github.com/hm-gamesrv/cso-server-go/code-review/probes

go 1.27
