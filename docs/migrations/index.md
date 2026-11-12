# Migration guides

Move an existing secret store into Clavis without a big-bang cutover. Each
guide starts with a dry run, maps keys into a project and environment, and
keeps the source system readable until you cut over.

- [Infisical](infisical.md)
- [Doppler](doppler.md)
- [Vault / OpenBao](vault.md)
- [`.env` files and directories](dotenv.md)

The importer CLI supports the same providers; see
[`clavis import`](../commands.md).

```sh
clavis import --provider infisical --dry-run
clavis import --provider vault --remote-path apps/web --dry-run
```
