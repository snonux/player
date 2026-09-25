export function initKeyboard(handlers) {
  document.addEventListener('keydown', (e) => {
    const tag = e.target.tagName;
    const textControl = tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || e.target.isContentEditable;
    const buttonLike = tag === 'BUTTON' || tag === 'A';
    const inDialog = Boolean(e.target.closest?.('.modal-overlay'));
    // Enter activates a focused button or link natively. Space does so only
    // inside dialogs; elsewhere it stays play/pause, so a toolbar or card
    // button that kept focus after a mouse click cannot capture it.
    const nativeActivation = buttonLike && (e.key === 'Enter' || (e.key === ' ' && inDialog));
    // Set/folder open buttons keep DOM focus while j/k/h/l move the
    // selection, so Enter there opens the selected card instead of
    // whichever card button happens to hold focus.
    const gridNavigation = e.target.matches?.('.set-open, .folder-open');

    // Lightbox keyboard navigation (overrides global keys while open)
    if (handlers.isLightboxOpen?.()) {
      if (e.ctrlKey || e.metaKey || e.altKey) return;
      if (e.shiftKey && e.code === 'KeyS') {
        e.preventDefault();
        handlers.toggleSlideshow?.(e);
        return;
      }
      switch (e.key) {
        case 'ArrowLeft':
        case 'h':
          e.preventDefault();
          handlers.lightboxPrev?.(e);
          return;
        case 'ArrowRight':
        case 'l':
          e.preventDefault();
          handlers.lightboxNext?.(e);
          return;
        case 'Escape':
          e.preventDefault();
          handlers.closeLightbox?.(e);
          return;
        case '+':
        case '=':
          e.preventDefault();
          handlers.zoomIn?.(e);
          return;
        case '-':
          e.preventDefault();
          handlers.zoomOut?.(e);
          return;
      }
      // Allow only Escape/Arrows/h/l/+/−/Shift+S inside the lightbox; ignore everything else
      return;
    }

    // Shares modal keyboard navigation (overrides global keys while open)
    if (handlers.isSharesOpen?.()) {
      // Enter on a focused Copy, Revoke, or Close button must activate that
      // button. The row shortcut applies only when focus is on a share row.
      if ((textControl || nativeActivation) && e.key === 'Enter') return;
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

    // Media info modal keyboard scrolling.
    if (handlers.isMediaInfoOpen?.()) {
      switch (e.key) {
        case 'ArrowUp':
        case 'k':
          e.preventDefault();
          handlers.mediaInfoScroll?.(-1, e);
          return;
        case 'ArrowDown':
        case 'j':
          e.preventDefault();
          handlers.mediaInfoScroll?.(1, e);
          return;
        case 'Escape':
          e.preventDefault();
          handlers.closeMediaInfo?.(e);
          return;
      }
      return;
    }

    if (textControl) {
      if (e.key === 'Escape') {
        e.target.blur();
        handlers.escape?.(e);
      }
      return;
    }

    // Let Enter and Space activate a focused button or link. Letter shortcuts
    // remain available after clicking toolbar and card controls.
    if (nativeActivation && !gridNavigation) return;
    // Escape releases focus from a button so the next shortcut starts clean;
    // the dialog guard or the global Escape handler below still runs.
    if (buttonLike && e.key === 'Escape') e.target.blur?.();

    // Any other open dialog (help, notes, tags, upload, admin, podcasts) owns
    // the keyboard. Focused dialog buttons no longer swallow letter keys, so
    // grid, playback, and rescan shortcuts must not act behind the dialog.
    // Only Escape and ? (closing help itself) pass through.
    if (handlers.isModalOpen?.()) {
      if (e.key === 'Escape') {
        handlers.escape?.(e);
      } else if (e.key === '?' && handlers.isOnlyHelpOpen?.()) {
        e.preventDefault();
        handlers.help?.(e);
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

    // Space toggles the focused set row's selection; any other element
    // outside a dialog gets play/pause.
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

    const imageDelta = fullscreenImageDelta(e);
    if (imageDelta && handlers.imageFullscreenNavigate?.(imageDelta)) {
      e.preventDefault();
      return;
    }

    if (/^[1-9]$/.test(e.key) && handlers.selectSetByHotkey?.(e.key)) {
      e.preventDefault();
      return;
    }

    if (e.shiftKey) {
      switch (e.key) {
        case 'ArrowUp':
        case 'ArrowDown':
        case 'ArrowLeft':
        case 'ArrowRight': {
          const dx = e.key === 'ArrowLeft' ? -10 : e.key === 'ArrowRight' ? 10 : 0;
          const dy = e.key === 'ArrowUp' ? -10 : e.key === 'ArrowDown' ? 10 : 0;
          const acted = handlers.shiftCropPosition?.(dx, dy);
          if (acted) {
            e.preventDefault();
            return;
          }
          break;
        }
      }
    }

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
      if (!e.shiftKey) handlers.share?.(e);
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
      case 'PageUp':
        if (handlers.seekPercent?.(e, -0.10)) {
          e.preventDefault();
        }
        break;
      case 'PageDown':
        if (handlers.seekPercent?.(e, 0.10)) {
          e.preventDefault();
        }
        break;
      case 'Enter':
        e.preventDefault();
        handlers.enter?.(e);
        break;
      case 'n':
        handlers.notes?.(e);
        break;
      case 't':
        e.preventDefault();
        handlers.tags?.(e);
        break;
      case 'F':
        e.preventDefault();
        handlers.favorite?.(e);
        break;
      case 'A':
        e.preventDefault();
        handlers.admin?.(e);
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
      case 'c': handlers.toggleCrop?.(e); break;
      case 'x':
        e.preventDefault();
        handlers.cycleCropPosition?.(e);
        break;
      case 'Escape': handlers.escape?.(e); break;
      case 'Backspace': handlers.backspace?.(e); break;
      case 'r': handlers.shuffle?.(e); break;
      case 'R':
        e.preventDefault();
        handlers.playRandom?.(e);
        break;
      case 'M':
        e.preventDefault();
        handlers.rescanMedia?.(e);
        break;
      // Note: 's' / 'S' are handled above by the e.code === 'KeyS' block
      case '/':
        e.preventDefault();
        handlers.search?.(e);
        break;
      case 'i': handlers.mediaInfo?.(e); break;
      case 'd': handlers.toggleDetach?.(e); break;
      case 'D': handlers.download?.(e); break;
      case 'u': handlers.upload?.(e); break;
      case 'L': handlers.sharesToggle?.(e); break;
      case 'T': handlers.regenThumbnail?.(e); break;
      case 'q':
        e.preventDefault();
        handlers.stopAndClose?.(e);
        break;
      case '?':
        e.preventDefault();
        handlers.help?.(e);
        break;
    }
  });
}

function fullscreenImageDelta(e) {
  switch (e.key) {
    case 'ArrowLeft':
    case 'ArrowUp':
    case 'h':
    case 'k':
    case 'PageUp':
      return e.key === 'PageUp' ? -10 : -1;
    case 'ArrowRight':
    case 'ArrowDown':
    case 'j':
    case 'l':
    case 'PageDown':
      return e.key === 'PageDown' ? 10 : 1;
    default:
      return 0;
  }
}
