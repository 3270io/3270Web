## 2024-05-23 - SSRF Protection against Cloud Metadata
**Vulnerability:** The `isValidHostname` validator allowed `169.254.169.254` (AWS/GCP/Azure Metadata Service) and other Link-Local IPs.
**Learning:** `net.ParseIP` returning non-nil is not sufficient for security; it only validates format. Specific dangerous ranges must be blocked.
**Prevention:** Use `ip.IsLinkLocalUnicast()` and `ip.IsLinkLocalMulticast()` to detect and block non-routable addresses that might be used for SSRF against cloud infrastructure.
