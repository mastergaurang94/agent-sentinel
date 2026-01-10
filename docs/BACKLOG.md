# Agent Sentinel - Backlog

Tracking near-term improvements and testing priorities.

## Features
- Metrics dashboard for rate limits, costs, provider latency/TTFT, loop detection counts, and runtime saturation (goroutines, async queue depth).
- Tenant ID hashing middleware before metrics labeling/logging.

## Infrastructure
- Prometheus export alongside OTLP plus a starter dashboard; decide default exporter path.
- Config file support in addition to environment variables.
