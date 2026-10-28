import assert from 'node:assert/strict';

function el(tag = 'div', props = {}) {
  const node = {
    tagName: tag.toUpperCase(),
    id: props.id || '',
    className: props.className || '',
    classList: {
      contains(name) { return (` ${node.className} `).includes(` ${name} `); },
    },
    children: [],
    parentElement: null,
    hidden: false,
    textContent: props.text || '',
    attributes: {},
    setAttribute(name, value) { this.attributes[name] = String(value); if (name === 'id') this.id = String(value); },
    getAttribute(name) { return this.attributes[name]; },
    hasAttribute(name) { return name in this.attributes; },
    removeAttribute(name) { delete this.attributes[name]; },
    append(...nodes) { for (const child of nodes) this.appendChild(child); },
    appendChild(child) {
      if (child.parentElement) child.parentElement.children = child.parentElement.children.filter(c => c !== child);
      child.parentElement = this;
      this.children.push(child);
      return child;
    },
    insertBefore(child, before) {
      if (child.parentElement) child.parentElement.children = child.parentElement.children.filter(c => c !== child);
      child.parentElement = this;
      const at = before ? this.children.indexOf(before) : -1;
      if (at < 0) this.children.push(child);
      else this.children.splice(at, 0, child);
      return child;
    },
    contains(other) { return other === this || this.children.some(c => c === other || c.contains?.(other)); },
    matches(selector) {
      if (selector === ':popover-open') return !!this._open;
      return match(this, selector);
    },
    querySelector(selector) { return this.querySelectorAll(selector)[0] || null; },
    querySelectorAll(selector) {
      const out = [];
      const visit = current => {
        for (const child of current.children || []) {
          if (match(child, selector)) out.push(child);
          visit(child);
        }
      };
      visit(this);
      return out;
    },
    addEventListener() {},
    focus() { this.focused = true; },
    closest() { return null; },
  };
  if (props.id) node.id = props.id;
  if (props.className) node.className = props.className;
  return node;
}

function match(node, selector) {
  return selector.split(',').some(part => {
    part = part.trim();
    if (!part) return false;
    if (part.startsWith('#')) return node.id === part.slice(1);
    if (part.startsWith('.')) return part.slice(1).split('.').every(c => node.classList.contains(c));
    return false;
  });
}

function mountPlayer() {
  const transport = el('div', { className: 'transport-controls' });
  const group = el('div', { className: 'transport-group' });
  group.append(el('button', { id: 'btn-prev' }), el('button', { id: 'btn-play' }), el('button', { id: 'btn-next' }));
  const minimize = el('button', { id: 'btn-minimize', className: 'btn btn-sm', text: 'Minimize' });
  const notes = el('button', { id: 'btn-player-notes', className: 'btn btn-sm' });
  const lyrics = el('button', { className: 'btn btn-sm lyrics-toggle' });
  const fullscreen = el('button', { id: 'btn-fullscreen', className: 'icon-btn' });
  transport.append(group, minimize, notes, lyrics, fullscreen);
  const player = el('aside', { id: 'player', className: 'player floating open' });
  const controls = el('div', { className: 'controls' });
  controls.appendChild(transport);
  player.appendChild(controls);
  player.classList.contains = name => (` ${player.className} `).includes(` ${name} `);
  return { player, transport, minimize, notes, lyrics, fullscreen };
}

globalThis.document = {
  createElement(tag) { return el(tag); },
  addEventListener() {},
};
globalThis.MutationObserver = class { observe() {} };

{
  globalThis.HTMLElement = class { showPopover() {} };
  document.createElement = tag => {
    const node = el(tag);
    node.hidePopover = function () { this._open = false; };
    node.showPopover = function () { this._open = true; };
    return node;
  };
  const { initCompactControls } = await import('../compactControls.js');
  const { player, transport, minimize, notes, lyrics } = mountPlayer();
  initCompactControls(player);
  assert.equal(minimize.parentElement?.id, 'player-options', 'minimize moves into options');
  assert.equal(notes.parentElement?.id, 'player-options', 'notes move into options');
  assert.equal(lyrics.parentElement, transport, 'lyrics toggle stays on the strip');
  assert.ok(transport.querySelector('#btn-player-options'), 'overflow menu is on the strip');
  assert.ok(player.querySelector('#player-options')?.hasAttribute('popover'), 'uses Popover when available');
}

{
  globalThis.HTMLElement = class {};
  document.createElement = tag => el(tag);
  // Re-import is cached; call with a fresh player against the same module.
  const { initCompactControls } = await import('../compactControls.js');
  const { player, transport, minimize, notes, lyrics } = mountPlayer();
  for (let i = 0; i < 4; i++) {
    transport.insertBefore(el('button', { id: `extra-${i}`, className: 'btn btn-sm' }), transport.querySelector('#btn-fullscreen'));
  }
  initCompactControls(player);
  assert.equal(minimize.parentElement?.id, 'player-options');
  assert.equal(notes.parentElement?.id, 'player-options');
  assert.equal(lyrics.parentElement, transport);
  assert.ok(transport.querySelector('#btn-player-options'));
  assert.equal(player.querySelector('#player-options')?.hasAttribute('popover'), false);
  assert.equal(player.querySelector('#player-options')?.hidden, true);
  assert.ok(transport.children.length <= 6, `fallback keeps a short strip, got ${transport.children.length}`);
  assert.ok(player.querySelectorAll('.btn').some(c => c.id?.startsWith('extra-') && c.parentElement?.id === 'player-options')
    || player.querySelector('#player-options').children.some(c => c.id?.startsWith('extra-')),
    'overflow absorbs extras');
}

console.log('compactControls: popover path, lyrics primary, minimize-in-menu, and no-popover fallback passed');
