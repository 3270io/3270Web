## 2024-05-23 - SSRF Protection against Cloud Metadata
**Vulnerability:** The `isValidHostname` validator allowed `169.254.169.254` (AWS/GCP/Azure Metadata Service) and other Link-Local IPs.
**Learning:** `net.ParseIP` returning non-nil is not sufficient for security; it only validates format. Specific dangerous ranges must be blocked.
**Prevention:** Use `ip.IsLinkLocalUnicast()` and `ip.IsLinkLocalMulticast()` to detect and block non-routable addresses that might be used for SSRF against cloud infrastructure.

## 2026-10-08 - A CSP permission that is for a feature that never shipped is still a permission
**Vulnerability:** `baseCSP` in `embedding.go` declared `connect-src 'self' ws: wss:`. The web frontend uses only `fetch` and `EventSource`; no WebSocket code exists on the client, and `docs/rest-api.md` lists WebSocket streaming as explicitly out of scope. The two schemes were a planning placeholder that let a script-execution gadget on any page open a WebSocket to an attacker-controlled host.
**Learning:** CSP directives tracking a roadmap item that never shipped are indistinguishable from an intentional egress hole once a bug finds them. Grep the codebase for actual users of a permission before leaving it in the policy. In particular, `connect-src` with a scheme and no host is a wildcard, not a hint.
**Prevention:** When adding a CSP directive, cite the exact client code that uses it; when retiring a feature, retire its CSP permission with it. The regression test for the headers (`TestSecurityHeaders`) now pins the tightened directive.

## 2026-09-03 - Every connection path has to run every host check
**Vulnerability:** `resetSessionHost` (the workflow-driven reconnect and Copilot `connect_session` path) ran `isValidHostname` and `checkHostAllowed` but skipped `checkHostResolves`, so `"localhost"` or any attacker-controlled DNS name pointing at 169.254.169.254 bypassed the SSRF gate that `startHostSession` closes.
**Learning:** A syntactic literal check catches literals. Loopback/link-local reachability through a *name* is a separate check (`egress.Policy.CheckHost`), and every path that reaches a host — creation *and* reconnect — has to run it. A fence with one gate open is not a fence.
**Prevention:** When adding a new "point this session at a host" path, mirror `startHostSessionWithProfile`: `isValidHostname` → `checkHostAllowed` → `checkHostResolves`. Grep for `parseHostPort` callers when auditing.
