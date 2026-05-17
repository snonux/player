let _onChange = () => {};
let _debounceTimer = null;
let _helpTimer = null;

export function initSearch({ onChange, input, clearBtn }) {
  _onChange = onChange;
  if (!input) return;
  input.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' || e.key === 'Enter') {
      e.preventDefault();
      e.stopPropagation();
      input.blur();
      hideSearch();
      document.dispatchEvent(new CustomEvent('search:navigate-results'));
    }
  });
  input.addEventListener('input', () => {
    clearTimeout(_debounceTimer);
    _debounceTimer = setTimeout(() => trigger(input.value.trim()), 300);
  });
  input.addEventListener('blur', () => {
    hideSearchHelp();
  });
  if (clearBtn) {
    clearBtn.addEventListener('click', () => {
      input.value = '';
      input.focus();
      trigger('');
    });
  }
}

export function focusSearch() {
  showSearch();
  const input = document.getElementById('search-input');
  if (input) {
    input.focus();
    input.select();
  }
}

export function trigger(query) {
  clearTimeout(_debounceTimer);
  _onChange(query);
}

export function showSearch() {
  document.getElementById('search-overlay')?.classList.remove('hidden');
}

export function hideSearch() {
  document.getElementById('search-overlay')?.classList.add('hidden');
  hideSearchHelp();
}

/**
 * Parse a raw search string into structured filters and free-text search.
 *
 * Supported syntax:
 *   min:30        – minimum duration in minutes
 *   max:55        – maximum duration in minutes
 *   tag:a,b       – comma-separated tag names
 *   like:1        – only favorited items
 *   type:video    – media type (video | audio)
 *   set:yoga      – select a set by id or name
 *   sort:name     – sort order (name | date | duration | play_count | random)
 *   minsize:10    – minimum file size in MB
 *   maxsize:500   – maximum file size in MB
 *
 * Anything that does not match key:value becomes free-text search.
 */
export function parseQuery(raw) {
  const filters = {
    type: '',
    favorites: false,
    tags: '',
    sort: '',
    set: '',
    minDuration: '',
    maxDuration: '',
    minFileSize: '',
    maxFileSize: '',
    search: '',
  };
  const tokens = [];
  const parts = tokenize(raw);
  for (const part of parts) {
    const m = part.match(/^([a-zA-Z_]+):(.+)$/);
    if (m) {
      const key = m[1].toLowerCase();
      let value = m[2];
      if (value.startsWith('"') && value.endsWith('"')) {
        value = value.slice(1, -1);
      }
      switch (key) {
        case 'min':
          filters.minDuration = value;
          break;
        case 'max':
          filters.maxDuration = value;
          break;
        case 'tag':
          filters.tags = value;
          break;
        case 'like':
          filters.favorites = ['1', 'true', 'yes'].includes(value);
          break;
        case 'type':
          filters.type = value;
          break;
        case 'set':
          filters.set = value;
          break;
        case 'sort':
          filters.sort = value;
          break;
        case 'minsize':
          filters.minFileSize = value;
          break;
        case 'maxsize':
          filters.maxFileSize = value;
          break;
        default:
          tokens.push(part);
          break;
      }
    } else {
      tokens.push(part);
    }
  }
  filters.search = tokens.join(' ').trim();
  return filters;
}

function tokenize(raw) {
  const parts = [];
  let current = '';
  let inQuote = false;
  for (const ch of raw || '') {
    if (ch === '"') {
      inQuote = !inQuote;
      continue;
    }
    if (/\s/.test(ch) && !inQuote) {
      if (current) {
        parts.push(current);
        current = '';
      }
      continue;
    }
    current += ch;
  }
  if (current) parts.push(current);
  return parts;
}

export function showSearchHelp() {
  const el = document.getElementById('search-help');
  if (!el) return;
  if (el.classList.contains('hidden')) {
    el.classList.remove('hidden');
    clearTimeout(_helpTimer);
    _helpTimer = setTimeout(() => el.classList.add('hidden'), 5000);
  } else {
    el.classList.add('hidden');
    clearTimeout(_helpTimer);
  }
}

function hideSearchHelp() {
  const el = document.getElementById('search-help');
  if (!el) return;
  el.classList.add('hidden');
  clearTimeout(_helpTimer);
}
