# clavis-client (Ruby)

A tiny, dependency-free client that fetches a [Clavis](https://github.com/Dumhouse/clavis)
environment's secrets at start-up. Use it where you can't run `clavis run`,
such as a serverless function or a slim container. Ruby 3.1 or later.

Copy [`clavis_client.rb`](clavis_client.rb) into your project, or add this
directory to the load path:

```ruby
require_relative "clavis_client"

Clavis.load_secrets # fills ENV; values already set win
```

Set `CLAVIS_SERVER`, `CLAVIS_TOKEN` (with `secrets:read-values`),
`CLAVIS_ORG`, `CLAVIS_PROJECT` and `CLAVIS_ENV` (default `dev`).
`Clavis.fetch_secrets` returns the values as a hash instead.

Every fetch is recorded in the audit log. Nothing is cached, and output is not
masked, so don't log what it returns.

Guide: https://dumhouse.github.io/clavis/guides/sdks
