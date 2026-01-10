# Agent Sentinel - Backlog

This document tracks future enhancements and improvements for Agent Sentinel.

## Telemetry / Logging Enhancements

- **Tenant ID hashing middleware**: add upstream hashing of tenant IDs before metrics labeling / logging


## Testing

### Unit Tests
- **Core Functionality Tests**
  - Test rate limiting logic (allow/deny scenarios)
  - Test cost calculation and adjustment
  - Test token counting accuracy
  - Test streaming response parsing
  - Test fail-open behavior when Redis unavailable

## Future Enhancements

### Features
- **Metrics Dashboard**: Build observability dashboard for rate limiting and costs

### Infrastructure
- **Prometheus Metrics**: Export Prometheus metrics in addition to OTLP
- **Configuration Management**: Support config files in addition to env vars
