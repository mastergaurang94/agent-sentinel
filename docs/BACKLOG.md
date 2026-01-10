# Agent Sentinel - Backlog

This document tracks future enhancements and improvements for Agent Sentinel.

## Telemetry Enhancements

### Add Additional Telemetry Metrics
- [ ] **Goroutine Metrics**: Add OpenTelemetry metrics for async operations
  - Track async operation queue depth
  - Measure async operation latency
  - Monitor semaphore utilization
  
- [ ] **TTFT (Time-To-First-Token) Metrics**: Add telemetry for streaming responses
  - Measure time from request start to first chunk received
  - Track streaming response duration
  
- [ ] **Rate Limiting Metrics**: Add detailed telemetry for rate limiting operations
  - Track rate limit checks (allowed/denied)
  - Measure Redis operation latency
  - Monitor cost estimation accuracy (estimate vs actual)
- [ ] **Tenant ID hashing middleware**: add upstream hashing of tenant IDs before telemetry/metrics labeling


## Testing

### Unit Tests
- [ ] **Core Functionality Tests**
  - Test rate limiting logic (allow/deny scenarios)
  - Test cost calculation and adjustment
  - Test token counting accuracy
  - Test streaming response parsing
  - Test fail-open behavior when Redis unavailable

## Future Enhancements

### Features
- [ ] **Metrics Dashboard**: Build observability dashboard for rate limiting and costs

### Infrastructure
- [ ] **Prometheus Metrics**: Export Prometheus metrics in addition to OTLP
- [ ] **Configuration Management**: Support config files in addition to env vars
