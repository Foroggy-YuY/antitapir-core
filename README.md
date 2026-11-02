# Anti-Tapir Core

Windows networking component for Anti-Tapir, maintained by Foroggy
(https://github.com/Foroggy-YuY). Local source preparation, not a public release.

Based on SagerNet/sing-box v1.14.0-alpha.18 at
b2d5fb62290b9a648a04693a2d32ebf253d684ae. Original authorship, LICENSE and
upstream history are retained. Historical Anti-Tapir integration by devMorpheus:
https://github.com/devMorpheus/sing-box (46659f117676e66dd2b5caec41215f0595a72984).
The inherited XOR/UDP and embedded-profile design has been replaced.

## Changes

- External JSON configuration; no private credentials embedded at build time.
- Managed inherited-stdio protocol v1, startup readiness and graceful shutdown.
- Parent EOF cancellation, startup/shutdown deadlines and fixed safe error codes.
- Core identity via `core-info` and Go lifecycle tests with no VPN connection.
- Core 0.2 adds the memory-config capability: decrypted user profiles arrive
  through the inherited pipe, never through a temporary cleartext JSON file.
- Upstream protocol/routing implementations remain unchanged.

## Build and verify

Requires Go 1.24.7+. Run on Windows:

```bat
go test ./cmd/sing-box/internal/control
go build -tags with_utls -o antitapir-core.exe ./cmd/sing-box
antitapir-core.exe core-info
antitapir-core.exe check -c examples/antitapir-smoke.json
```

The synthetic config has no TUN, listener or server. Successful smoke tests do
not prove live TUN operation. See docs/antitapir-protocol.md for the IPC contract;
its application-side commands refer to the companion Anti-Tapir repository.

See LICENSE and UPSTREAM_README.md for upstream notices and naming conditions.
No upstream authorship is claimed by this fork.
