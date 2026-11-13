---
description: "Answer a few questions and get a ready docker-compose.yaml for Clavis: localhost, home lab, public domain with Let's Encrypt, or behind your reverse proxy."
---

# Install wizard

Pick where Clavis will run and how it should do HTTPS. The wizard writes a
`compose.yaml`, the command to start it, and a setup link you can click. It all
runs in your browser; nothing you type here is sent anywhere.

<InstallWizard />

## What the options mean

**Where will Clavis run?**

- **Try it on this computer.** Plain HTTP on `http://localhost`. Browsers
  treat `localhost` as secure, so sign-in and passkeys work without a
  certificate.
- **Home lab or office network.** You reach the server by IP address or a
  local name. Clavis makes a **self-signed certificate** for that address, so
  traffic is encrypted. Your browser warns once; see
  [trusting the certificate](./https#trust-the-self-signed-certificate).
- **Public server with a domain.** Clavis gets a free certificate from
  **Let's Encrypt** and renews it. The domain must point at the server and
  ports 80 and 443 must be reachable from the internet.
- **Behind my reverse proxy.** Caddy, nginx, Traefik or Cloudflare handles
  HTTPS. Clavis speaks plain HTTP on `127.0.0.1:8080` and the proxy forwards to
  it. The base URL is still `https://`, so cookies and passkeys work.

**Setup token in the file.** A fresh server must be claimed with a one-time
token, so whoever reaches it first cannot take it over. When you let the wizard
put a token in `compose.yaml`, you already know it, and the setup link carries
it: open the link, create your account, done. Without it, read the token with:

```sh
docker compose exec clavis clavis setup-token
```

**Owner email.** When set, only that address can create the owner account,
and the setup page fills it in for you.

## After setup

1. Install the CLI: `brew install dumhouse/tap/clavis` (other systems:
   [Download](./download)).
2. `clavis login --server <your base URL>`
3. In your repository: `clavis init`, then `clavis run npm run dev`.

All settings are listed in [Self-hosting](./self-hosting), and every HTTPS
option is explained in [HTTPS and certificates](./https).
