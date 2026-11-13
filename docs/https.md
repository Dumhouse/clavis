---
description: "Serve Clavis over HTTPS: a self-signed certificate for a home lab, your own certificate files, automatic Let's Encrypt (also on a private network), or a reverse proxy."
---

# HTTPS and certificates

Clavis protects sign-in with secure cookies, and browsers only keep those over
HTTPS or on `localhost`. Passkeys have the same rule. So as soon as you open
Clavis from another machine, give it HTTPS.

There are two ways:

1. **Clavis does HTTPS itself.** Set `CLAVIS_TLS` to one of the modes below.
2. **A reverse proxy does HTTPS** (Caddy, nginx, Traefik, Cloudflare). Clavis
   stays on plain HTTP behind it. See [Behind a reverse proxy](#behind-a-reverse-proxy).

Not sure? The [install wizard](./install) asks a few questions and writes the
compose file for you.

## Pick a mode

| `CLAVIS_TLS` | Name in the wizard | Use it when | Browser warning | Passkeys |
| --- | --- | --- | --- | --- |
| `off` (default) | No HTTPS (HTTP only) | Trying it on `localhost`, or behind a reverse proxy | none | on `localhost` and behind an HTTPS proxy |
| `self-signed` | Self-signed certificate | Home lab or office network, reached by IP or local name | once, until you trust the certificate | after you trust it, with a name (not an IP) |
| `file` | My own certificate files | You already have a certificate (your own CA, certbot, a wildcard) | none if the CA is trusted | yes |
| `acme` | Let's Encrypt (automatic) | A public domain pointing at this server | none | yes |
| `acme` + `CLAVIS_ACME_DNS` | Let's Encrypt via DNS | Your own domain, but the server is only on your LAN | none | yes |

With any mode other than `off`:

- Clavis serves HTTPS on container port **8443** and plain HTTP on **8080**.
  The HTTP port only redirects to HTTPS (and answers `/healthz` and
  `/readyz`, so health checks need no certificate).
- `CLAVIS_SERVER_BASE_URL` must start with `https://`. Clavis refuses to start
  otherwise and tells you the value to use.
- Publish both ports. The usual choice is the standard ports on the host:
  `-p 80:8080 -p 443:8443`.

## Self-signed certificate (home lab)

```sh
docker run -d --name clavis --restart unless-stopped \
  -p 80:8080 -p 443:8443 \
  -v clavis-data:/data \
  -e CLAVIS_TLS=self-signed \
  -e CLAVIS_SERVER_BASE_URL=https://192.168.1.20 \
  dumhouse/clavis:latest
```

Clavis creates a certificate for the base URL host (plus `localhost` and
`127.0.0.1`), stores it in `/data/tls/`, and renews it 30 days before it
expires. If you change the base URL host, it makes a new one on the next
start. The log line `https enabled` shows the certificate's SHA-256
fingerprint, so you can compare it with what your browser shows.

### The CLI: trust on first use

Nobody has to copy certificate files to use the CLI. The first time
`clavis login` meets the server, it shows the certificate and asks:

```text
This computer does not trust the certificate of https://192.168.1.20.
That is normal for a Clavis server with a self-signed certificate.

  Server        https://192.168.1.20
  Issued to     192.168.1.20, localhost, 127.0.0.1, ::1
  Issued by     itself (self-signed)
  Valid until   2027-12-15
  SHA-256       9E:1C:4B:…:F9

Compare the SHA-256 with the one the server shows: on the server run
  docker exec clavis clavis admin tls-info
or open the server in a browser and view the certificate details.

? Do the fingerprints match, and do you trust this server? [y/N]
```

The answer is saved for this server only, pinned to the server's public key.
Clavis keeps that key when it renews the certificate each year or when you
change the base URL host, so people are asked **once**. If the key ever
changes (new volume, someone in the middle), the CLI stops with a warning and
asks you to type `trust` to accept the new key.

Get the fingerprint to share with your team on the server:

```sh
docker exec clavis clavis admin tls-info
```

It is also in the `https enabled` log line. Put it in your onboarding notes,
so people can check it, or skip the question altogether:

```sh
clavis trust add https://192.168.1.20 --fingerprint 9E:1C:4B:…:F9
```

For CI and containers, nothing needs to be saved:

| Variable | Effect |
| --- | --- |
| `CLAVIS_SERVER_FINGERPRINT` | Trust a certificate with this SHA-256 (of the certificate or of its public key) |
| `CLAVIS_CA_CERT` | PEM file with your own CA (for example from step-ca or a company CA), added to the system roots |
| `CLAVIS_TRUST_FILE` | Where trusted servers are stored, instead of `trusted-servers.json` in the config folder |

`clavis trust ls` lists the trusted servers and `clavis trust rm` forgets one.
See [`clavis trust`](./reference/trust).

### Browsers: trust the certificate

Browsers show a warning until you trust the certificate. You can click
through it (*Advanced → Proceed*), but then passkeys stay off. To trust it,
copy the certificate out of the container:

```sh
docker cp clavis:/data/tls/self-signed.crt ./clavis.crt
```

Then add it as trusted:

- **macOS:** open `clavis.crt`, add it to the *System* keychain, open it in
  Keychain Access and set *When using this certificate* to *Always Trust*.
- **Windows:** double-click `clavis.crt`, *Install Certificate*, *Local
  Machine*, store *Trusted Root Certification Authorities*.
- **Linux (Debian, Ubuntu):** `sudo cp clavis.crt /usr/local/share/ca-certificates/ && sudo update-ca-certificates`.
  Firefox keeps its own list: *Settings → Privacy & Security → Certificates →
  View Certificates → Import*.
- **iPhone, iPad:** send the file to the device, install the profile, then
  enable it in *Settings → General → About → Certificate Trust Settings*.
- **Android:** *Settings → Security → Encryption & credentials → Install a
  certificate → CA certificate*.

A system-trusted certificate is also trusted by the CLI, so the first-use
question doesn't appear.

For more than a few people, doing this on every device gets old. If you own a
domain, [Let's Encrypt via DNS](#let-s-encrypt-on-a-private-network) gives
the LAN server a certificate every device already trusts.

::: tip Passkeys in a home lab
Browsers only allow passkeys for a host **name** with a **trusted**
certificate. Give the server a local DNS name (for example in your router or
Pi-hole: `clavis.home.arpa → 192.168.1.20`), use that name in the base URL, and
trust the certificate. Passwords work in every case.
:::

## Your own certificate files

```sh
docker run -d --name clavis --restart unless-stopped \
  -p 80:8080 -p 443:8443 \
  -v clavis-data:/data \
  -v /etc/clavis/certs:/certs:ro \
  -e CLAVIS_TLS=file \
  -e CLAVIS_TLS_CERT_FILE=/certs/fullchain.pem \
  -e CLAVIS_TLS_KEY_FILE=/certs/privkey.pem \
  -e CLAVIS_SERVER_BASE_URL=https://clavis.example.com \
  dumhouse/clavis:latest
```

The certificate file should contain the full chain. Clavis checks the files
every 30 seconds and picks up a renewed certificate without a restart. The
container runs as user `65532`, so the files must be readable by that user.

## Let's Encrypt (automatic)

```sh
docker run -d --name clavis --restart unless-stopped \
  -p 80:8080 -p 443:8443 \
  -v clavis-data:/data \
  -e CLAVIS_TLS=acme \
  -e CLAVIS_SERVER_BASE_URL=https://clavis.example.com \
  -e CLAVIS_ACME_EMAIL=you@example.com \
  dumhouse/clavis:latest
```

Requirements:

- `clavis.example.com` resolves to this server's public IP.
- Ports **80 and 443** on that IP reach the container (open them in the
  firewall and forward them on your router).

Clavis requests the certificate on the first HTTPS visit and renews it
automatically. Certificates and the account key live in `/data/tls/acme`, so
keep the volume. By default the domain is the base URL host; to cover more
names set `CLAVIS_ACME_DOMAINS=clavis.example.com,secrets.example.com`. To test
against the Let's Encrypt staging service, set
`CLAVIS_ACME_DIRECTORY=https://acme-staging-v02.api.letsencrypt.org/directory`.

::: info Domain only on your LAN?
The setup above needs the server to be reachable from the internet. For a
private server, use the DNS challenge below.
:::

## Let's Encrypt on a private network

With the DNS challenge, Let's Encrypt checks a temporary DNS record instead
of connecting to the server. The server needs outgoing internet access, but
nothing has to reach it from outside, and no port has to be forwarded. Every
browser and CLI then trusts the certificate with no setup on any device.

You need:

1. A domain at a supported DNS provider: **Cloudflare**, or a free
   **DuckDNS** subdomain.
2. A name for the server that resolves to its LAN address, either a DNS
   record at the provider (`clavis.home.example.com → 192.168.1.20`, an A record with
   a private IP is fine) or an entry in your router or Pi-hole.
3. An API token for the provider.

**Cloudflare.** Create a token at *My Profile → API Tokens → Create Token →
Edit zone DNS*, limited to your zone:

```sh
docker run -d --name clavis --restart unless-stopped \
  -p 80:8080 -p 443:8443 \
  -v clavis-data:/data \
  -e CLAVIS_TLS=acme \
  -e CLAVIS_ACME_DNS=cloudflare \
  -e CLAVIS_ACME_DNS_TOKEN=your-cloudflare-token \
  -e CLAVIS_SERVER_BASE_URL=https://clavis.home.example.com \
  -e CLAVIS_ACME_EMAIL=you@example.com \
  dumhouse/clavis:latest
```

**DuckDNS.** Use your account token from duckdns.org and a name under your
subdomain:

```sh
  -e CLAVIS_ACME_DNS=duckdns \
  -e CLAVIS_ACME_DNS_TOKEN=your-duckdns-token \
  -e CLAVIS_SERVER_BASE_URL=https://myhome.duckdns.org \
```

What happens, and the edge cases:

- **Right after start**, before the certificate is issued, Clavis serves a
  temporary self-signed certificate so it is never down. Issuance usually
  takes a minute; `clavis admin tls-info` shows whether it's done. The CLI
  won't trust the temporary certificate: it asks people to try again in a
  minute, so nobody is left with a stale pin.
- **If issuance fails** (wrong token, rate limit, no internet), Clavis logs
  the reason and retries after 1, 5, 15 and then every 60 minutes. A token
  without permission is reported as such.
- **Renewal** happens 30 days before expiry, in the background, with the same
  retries. The current certificate keeps working meanwhile.
- **Propagation:** Clavis waits until the zone's own nameservers return the
  record (up to `CLAVIS_ACME_DNS_WAIT`, default `3m`) before asking Let's
  Encrypt to check. This works with split DNS, where your LAN resolver doesn't
  see the public zone. If your network blocks outgoing DNS, set
  `CLAVIS_ACME_DNS_RESOLVERS=1.1.1.1:53,8.8.8.8:53`.
- **Wildcards** (`CLAVIS_ACME_DOMAINS=*.home.example.com`) work with the DNS
  challenge only.
- **Docker secrets:** use `CLAVIS_ACME_DNS_TOKEN_FILE=/run/secrets/cf_token`
  instead of putting the token in the environment.
- **Testing:** set `CLAVIS_ACME_DIRECTORY=https://acme-staging-v02.api.letsencrypt.org/directory`
  to avoid the production rate limits while you experiment.
- IP addresses can't get a Let's Encrypt certificate; Clavis refuses to start
  with a clear message if the base URL is an IP.

## Behind a reverse proxy

Keep `CLAVIS_TLS=off`, publish Clavis only to the proxy, and set the public
URL:

```yaml
services:
  clavis:
    image: dumhouse/clavis:latest
    container_name: clavis
    restart: unless-stopped
    ports:
      - "127.0.0.1:8080:8080"
    environment:
      CLAVIS_SERVER_BASE_URL: "https://clavis.example.com"
      CLAVIS_TRUSTED_PROXIES: "172.16.0.0/12"
    volumes:
      - clavis-data:/data

volumes:
  clavis-data:
```

`CLAVIS_TRUSTED_PROXIES` lets Clavis read the client IP from
`X-Forwarded-For` for audit logs and rate limits. Set it to the address your
proxy connects from (`172.16.0.0/12` covers Docker's default networks).

**Caddy** (automatic HTTPS):

```
clavis.example.com {
  reverse_proxy 127.0.0.1:8080
}
```

**nginx:**

```nginx
server {
  listen 443 ssl http2;
  server_name clavis.example.com;
  ssl_certificate     /etc/letsencrypt/live/clavis.example.com/fullchain.pem;
  ssl_certificate_key /etc/letsencrypt/live/clavis.example.com/privkey.pem;

  location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto https;
    # live updates use server-sent events
    proxy_buffering off;
    proxy_read_timeout 1h;
  }
}
```

**Traefik** labels:

```yaml
labels:
  - traefik.enable=true
  - traefik.http.routers.clavis.rule=Host(`clavis.example.com`)
  - traefik.http.routers.clavis.tls.certresolver=letsencrypt
  - traefik.http.services.clavis.loadbalancer.server.port=8080
```

## Plain HTTP on a network address

If you open Clavis at `http://<ip>` (no HTTPS at all), Clavis falls back to
cookies without the `Secure` flag so that sign-in still works, and logs a
warning. Your password and secrets then cross the network unencrypted and
passkeys are unavailable. Use it only for a short test on a trusted network.

## Settings reference

| Variable | Default | Meaning |
| --- | --- | --- |
| `CLAVIS_TLS` | `off` | `off`, `self-signed`, `file`, or `acme` |
| `CLAVIS_SERVER_BASE_URL` | `http://localhost:8080` | Public URL. Must be `https://` with any TLS mode |
| `CLAVIS_SERVER_ADDR` | `:8080` | Plain-HTTP listener (redirects when TLS is on) |
| `CLAVIS_TLS_ADDR` | `:8443` | HTTPS listener |
| `CLAVIS_TLS_DIR` | `<data-dir>/tls` | Generated and Let's Encrypt certificates |
| `CLAVIS_TLS_CERT_FILE`, `CLAVIS_TLS_KEY_FILE` | | Certificate and key for `file` |
| `CLAVIS_ACME_EMAIL` | | Contact for Let's Encrypt expiry notices |
| `CLAVIS_ACME_DOMAINS` | base URL host | Comma-separated names for the certificate |
| `CLAVIS_ACME_DIRECTORY` | Let's Encrypt production | Another ACME server, for example staging |
| `CLAVIS_ACME_DNS` | | `cloudflare` or `duckdns`: use the DNS challenge |
| `CLAVIS_ACME_DNS_TOKEN` | | API token for the DNS provider |
| `CLAVIS_ACME_DNS_TOKEN_FILE` | | Read the token from a file instead |
| `CLAVIS_ACME_DNS_RESOLVERS` | the zone's nameservers | Comma-separated `host:port` list to check the record on |
| `CLAVIS_ACME_DNS_WAIT` | `3m` | How long to wait for the record to appear |

The same settings exist in `clavis.yaml` under `server.tls` (`mode`, `addr`,
`dir`, `cert_file`, `key_file`, `acme_email`, `acme_domains`,
`acme_directory`, `acme_dns`, `acme_dns_token`, `acme_dns_token_file`,
`acme_dns_resolvers`, `acme_dns_wait`).
