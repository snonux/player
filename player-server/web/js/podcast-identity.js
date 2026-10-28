// Account wire identity, byte-compatible with internal/podcast/identity.go.
// Do not use URL(): it rewrites IDN hosts, dot segments and path backslashes.
// Device-only IndexedDB keys are a separate existing format and stay unchanged.
const asciiLower = value => value.replace(/[A-Z]/g, c => c.toLowerCase());
const invalid = () => { throw new Error('Enter a valid podcast feed URL.'); };
const encoder = new TextEncoder();

function splitScheme(value, unwrap = true) {
  const match = /^([A-Za-z][A-Za-z0-9+.-]*):(.*)$/s.exec(value);
  if (!match) return invalid();
  let scheme = asciiLower(match[1]);
  const rest = match[2];
  if (['feed', 'itpc', 'pcast', 'podcast'].includes(scheme)) {
    if (!unwrap) return invalid();
    const inner = rest.startsWith('//') ? rest.slice(2) : rest;
    if (/^[A-Za-z][A-Za-z0-9+.-]*:\/\//.test(inner)) return splitScheme(inner, false);
    scheme = 'http';
  }
  if (scheme !== 'http' && scheme !== 'https') return invalid();
  return { scheme, rest };
}

function canonicalAuthority(authority, scheme) {
  if (authority.includes('@') || authority.includes('\\')) return invalid();
  let host, port = '';
  if (authority.startsWith('[')) {
    const end = authority.indexOf(']');
    if (end < 0) return invalid();
    host = authority.slice(0, end + 1);
    const suffix = authority.slice(end + 1);
    if (suffix && !suffix.startsWith(':')) return invalid();
    port = suffix.slice(1);
  } else {
    const colon = authority.indexOf(':');
    host = colon < 0 ? authority : authority.slice(0, colon);
    port = colon < 0 ? '' : authority.slice(colon + 1);
  }
  host = asciiLower(host).replace(/\.$/, '');
  if (!host || host === '[]') return invalid();
  if (!port) return host;
  if (!/^[0-9]+$/.test(port)) return invalid();
  const number = Number(port);
  if (!Number.isInteger(number) || number < 1 || number > 65535) return invalid();
  if ((scheme === 'http' && number === 80) || (scheme === 'https' && number === 443)) return host;
  return `${host}:${number}`;
}

function escapeIdentity(value) {
  let output = '';
  for (const byte of encoder.encode(value)) {
    output += byte >= 128 ? `%${byte.toString(16).toUpperCase().padStart(2, '0')}` : String.fromCharCode(byte);
  }
  return output.replace(/%[a-fA-F0-9]{2}/g, escape => escape.toUpperCase());
}

export function podcastFeedKey(raw) {
  if (typeof raw !== 'string') return invalid();
  const value = raw.replace(/^[ \t\r\n\f\v]+|[ \t\r\n\f\v]+$/g, '');
  const bytes = encoder.encode(value);
  // Lone UTF-16 surrogates have no UTF-8 identity; TextEncoder replaces them.
  if (!bytes.length || bytes.length > 2048 || /[\x00-\x20\x7f]/.test(value)
      || new TextDecoder().decode(bytes) !== value) return invalid();
  const { scheme, rest } = splitScheme(value);
  if (!rest.startsWith('//')) return invalid();
  const location = rest.slice(2);
  const boundary = location.search(/[/?#]/);
  const authority = canonicalAuthority(boundary < 0 ? location : location.slice(0, boundary), scheme);
  const tail = (boundary < 0 ? '' : location.slice(boundary)).split('#', 1)[0];
  const question = tail.indexOf('?');
  const path = (question < 0 ? tail : tail.slice(0, question)) || '/';
  const query = question < 0 ? '' : tail.slice(question + 1);
  return `feed:${authority}${escapeIdentity(path)}${query ? `?${escapeIdentity(query)}` : ''}`;
}
