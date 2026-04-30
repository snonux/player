export function initKeyboard(handlers) {
  document.addEventListener('keydown', (e) => {
    const tag = e.target.tagName;
    const editing = tag === 'INPUT' || tag === 'TEXTAREA' || e.target.isContentEditable;
    if (editing) {
      if (e.key === 'Escape') {
        e.target.blur();
        handlers.escape?.(e);
      }
      return;
    }
    switch (e.key) {
      case 'ArrowUp':    e.preventDefault(); handlers.navUp?.(e); break;
      case 'ArrowDown':  e.preventDefault(); handlers.navDown?.(e); break;
      case 'ArrowLeft':  handlers.navLeft?.(e); break;
      case 'ArrowRight': handlers.navRight?.(e); break;
      case 'Enter':      handlers.enter?.(e); break;
      case ' ':          e.preventDefault(); handlers.playPause?.(e); break;
      case 'p':          handlers.playPause?.(e); break;
      case 'f':          handlers.fullscreen?.(e); break;
      case 'Escape':     handlers.escape?.(e); break;
      case 'r':          handlers.shuffle?.(e); break;
      case 's':          handlers.share?.(e); break;
      case '/':          e.preventDefault(); handlers.search?.(e); break;
      case 'n':          handlers.notes?.(e); break;
      case 't':          handlers.tags?.(e); break;
      case 'd':          handlers.download?.(e); break;
    }
  });
}
