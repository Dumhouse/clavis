# clavis-client

A tiny, dependency-free client that fetches a [Clavis](https://github.com/Dumhouse/clavis)
environment's secrets at start-up. Use it where you can't run `clavis run`,
such as a serverless function or a slim container.

```sh
pip install clavis-client
```

```python
import clavis_client

clavis_client.load_secrets()  # fills os.environ; values already set win
```

Set `CLAVIS_SERVER`, `CLAVIS_TOKEN` (with `secrets:read-values`),
`CLAVIS_ORG`, `CLAVIS_PROJECT` and `CLAVIS_ENV` (default `dev`).
`fetch_secrets()` returns a dict instead.

Every fetch is recorded in the audit log. Nothing is cached, and output is not
masked, so don't log what it returns.

Guide: https://dumhouse.github.io/clavis/guides/sdks
