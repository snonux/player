export function initKeyboard(handlers) {
  document.addEventListener('keydown', (e) => {
    const tag = e.target.tagName;
    const editing = tag === 'INPUT' || tag === 'TEXTAREA' || e.target.isContentEditable;

    // Shares modal keyboard navigation (overrides global keys while open)
    if (handlers.isSharesOpen?.()) {
      switch (e.key) {
        case 'ArrowUp':
        case 'k':
          e.preventDefault();
          handlers.sharesNavUp?.(e);
          return;
        case 'ArrowDown':
        case 'j':
          e.preventDefault();
          handlers.sharesNavDown?.(e);
          return;
        case 'Enter':
          e.preventDefault();
          handlers.sharesCopy?.(e);
          return;
        case 'Delete':
          e.preventDefault();
          handlers.sharesDelete?.(e);
          return;
        case 'Escape':
          e.preventDefault();
          handlers.sharesToggle?.(e);
          return;
      }
      // Allow only Escape/Enter/Arrows/k/j/d inside the shares modal; ignore everything else
      return;
    }

    if (editing) {
      if (e.key === 'Escape') {
        e.target.blur();
        handlers.escape?.(e);
      }
      return;
    }

    // Tab / Shift+Tab set navigation when sidebar is open
    if (e.key === 'Tab') {
      if (handlers.isSidebarOpen?.()) {
        e.preventDefault();
        if (e.shiftKey) {
          handlers.prevSet?.(e);
        } else {
          handlers.nextSet?.(e);
        }
      }
      return;
    }

    // Space toggles focused set selection when sidebar is focused/open
    if (e.key === ' ') {
      if (handlers.isSidebarFocused?.()) {
        e.preventDefault();
        handlers.toggleSetSelect?.(e);
        return;
      }
      e.preventDefault();
      handlers.playPause?.(e);
      return;
    }

    if (e.ctrlKey || e.metaKey || e.altKey) return; // don't intercept browser shortcuts

    if (e.shiftKey && e.code === 'KeyN') {
      e.preventDefault();
      handlers.nextTrack?.(e);
      return;
    }
    if (e.shiftKey && e.code === 'KeyP') {
      e.preventDefault();
      handlers.prevTrack?.(e);
      return;
    }
    if (e.code === 'KeyS') {
      e.preventDefault();
      if (e.shiftKey) handlers.share?.(e);
      else handlers.sidebar?.(e);
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
        if (handlers.seekBackward?.(e)) {
          e.preventDefault();
          break;
        }
        handlers.navLeft?.(e);
        break;
      case 'ArrowRight':
        if (handlers.seekForward?.(e)) {
          e.preventDefault();
          break;
        }
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
        if (handlers.seekBackward?.(e)) {
          e.preventDefault();
          break;
        }
        handlers.navLeft?.(e);
        break;
      case 'l':
        if (handlers.seekForward?.(e)) {
          e.preventDefault();
          break;
        }
        handlers.navRight?.(e);
        break;
      case 'Enter': handlers.enter?.(e); break;
      case 'n':
        handlers.notes?.(e);
        break;
      case 'p':
        e.preventDefault();
        handlers.playPause?.(e);
        break;
      case 'N':
        e.preventDefault();
        handlers.nextTrack?.(e);
        break;
      case 'P':
        e.preventDefault();
        handlers.prevTrack?.(e);
        break;
      case 'C': handlers.toggleMinimize?.(e); break;
      case 'f': handlers.fullscreen?.(e); break;
      case 'Escape': handlers.escape?.(e); break;
      case 'Backspace': handlers.backspace?.(e); break;
      case 'r': handlers.shuffle?.(e); break;
      case 's': handlers.sidebar?.(e); break;
      case 'S': handlers.share?.(e); break;
      case '/':
        e.preventDefault();
        handlers.search?.(e);
        break;
      case 'i': handlers.mediaInfo?.(e); break;
      case 'd': handlers.toggleDetach?.(e); break;
      case 'D': handlers.download?.(e); break;
      case 'u': handlers.upload?.(e); break;
      case 'L': handlers.sharesToggle?.(e); break;
      case '?':
        e.preventDefault();
        handlers.help?.(e);
        break;
    }
  });
}
