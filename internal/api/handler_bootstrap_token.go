package api

import (
	"net"
	"net/http"
	"net/url"
)

// HandleBootstrapToken hands the auto-generated admin bearer token to a
// loopback caller so the local-machine UI can skip the manual paste step
// on first boot. Refuses for any other source:
//
//   - non-loopback connection peer (X-Forwarded-For is ignored — the
//     check reads the raw RemoteAddr, so a reverse proxy can't trick us
//     into handing the token to a remote browser),
//   - cross-origin request (Origin header doesn't match the request's
//     Host),
//   - operator-supplied token (GATEWAY_AUTH_TOKEN was set at boot —
//     the gateway doesn't hand out tokens it doesn't own; that secret
//     is the operator's, and the bootstrap-token surface stays sealed).
//
// The response is JSON: {"object":"bootstrap_token","data":{"token":"…"}}.
// Refusals return 403 with the standard error envelope and a brief
// reason so the UI's TokenGate can fall back to its paste flow.
func (h *Handler) HandleBootstrapToken(w http.ResponseWriter, r *http.Request) {
	if !h.bootstrapTokenExposable {
		WriteError(w, http.StatusForbidden, errCodeUnauthorized, "bootstrap-token surface is disabled (operator-supplied admin token)")
		return
	}
	if !isLoopbackRequest(r) {
		WriteError(w, http.StatusForbidden, errCodeUnauthorized, "bootstrap-token is only available to loopback callers")
		return
	}
	if !sameOriginRequest(r) {
		WriteError(w, http.StatusForbidden, errCodeUnauthorized, "bootstrap-token rejects cross-origin requests")
		return
	}
	token := h.config.Server.AuthToken
	if token == "" {
		WriteError(w, http.StatusForbidden, errCodeUnauthorized, "bootstrap-token has nothing to hand out (no admin token configured)")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"object": "bootstrap_token",
		"data":   map[string]string{"token": token},
	})
}

// isLoopbackRequest reads the raw connection peer (no X-Forwarded-For
// trust) and reports whether it sits on the loopback interface. Empty
// or unparseable RemoteAddr fails closed.
func isLoopbackRequest(r *http.Request) bool {
	if r == nil || r.RemoteAddr == "" {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// sameOriginRequest confirms that when the browser supplied an Origin
// header, its host matches the request's Host. Requests without an
// Origin header (curl, server-to-server) pass — the loopback check is
// the primary boundary; Origin only adds a cross-origin defense for
// browser callers.
func sameOriginRequest(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return u.Host == r.Host
}
