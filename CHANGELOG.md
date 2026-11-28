# Changelog

All notable changes to this project will be documented in this file.

## v1.0.0 (2026-11-27)

### Added

- Web dashboard for server monitoring and control.
-  CLI (mcsm) for remote management.
- HTTP API with OpenAPI documentation.
- Live console streaming via Server-Sent Events (SSE).
- Player tracking and server event parsing.
- CPU, memory, and disk metrics.
- Automatic crash restart with configurable backoff.
- World backup creation, retention, and scheduling.
- Token-based API authentication.
- Tailscale integration for remote access.
- Windows and Linux support.
- Graceful shutdown with world save handling.

### Security

- Require authentication when binding to non-loopback addresses.
- Documented that remote access uses plain HTTP and should only be used on trusted networks.
