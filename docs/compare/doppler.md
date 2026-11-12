---
description: "How Clavis compares with Doppler: license, self-hosting, dynamic credentials, typed schema, and agent support."
---

# Clavis vs Doppler

Doppler is a hosted secrets manager with a polished developer experience. It is proprietary and primarily a cloud service.

## At a glance

| | Clavis | Doppler |
| --- | --- | --- |
| License | Apache-2.0 | Proprietary |
| Self-hosting | Yes | Enterprise |
| Single binary with embedded UI | Yes | No |
| SSO and RBAC | Yes | Paid |
| Dynamic database and cloud credentials | Yes | Enterprise |
| Time-limited access and approvals | Yes | Paid |
| Typed schema | Yes | Value types |
| Generated typed config loaders | Yes | No |
| MCP server | Yes | Yes |
| Agents run commands without seeing values | Yes | No |
| Expiring share links | Yes | Yes |

Compared from public documentation. Plans and features change, so check
Doppler's own docs before you decide, and [open an issue](https://github.com/dumhouse/clavis/issues)
if something here is out of date.

## Which to choose

- Choose **Doppler** if you want a fully managed service and do not need to self-host.
- Choose **Clavis** if you need to run it yourself, want open-source code you can audit, or want generated typed config and agent-safe tools.

## Moving over

[Migrate from Doppler](../migrations/doppler) covers importing your existing secrets, with a dry run first.
