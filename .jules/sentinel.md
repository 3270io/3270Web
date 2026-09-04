## 2024-05-23 - SSRF Protection against Cloud Metadata
**Vulnerability:** The `isValidHostname` validator allowed `169.254.169.254` (AWS/GCP/Azure Metadata Service) and other Link-Local IPs.
**Learning:** `net.ParseIP` returning non-nil is not sufficient for security; it only validates format. Specific dangerous ranges must be blocked.
**Prevention:** Use `ip.IsLinkLocalUnicast()` and `ip.IsLinkLocalMulticast()` to detect and block non-routable addresses that might be used for SSRF against cloud infrastructure.

## 2026-09-03 - Every connection path has to run every host check
**Vulnerability:** `resetSessionHost` (the workflow-driven reconnect and Copilot `connect_session` path) ran `isValidHostname` and `checkHostAllowed` but skipped `checkHostResolves`, so `"localhost"` or any attacker-controlled DNS name pointing at 169.254.169.254 bypassed the SSRF gate that `startHostSession` closes.
**Learning:** A syntactic literal check catches literals. Loopback/link-local reachability through a *name* is a separate check (`egress.Policy.CheckHost`), and every path that reaches a host — creation *and* reconnect — has to run it. A fence with one gate open is not a fence.
**Prevention:** When adding a new "point this session at a host" path, mirror `startHostSessionWithProfile`: `isValidHostname` → `checkHostAllowed` → `checkHostResolves`. Grep for `parseHostPort` callers when auditing.
