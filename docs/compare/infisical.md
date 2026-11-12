---
description: "How Clavis compares with Infisical: license, self-hosting, dynamic credentials, typed schema, and agent support."
---

# Clavis vs Infisical

Infisical is an open-source secrets platform with a hosted cloud and a self-hosted option. Its core is MIT licensed, with some features in paid tiers.

## At a glance

| | Clavis | Infisical |
| --- | --- | --- |
| License | Apache-2.0 | MIT core + paid |
| Self-hosting | Yes | Yes |
| Single binary with embedded UI | Yes | No |
| SSO and RBAC | Yes | Paid |
| Dynamic database and cloud credentials | Yes | Paid |
| Time-limited access and approvals | Yes | Paid |
| Typed schema | Yes | No |
| Generated typed config loaders | Yes | No |
| MCP server | Yes | Yes |
| Agents run commands without seeing values | Yes | Separate product |
| Expiring share links | Yes | Yes |

Compared from public documentation. Plans and features change, so check
Infisical's own docs before you decide, and [open an issue](https://github.com/dumhouse/clavis/issues)
if something here is out of date.

## Which to choose

- Choose **Infisical** if you want a hosted service run for you, or you rely on a feature that is in its tier.
- Choose **Clavis** if you want everything in one Apache-2.0 self-hosted binary with no feature tiers, plus typed schema and agent-safe command execution.

## Moving over

[Migrate from Infisical](../migrations/infisical) covers importing your existing secrets, with a dry run first.
