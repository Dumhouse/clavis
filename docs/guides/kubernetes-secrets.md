---
description: "Four ways to get Clavis secrets into Kubernetes: a one-off Secret manifest, the operator, the CSI driver, or External Secrets Operator. Which to choose."
---

# Secrets for Kubernetes

Pick by how much you want Kubernetes to know about Clavis.

| You want | Use | Secrets live as |
| --- | --- | --- |
| A quick, one-time hand-off | `clavis export --format k8s` | a normal `Secret` you apply |
| Secrets that stay in sync and restart pods when they change | the [operator](#operator) | `Secret` objects it manages |
| No `Secret` objects at all | the [CSI driver](#csi-driver) | files in the pod |
| You already run External Secrets Operator | [ESO](#external-secrets-operator) | `Secret` objects ESO creates |

## One-off Secret

```sh
clavis export --env production --format k8s | kubectl apply -f -
```

Exports are recorded in the audit log. Nothing keeps the `Secret` up to date
afterwards, so use one of the options below for anything long-lived.

## Operator

The operator reconciles a `ClavisSecret` resource into a Kubernetes `Secret`
and restarts annotated Deployments when values change. Install and token
steps are in the [operator README](https://github.com/Dumhouse/clavis/tree/main/operator).
Give it a machine identity limited to one environment with the
`secrets:read-values` permission.

## CSI driver

`clavis-csi` writes resolved secrets as files into a pod volume, so no
Kubernetes `Secret` is created. Install and volume examples are in the
[CSI README](https://github.com/Dumhouse/clavis/tree/main/csi).

## External Secrets Operator

Clavis serves a flat JSON map at `/secrets:eso` for ESO's webhook provider.
See the [ESO guide](https://github.com/Dumhouse/clavis/tree/main/integrations/eso).

## From a job inside the cluster

A Kubernetes workload can sign in with its service account instead of a
stored token: create an identity with `--oidc kubernetes` (see
`clavis identities create --help`), then use `clavis run` in the job.
