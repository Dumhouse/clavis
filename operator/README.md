# Kubernetes operator

A separate Go module will live here in M3. The operator reconciles `ClavisSecret`
resources into Kubernetes Secrets by asking the server for short-lived access,
and never persists values in etcd.

Planned tasks: see [tasks/M3-integrations.md](../tasks/M3-integrations.md).
