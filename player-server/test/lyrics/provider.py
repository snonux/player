#!/usr/bin/env python3
"""Deterministic localhost lyric provider for A03c installed-client checks."""

import argparse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
from urllib.parse import parse_qs, urlsplit


LYRICS = {
    'language': 'en', 'synced': True,
    'lines': [
        {'text': 'Provider sky', 'start': 1, 'end': 4, 'voice': 'Lead',
         'words': [{'text': 'Provider ', 'start': 1, 'end': 2},
                   {'text': 'sky', 'start': 2, 'end': 4}],
         'translations': {'fr': 'Ciel du fournisseur'}},
        {'text': 'Second line', 'start': 5, 'end': 8},
    ],
}


def serve(port, mode_file, calls_file):
    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            query = parse_qs(urlsplit(self.path).query)
            with calls_file.open('a') as calls:
                calls.write(json.dumps({'path': urlsplit(self.path).path,
                                        'title': query.get('title'),
                                        'artist': query.get('artist'),
                                        'authorization': bool(self.headers.get('Authorization')),
                                        'cookie': bool(self.headers.get('Cookie'))}) + '\n')
            mode = mode_file.read_text().strip() if mode_file.exists() else 'ready'
            if mode == 'error':
                self.send_error(503, 'fixture provider error')
                return
            if mode == 'empty':
                self.send_response(204)
                self.end_headers()
                return
            if query.get('title') != ['Provider Sky'] or query.get('artist') != ['Fixture Artist']:
                self.send_response(204)
                self.end_headers()
                return
            body = json.dumps(LYRICS).encode()
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, *_):
            pass

    ThreadingHTTPServer(('127.0.0.1', port), Handler).serve_forever()


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--port', type=int, required=True)
    parser.add_argument('--mode-file', type=Path, required=True)
    parser.add_argument('--calls-file', type=Path, required=True)
    args = parser.parse_args()
    serve(args.port, args.mode_file, args.calls_file)
