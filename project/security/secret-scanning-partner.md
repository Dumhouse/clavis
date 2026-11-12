# GitHub secret scanning partner registration packet

Token formats:

- `clv_mt_` machine identity
- `clv_at_` agent identity
- `clv_pat_` personal access token

Pattern: `clv_(mt|at|pat)_[A-Za-z0-9]{32,64}[0-9a-f]{8}`

The suffix is a CRC32 checksum over `<kind>_<body>`, so scanners can reduce false
positives. Tokens are high-entropy and shown once. Revocation is immediate by
deleting the token record; identity deletion revokes all tokens.

Owner: security@clavis.dev
Policy: https://github.com/dumhouse/clavis/security/policy
