# Copyright 2026 Michael Davis
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""A deliberately small dashboard over the events tilegroxy writes to postgres.

Serves the map at / and the dashboard at /dashboard, with /api/stats returning the numbers
behind it. Nothing here is tilegroxy specific: the analytics table is an ordinary table you
own, so any reporting tool that speaks SQL works the same way.
"""

import json
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

import psycopg
from psycopg.rows import dict_row

HERE = Path(__file__).parent
DATABASE_URL = os.environ["DATABASE_URL"]
TILE_URL = os.environ.get("TILE_URL", "http://localhost:8085/tiles/{layer}/{z}/{x}/{y}")

# Every value from the analytics table is bound rather than formatted in, the same rule tilegroxy
# follows when it writes them
QUERIES = {
    "totals": """
        SELECT count(*)                       AS tiles,
               count(DISTINCT layer)          AS layers,
               coalesce(avg((extra->>'duration')::numeric), 0)::int AS avg_duration_ms,
               coalesce(sum((extra->>'bytes')::numeric), 0)::bigint AS total_bytes
        FROM tilegroxy_analytics
    """,
    "by_layer": """
        SELECT layer,
               count(*) AS tiles,
               max(time) AS last_seen,
               coalesce(avg((extra->>'duration')::numeric), 0)::int AS avg_duration_ms
        FROM tilegroxy_analytics
        GROUP BY layer
        ORDER BY tiles DESC
    """,
    "by_zoom": """
        SELECT z, count(*) AS tiles
        FROM tilegroxy_analytics
        GROUP BY z
        ORDER BY z
    """,
    "recent": """
        SELECT time, layer, z, x, y,
               coalesce(user_id, '') AS user_id,
               extra
        FROM tilegroxy_analytics
        ORDER BY time DESC
        LIMIT 15
    """,
}


def collect_stats():
    with psycopg.connect(DATABASE_URL, row_factory=dict_row) as conn:
        with conn.cursor() as cur:
            out = {}
            for key, sql in QUERIES.items():
                cur.execute(sql)
                rows = cur.fetchall()
                out[key] = rows[0] if key == "totals" else rows
            return out


def serialize(value):
    if hasattr(value, "isoformat"):
        return value.isoformat()
    raise TypeError(f"cannot serialize {type(value)}")


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_GET(self):  # noqa: N802 - name fixed by BaseHTTPRequestHandler
        path = self.path.split("?", 1)[0]

        if path == "/api/stats":
            self.send_json()
        elif path in ("/", "/index.html"):
            self.send_page("index.html")
        elif path in ("/dashboard", "/dashboard.html"):
            self.send_page("dashboard.html")
        else:
            self.send_body(404, "text/plain; charset=utf-8", b"not found")

    def send_json(self):
        try:
            body = json.dumps(collect_stats(), default=serialize).encode()
        except psycopg.Error as err:
            body = json.dumps({"error": str(err)}).encode()
            self.send_body(503, "application/json", body)
            return

        self.send_body(200, "application/json", body)

    def send_page(self, name):
        # The tile URL is templated in so the page doesn't need its own configuration
        html = (HERE / name).read_text().replace("__TILE_URL__", TILE_URL)
        self.send_body(200, "text/html; charset=utf-8", html.encode())

    def send_body(self, status, content_type, body):
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, fmt, *args):
        print(f"{self.address_string()} {fmt % args}", flush=True)


if __name__ == "__main__":
    print("dashboard listening on http://localhost:8000", flush=True)
    ThreadingHTTPServer(("0.0.0.0", 8000), Handler).serve_forever()
