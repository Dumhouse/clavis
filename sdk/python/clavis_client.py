"""A tiny Clavis client for Python 3.8+ (standard library only).

Fetch one environment's secrets at start-up, so an app does not need the CLI::

    import clavis_client
    clavis_client.load_secrets()   # fills os.environ from CLAVIS_* settings

Settings come from the arguments or from CLAVIS_SERVER, CLAVIS_TOKEN,
CLAVIS_ORG, CLAVIS_PROJECT and CLAVIS_ENV. The token needs the
secrets:read-values permission for the environment. Reads are audited.
"""

import json
import os
import urllib.error
import urllib.parse
import urllib.request

__all__ = ["fetch_secrets", "load_secrets", "ClavisError"]


class ClavisError(Exception):
    """The server could not be reached, or refused the request."""


def fetch_secrets(server=None, token=None, org=None, project=None, env=None, timeout=10):
    """Return an environment's secrets as a dict of NAME -> value."""
    settings = {
        "server": (server or os.environ.get("CLAVIS_SERVER") or "").rstrip("/"),
        "token": token or os.environ.get("CLAVIS_TOKEN"),
        "org": org or os.environ.get("CLAVIS_ORG"),
        "project": project or os.environ.get("CLAVIS_PROJECT"),
    }
    for name, value in settings.items():
        if not value:
            raise ClavisError("missing %s (pass it or set CLAVIS_%s)" % (name, name.upper()))
    environment = env or os.environ.get("CLAVIS_ENV") or "dev"
    quote = lambda part: urllib.parse.quote(part, safe="")  # noqa: E731
    url = "%s/api/v1/orgs/%s/projects/%s/environments/%s/secrets:resolve?channel=api" % (
        settings["server"], quote(settings["org"]), quote(settings["project"]), quote(environment))
    request = urllib.request.Request(url, headers={
        "Authorization": "Bearer " + settings["token"],
        "User-Agent": "clavis-python-sdk",
    })
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:  # noqa: S310 - the operator's own server
            body = json.load(response)
    except urllib.error.HTTPError as error:
        raise ClavisError("the server answered %d %s" % (error.code, error.reason)) from None
    except urllib.error.URLError as error:
        raise ClavisError("could not reach the server: %s" % error.reason) from None
    return {item["key"]: item.get("value") or "" for item in body.get("secrets", [])}


def load_secrets(override=False, **options):
    """Fetch the secrets and put them in os.environ.

    Values already set in the environment win unless ``override`` is true.
    Returns the names that were set.
    """
    names = []
    for key, value in fetch_secrets(**options).items():
        if override or key not in os.environ:
            os.environ[key] = value
            names.append(key)
    return names
