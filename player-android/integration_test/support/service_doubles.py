#!/usr/bin/env python3
"""Deterministic local doubles of external music services, on loopback.

Future music features talk to services the test laptop must not depend on:
scrobbling (ListenBrainz-compatible, M44), artist/album metadata (a
Last.fm-compatible artist.getinfo, M10/M41), internet radio streams (M61) and
UPnP/DLNA renderers (M59). This server stands in for them with fixed answers
and records every request, so a test can assert *what the client sent*.

A double is never proof of a real integration. Every response carries
`X-Player-Service-Double: <service>`, `/_double/info` states it, and runners
must label evidence produced against a double with proof "service-double"
(see evidence.py). Accepting a feature that integrates with a real service
still needs a check against that service or a compatible real program.

Endpoints (all below one origin):

  GET  /listenbrainz/1/validate-token      Authorization: Token <token>
  POST /listenbrainz/1/submit-listens      ListenBrainz JSON payload
  GET  /lastfm/2.0/?method=artist.getinfo&artist=<name>&format=json
  GET  /radio/fixture.mp3                  endless ICY-style MP3 stream
  GET  /radio/down.mp3                     503: a station that is off the air
  GET  /upnp/description.xml               MediaRenderer device description
  POST /upnp/control/AVTransport           SOAP actions (recorded)
  GET  /_double/info | /_double/requests   description and request log
  POST /_double/reset                      clear the request log

Not doubled: SSDP multicast discovery (renderers are addressed by their
description URL) and ICY in-stream metadata (only the icy-* headers).

Usage: service_doubles.py [--port 18767] [--radio-source FILE.mp3]
"""

import argparse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
import threading
import time
import urllib.parse

VALID_TOKEN = 'fixture-listenbrainz-token'
LISTEN_TYPES = {'single', 'playing_now', 'import'}
RADIO_CHUNK = 4096
# Pace the radio stream at roughly 128 kbit/s so a client buffers as it
# would for a real station instead of receiving the loop as fast as possible.
RADIO_BYTES_PER_SECOND = 16000

# Canned artist biographies keyed by the fixture's artist names. Names that
# are not listed answer with Last.fm's "artist not found" error 6.
ARTISTS = {
    'Aurora Lane': {'bio': 'Aurora Lane is a fictional synthwave act made for Player tests.',
                    'similar': ['Neon Coast', 'Glass Avenue'], 'tags': ['synthwave', 'electronic']},
    'Neon Coast': {'bio': 'Neon Coast is a fictional act on the Synth Compilation.',
                   'similar': ['Aurora Lane'], 'tags': ['synthwave']},
    'Ünïcødé Artïst': {'bio': 'Ünïcødé Artïst tests non-ASCII names end to end.',
                       'similar': [], 'tags': ['électronique']},
}

DEVICE_DESCRIPTION = """<?xml version="1.0" encoding="utf-8"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
 <specVersion><major>1</major><minor>0</minor></specVersion>
 <device>
  <deviceType>urn:schemas-upnp-org:device:MediaRenderer:1</deviceType>
  <friendlyName>Player Test Renderer (double)</friendlyName>
  <manufacturer>Player E2E fixture</manufacturer>
  <modelName>service-double</modelName>
  <UDN>uuid:00000000-0000-4000-8000-0000706c6179</UDN>
  <serviceList><service>
   <serviceType>urn:schemas-upnp-org:service:AVTransport:1</serviceType>
   <serviceId>urn:upnp-org:serviceId:AVTransport</serviceId>
   <controlURL>/upnp/control/AVTransport</controlURL>
   <eventSubURL>/upnp/event/AVTransport</eventSubURL>
   <SCPDURL>/upnp/AVTransport.xml</SCPDURL>
  </service></serviceList>
 </device>
</root>
"""

SOAP_RESPONSE = """<?xml version="1.0" encoding="utf-8"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"
 s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>
<u:{action}Response xmlns:u="urn:schemas-upnp-org:service:AVTransport:1">{body}</u:{action}Response>
</s:Body></s:Envelope>
"""


class Recorder:
    """Thread-safe request log plus the renderer's transport state."""

    def __init__(self):
        self.lock = threading.Lock()
        self.requests = []
        self.transport = {'state': 'NO_MEDIA_PRESENT', 'uri': ''}

    def add(self, service, method, path, headers, body):
        entry = {'time': time.time(), 'service': service, 'method': method, 'path': path,
                 'headers': headers, 'body': body}
        with self.lock:
            self.requests.append(entry)

    def snapshot(self):
        with self.lock:
            return list(self.requests)

    def reset(self):
        with self.lock:
            self.requests.clear()
            self.transport = {'state': 'NO_MEDIA_PRESENT', 'uri': ''}


class Handler(BaseHTTPRequestHandler):
    """Serves every double; make_handler() binds recorder and radio_source."""

    server_version = 'PlayerServiceDouble/1'
    recorder = None
    radio_source = None

    def log_message(self, *_):
        # The request log endpoint is the record; keep stderr quiet.
        pass

    def do_GET(self):
        self._dispatch('GET')

    def do_POST(self):
        self._dispatch('POST')

    def _dispatch(self, method):
        url = urllib.parse.urlsplit(self.path)
        route = ROUTES.get((method, url.path))
        if route is None:
            return self._send('unknown', 404, {'error': 'no such double endpoint'})
        body = self._read_body() if method == 'POST' else None
        service = url.path.strip('/').split('/')[0]
        if not url.path.startswith('/_double/'):
            headers = {k: v for k, v in self.headers.items()
                       if k.lower() in ('authorization', 'content-type', 'soapaction',
                                        'icy-metadata', 'user-agent')}
            self.recorder.add(service, method, self.path, headers, body)
        route(self, urllib.parse.parse_qs(url.query), body)

    def _read_body(self):
        length = int(self.headers.get('Content-Length') or 0)
        raw = self.rfile.read(length) if length else b''
        try:
            return json.loads(raw) if raw else None
        except ValueError:
            return raw.decode(errors='replace')

    def _send(self, service, status, payload, content_type='application/json', extra=None):
        data = payload if isinstance(payload, bytes) else (
            payload.encode() if isinstance(payload, str) else json.dumps(payload).encode())
        self.send_response(status)
        self.send_header('Content-Type', content_type)
        self.send_header('Content-Length', str(len(data)))
        self.send_header('X-Player-Service-Double', service)
        for key, value in (extra or {}).items():
            self.send_header(key, value)
        self.end_headers()
        self.wfile.write(data)

    # -- ListenBrainz -------------------------------------------------
    def validate_token(self, _query, _body):
        valid = self.headers.get('Authorization', '') == f'Token {VALID_TOKEN}'
        payload = {'code': 200, 'message': 'Token valid.' if valid else 'Token invalid.',
                   'valid': valid}
        if valid:
            payload['user_name'] = 'fixture-listener'
        self._send('listenbrainz', 200, payload)

    def submit_listens(self, _query, body):
        if self.headers.get('Authorization', '') != f'Token {VALID_TOKEN}':
            return self._send('listenbrainz', 401, {'code': 401, 'error': 'Invalid authorization token.'})
        problem = listen_payload_problem(body)
        if problem:
            return self._send('listenbrainz', 400, {'code': 400, 'error': problem})
        self._send('listenbrainz', 200, {'status': 'ok'})

    # -- Last.fm-compatible metadata ------------------------------------
    def lastfm(self, query, _body):
        method = (query.get('method') or [''])[0].lower()
        name = (query.get('artist') or [''])[0]
        if method != 'artist.getinfo':
            return self._send('lastfm', 400, {'error': 3, 'message': 'Invalid Method'})
        artist = ARTISTS.get(name)
        if artist is None:
            return self._send('lastfm', 404, {'error': 6, 'message': 'The artist you supplied could not be found'})
        self._send('lastfm', 200, {'artist': {
            'name': name, 'bio': {'summary': artist['bio'], 'content': artist['bio']},
            'similar': {'artist': [{'name': n} for n in artist['similar']]},
            'tags': {'tag': [{'name': t} for t in artist['tags']]}}})

    # -- Internet radio -------------------------------------------------
    def radio(self, query, _body):
        seconds = float((query.get('seconds') or ['3600'])[0])
        data = Path(self.radio_source).read_bytes()
        self.send_response(200)
        for key, value in (('Content-Type', 'audio/mpeg'), ('icy-name', 'Fixture Radio'),
                           ('icy-genre', 'Test Tones'), ('icy-br', '128'),
                           ('X-Player-Service-Double', 'radio'), ('Cache-Control', 'no-cache')):
            self.send_header(key, value)
        self.end_headers()
        stream_loop(self.wfile, data, seconds)

    def radio_down(self, _query, _body):
        self._send('radio', 503, {'error': 'station off the air'})

    # -- UPnP renderer --------------------------------------------------
    def upnp_description(self, _query, _body):
        self._send('upnp', 200, DEVICE_DESCRIPTION, 'text/xml; charset="utf-8"')

    def upnp_control(self, _query, body):
        action = (self.headers.get('SOAPAction') or '').strip('"').split('#')[-1]
        reply = renderer_action(self.recorder, action, body if isinstance(body, str) else '')
        if reply is None:
            return self._send('upnp', 500, 'Invalid Action', 'text/plain')
        self._send('upnp', 200, SOAP_RESPONSE.format(action=action, body=reply),
                   'text/xml; charset="utf-8"')

    # -- Introspection --------------------------------------------------
    def info(self, _query, _body):
        self._send('double', 200, {
            'double': True, 'proof': 'service-double',
            'note': 'Deterministic local stand-in. Not proof of integration with a real service.',
            'services': ['listenbrainz', 'lastfm', 'radio', 'upnp'],
            'listenbrainz_token': VALID_TOKEN})

    def request_log(self, _query, _body):
        self._send('double', 200, self.recorder.snapshot())

    def reset(self, _query, _body):
        self.recorder.reset()
        self._send('double', 200, {'status': 'ok'})


# Endpoint table; anything else answers 404.
ROUTES = {
    ('GET', '/listenbrainz/1/validate-token'): Handler.validate_token,
    ('POST', '/listenbrainz/1/submit-listens'): Handler.submit_listens,
    ('GET', '/lastfm/2.0/'): Handler.lastfm,
    ('GET', '/radio/fixture.mp3'): Handler.radio,
    ('GET', '/radio/down.mp3'): Handler.radio_down,
    ('GET', '/upnp/description.xml'): Handler.upnp_description,
    ('POST', '/upnp/control/AVTransport'): Handler.upnp_control,
    ('GET', '/_double/info'): Handler.info,
    ('GET', '/_double/requests'): Handler.request_log,
    ('POST', '/_double/reset'): Handler.reset,
}


def make_handler(recorder, radio_source):
    """Handler subclass bound to one recorder and radio source file."""
    return type('BoundHandler', (Handler,), {'recorder': recorder, 'radio_source': radio_source})


def listen_payload_problem(body):
    """Validate the parts of a ListenBrainz submission clients get wrong.

    Returns an error message, or None for a well-formed payload: a known
    listen_type, a non-empty payload list, artist and track names in each
    item, a listened_at timestamp except for playing_now (which must be a
    single item without one).
    """
    if not isinstance(body, dict) or body.get('listen_type') not in LISTEN_TYPES:
        return 'listen_type must be single, playing_now or import'
    items = body.get('payload')
    if not isinstance(items, list) or not items:
        return 'payload must be a non-empty list'
    if body['listen_type'] != 'import' and len(items) != 1:
        return 'single and playing_now take exactly one listen'
    for item in items:
        metadata = item.get('track_metadata') if isinstance(item, dict) else None
        if not isinstance(metadata, dict) or not metadata.get('artist_name') or not metadata.get('track_name'):
            return 'track_metadata needs artist_name and track_name'
        timed = 'listened_at' in item
        if timed == (body['listen_type'] == 'playing_now'):
            return 'listened_at is required, except for playing_now where it is not allowed'
    return None


def renderer_action(recorder, action, envelope):
    """Apply an AVTransport action to the double's state; return reply XML."""
    with recorder.lock:
        transport = recorder.transport
        if action == 'SetAVTransportURI':
            start = envelope.find('<CurrentURI>')
            end = envelope.find('</CurrentURI>')
            transport['uri'] = envelope[start + 12:end] if 0 <= start < end else ''
            transport['state'] = 'STOPPED'
            return ''
        if action in ('Play', 'Pause', 'Stop'):
            transport['state'] = {'Play': 'PLAYING', 'Pause': 'PAUSED_PLAYBACK', 'Stop': 'STOPPED'}[action]
            return ''
        if action == 'GetTransportInfo':
            return (f'<CurrentTransportState>{transport["state"]}</CurrentTransportState>'
                    '<CurrentTransportStatus>OK</CurrentTransportStatus><CurrentSpeed>1</CurrentSpeed>')
    return None


def stream_loop(output, data, seconds):
    """Write data in a loop, paced, until seconds pass or the client leaves."""
    deadline = time.monotonic() + seconds
    offset = 0
    try:
        while time.monotonic() < deadline:
            chunk = data[offset:offset + RADIO_CHUNK] or data[:RADIO_CHUNK]
            offset = (offset + len(chunk)) % len(data)
            output.write(chunk)
            time.sleep(len(chunk) / RADIO_BYTES_PER_SECOND)
    except (BrokenPipeError, ConnectionResetError):
        pass


def serve(port, radio_source):
    """Create (not start) the double server bound to loopback."""
    recorder = Recorder()
    server = ThreadingHTTPServer(('127.0.0.1', port), make_handler(recorder, radio_source))
    server.daemon_threads = True
    server.recorder = recorder
    return server


def main():
    parser = argparse.ArgumentParser(description=__doc__.split('\n\n')[0])
    parser.add_argument('--port', type=int, default=18767)
    parser.add_argument('--radio-source', type=Path, required=True,
                        help='MP3 file the radio double loops')
    args = parser.parse_args()
    server = serve(args.port, args.radio_source.resolve(strict=True))
    print(json.dumps({'origin': f'http://127.0.0.1:{args.port}', 'proof': 'service-double'}), flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


if __name__ == '__main__':
    main()
