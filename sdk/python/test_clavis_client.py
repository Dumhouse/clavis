import json
import os
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer

import clavis_client


class Handler(BaseHTTPRequestHandler):
    seen = {}

    def do_GET(self):  # noqa: N802
        Handler.seen = {"path": self.path, "auth": self.headers.get("Authorization")}
        if self.headers.get("Authorization") != "Bearer good":
            self.send_response(403)
            self.end_headers()
            return
        body = json.dumps({"secrets": [{"key": "PY_A", "value": "1"}, {"key": "PY_B", "value": "two"}]}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass


class ClientTest(unittest.TestCase):
    def setUp(self):
        self.server = HTTPServer(("127.0.0.1", 0), Handler)
        threading.Thread(target=self.server.serve_forever, daemon=True).start()
        self.url = "http://127.0.0.1:%d" % self.server.server_port

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        for key in ("PY_A", "PY_B"):
            os.environ.pop(key, None)

    def test_fetches_with_the_bearer_token(self):
        secrets = clavis_client.fetch_secrets(self.url, "good", "acme", "web", "dev")
        self.assertEqual(secrets, {"PY_A": "1", "PY_B": "two"})
        self.assertEqual(Handler.seen["auth"], "Bearer good")
        self.assertEqual(Handler.seen["path"],
                         "/api/v1/orgs/acme/projects/web/environments/dev/secrets:resolve?channel=api")

    def test_load_keeps_values_already_set(self):
        os.environ["PY_A"] = "from-shell"
        names = clavis_client.load_secrets(server=self.url, token="good", org="o", project="p")
        self.assertEqual(names, ["PY_B"])
        self.assertEqual(os.environ["PY_A"], "from-shell")
        clavis_client.load_secrets(override=True, server=self.url, token="good", org="o", project="p")
        self.assertEqual(os.environ["PY_A"], "1")

    def test_errors_say_what_went_wrong(self):
        with self.assertRaisesRegex(clavis_client.ClavisError, "missing token"):
            clavis_client.fetch_secrets(self.url, None, "o", "p")
        with self.assertRaisesRegex(clavis_client.ClavisError, "403"):
            clavis_client.fetch_secrets(self.url, "bad", "o", "p")


if __name__ == "__main__":
    unittest.main()
