#!/usr/bin/env python3
"""Disposable, deterministic Player JSON enrichment agent. Never production data."""
import argparse
import base64
import json
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlsplit


class Agent(BaseHTTPRequestHandler):
    def do_GET(self):
        if urlsplit(self.path).path == '/artist.png':
            image = base64.b64decode('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+ip1sAAAAASUVORK5CYII=')
            self.send_response(200)
            self.send_header('Content-Type', 'image/png')
            self.send_header('Content-Length', str(len(image)))
            self.end_headers()
            self.wfile.write(image)
            return
        query = parse_qs(urlsplit(self.path).query)
        mode = Path(self.server.mode_file).read_text().strip() if Path(self.server.mode_file).exists() else 'ready'
        if mode == 'failure':
            self.send_response(503)
            self.end_headers()
            return
        if mode == 'empty':
            self.send_response(204)
            self.end_headers()
            return
        payload = json.dumps({
            'description': f"Fixture {query.get('kind', ['artist'])[0]} description for {query.get('name', ['Unknown'])[0]}.\nThis is deterministic test metadata, not a real biography.",
            'source': 'Local deterministic fixture',
            'image_url': self.server.public_url + '/artist.png',
            'links': [{'label': 'MusicBrainz', 'url': 'https://musicbrainz.org/'}],
        }).encode()
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--port', type=int, default=18910)
    parser.add_argument('--public-url', default='http://127.0.0.1:18910')
    parser.add_argument('--mode-file', default='/tmp/player-enrichment-mode')
    args = parser.parse_args()
    server = HTTPServer(('127.0.0.1', args.port), Agent)
    server.mode_file = args.mode_file
    server.public_url = args.public_url.rstrip('/')
    server.serve_forever()
