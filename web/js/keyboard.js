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
      // Navigation
      case 'ArrowUp':
        e.preventDefault();
        handlers.navUp?.(e);
        break;
      case 'ArrowDown':
        e.preventDefault();
        handlers.navDown?.(e);
        break;
      case 'ArrowLeft':
        handlers.navLeft?.(e);
        break;
      case 'ArrowRight':
        handlers.navRight?.(e);
        break;
      // Vim-style navigation
      case 'k':
        e.preventDefault();
        handlers.navUp?.(e);
        break;
      case 'j':
        e.preventDefault();
        handlers.navDown?.(e);
        break;
      case 'h':
        handlers.navLeft?.(e);
        break;
      case 'l':
        handlers.navRight?.(e);
        break;
      case 'Enter': handlers.enter?.(e); break;
      case ' ':
        e.preventDefault();
        handlers.playPause?.(e);
        break;
      case 'p': handlers.playPause?.(e); break;
      case 'f': handlers.fullscreen?.(e); break;
      case 'Escape': handlers.escape?.(e); break;
      case 'r': handlers.shuffle?.(e); break;
      case 's': handlers.share?.(e); break;
      case '/':
        e.preventDefault();
        handlers.search?.(e);
        break;
      case 'n': handlers.notes?.(e); break;
      case 't': handlers.toolbar?.(e); break;
      case 'm': handlers.sidebar?.(e); break;
      case 'd': handlers.download?.(e); break;
      case 'u': handlers.upload?.(e); break;
      case '?':
        e.preventDefault();
        handlers.help?.(e);
        break;
    }
  });
}
