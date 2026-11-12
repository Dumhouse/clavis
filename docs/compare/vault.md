---
description: "How Clavis compares with HashiCorp Vault: license, self-hosting, dynamic credentials, typed schema, and agent support."
---

# Clavis vs HashiCorp Vault

Vault is the long-standing standard for secrets and dynamic credentials, and OpenBao is its open-source fork. It is powerful and heavy: you run and unseal a cluster, write policies in HCL, and build the developer workflow yourself.

## At a glance

| | Clavis | Vault |
| --- | --- | --- |
| License | Apache-2.0 | BSL 1.1 |
| Self-hosting | Yes | Yes |
| Single binary with embedded UI | Yes | Yes |
| SSO and RBAC | Yes | Yes |
| Dynamic database and cloud credentials | Yes | Yes |
| Time-limited access and approvals | Yes | Enterprise |
| Typed schema | Yes | No |
| Generated typed config loaders | Yes | No |
| MCP server | Yes | Beta |
| Agents run commands without seeing values | Yes | No |
| Expiring share links | Yes | No |

Compared from public documentation. Plans and features change, so check
HashiCorp Vault's own docs before you decide, and [open an issue](https://github.com/dumhouse/clavis/issues)
if something here is out of date.

## Which to choose

- Choose **Vault** if you already operate it, need its broad secrets engines, or have compliance requirements that name it.
- Choose **Clavis** if you want one binary with a built-in UI, a typed schema, and a workflow for developers and coding agents without building it on top of Vault.

## Moving over

[Migrate from Vault or OpenBao](../migrations/vault) covers importing your existing secrets, with a dry run first.
