# Fuzzing

Clavis keeps fuzz targets for parsers and reference expansion:

- `FuzzParseToken` — token prefix, alphabet, and checksum parsing.
- `FuzzParseDotenv` — `.env` parsing used by imports.
- `FuzzResolveSecretReferences` — `${KEY}` expansion, including cycles.

Run a target locally:

```sh
go test -run=^$ -fuzz=FuzzParseToken -fuzztime=60s ./internal/authn
go test -run=^$ -fuzz=FuzzParseDotenv -fuzztime=60s ./internal/cli
go test -run=^$ -fuzz=FuzzResolveSecretReferences -fuzztime=60s ./internal/service
```

CI runs a 15-second smoke pass on every pull request through the `Fuzz smoke`
job. Corpus files live under `testdata/fuzz` when a failure is found and should
be committed with the fix.
