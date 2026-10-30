import { initKeyboard } from '../keyboard.js';

const failures = [];
let keydownHandler = null;

globalThis.document = {
  addEventListener(type, handler) {
    if (type === 'keydown') keydownHandler = handler;
  },
};

function assert(cond, msg) {
  if (!cond) failures.push(msg || 'assertion failed');
}

function pressKey(key, target = { tagName: 'BODY', isContentEditable: false }, modifiers = {}) {
  let prevented = false;
  keydownHandler?.({
    key,
    code: key.length === 1 ? `Key${key.toUpperCase()}` : key,
    target,
    ...modifiers,
    preventDefault() { prevented = true; },
  });
  return prevented;
}

function testEnterActivatesGridHandler() {
  let entered = 0;
  initKeyboard({ enter: () => { entered += 1; } });

  const prevented = pressKey('Enter', { tagName: 'DIV', isContentEditable: false });

  assert(entered === 1, 'Enter on a grid/card target should call the enter handler');
  assert(prevented, 'Enter on a grid/card target should prevent native default handling');
}

function testEnterLeavesFocusedButtonNative() {
  let entered = 0;
  initKeyboard({ enter: () => { entered += 1; } });

  const prevented = pressKey('Enter', { tagName: 'BUTTON', isContentEditable: false });

  assert(entered === 0, 'Enter on a focused button should not also call the global enter handler');
  assert(!prevented, 'Enter on a focused button should leave native button activation alone');
}

function testEscapeStillWorksOnNativeControl() {
  let escaped = 0;
  let blurred = 0;
  initKeyboard({ escape: () => { escaped += 1; } });

  const prevented = pressKey('Escape', {
    tagName: 'BUTTON',
    isContentEditable: false,
    blur() { blurred += 1; },
  });

  assert(escaped === 1, 'Escape on a focused button should call escape handler');
  assert(blurred === 1, 'Escape on a focused button should blur the button');
  assert(!prevented, 'Escape on a focused button should preserve existing keyboard behavior');
}

function testSharesEnterLeavesNativeControlsAlone() {
  let copied = 0;
  initKeyboard({ isSharesOpen: () => true, sharesCopy: () => { copied += 1; } });

  for (const tagName of ['BUTTON', 'A', 'INPUT']) {
    const prevented = pressKey('Enter', { tagName, isContentEditable: false });
    assert(!prevented, `Enter on ${tagName} inside My Shares should preserve native activation`);
  }
  assert(copied === 0, 'Enter on a native My Shares control must not copy the selected row');

  const rowPrevented = pressKey('Enter', { tagName: 'DIV', isContentEditable: false });
  assert(rowPrevented, 'Enter on a share row should prevent native default handling');
  assert(copied === 1, 'Enter on a share row should copy the selected link once');
}

function testLettersReachShortcutsFromFocusedButton() {
  let searched = 0;
  initKeyboard({ search: () => { searched += 1; } });

  const prevented = pressKey('/', { tagName: 'BUTTON', isContentEditable: false });

  assert(searched === 1, '/ on a focused toolbar button should open search');
  assert(prevented, '/ handled as a shortcut should prevent the default');
}

function testTextInputStillSwallowsLetters() {
  let searched = 0;
  initKeyboard({ search: () => { searched += 1; } });

  pressKey('/', { tagName: 'INPUT', isContentEditable: false });

  assert(searched === 0, '/ typed into a text input must stay text');
}

function testGridOpenButtonFollowsSelection() {
  let entered = 0;
  let played = 0;
  initKeyboard({ enter: () => { entered += 1; }, playPause: () => { played += 1; } });
  const gridButton = { tagName: 'BUTTON', isContentEditable: false, matches: () => true };

  const enterPrevented = pressKey('Enter', gridButton);
  const spacePrevented = pressKey(' ', gridButton);

  assert(entered === 1 && enterPrevented, 'Enter on a set/folder open button should open the selected card');
  assert(played === 1 && spacePrevented, 'Space on a set/folder open button should play/pause, not click the focused card');
}

function testOpenDialogBlocksGlobalShortcuts() {
  const calls = [];
  let helpOnly = true;
  initKeyboard({
    isModalOpen: () => true,
    navDown: () => calls.push('navDown'),
    rescanMedia: () => calls.push('rescan'),
    search: () => calls.push('search'),
    help: () => calls.push('help'),
    escape: () => calls.push('escape'),
    isOnlyHelpOpen: () => helpOnly,
  });
  const button = { tagName: 'BUTTON', isContentEditable: false, blur() {} };

  for (const key of ['j', 'M', '/']) pressKey(key, button);
  assert(calls.length === 0, `letters must not act behind an open dialog, got ${calls.join(',')}`);

  pressKey('?', button);
  pressKey('Escape', button);
  assert(calls.join(',') === 'help,escape', `? and Escape should reach the dialog, got ${calls.join(',')}`);

  // With notes/tags/etc. open, ? must not stack help underneath them.
  calls.length = 0;
  helpOnly = false;
  pressKey('?', button);
  assert(calls.length === 0, `? must not open help over another dialog, got ${calls.join(',')}`);
}

function testSpaceOnSidebarButtonPlays() {
  let played = 0;
  let toggled = 0;
  initKeyboard({
    isSidebarFocused: () => false,
    playPause: () => { played += 1; },
    toggleSetSelect: () => { toggled += 1; },
  });

  pressKey(' ', { tagName: 'BUTTON', isContentEditable: false, closest: () => null });

  assert(played === 1 && toggled === 0, 'Space on a non-row sidebar button should play/pause, not toggle a set');
}

function testSpaceOnButtonsOutsideDialogsPlays() {
  let played = 0;
  initKeyboard({ playPause: () => { played += 1; } });

  const toolbar = pressKey(' ', { tagName: 'BUTTON', isContentEditable: false, closest: () => null });
  const dialogButton = pressKey(' ', { tagName: 'BUTTON', isContentEditable: false, closest: () => ({}) });

  assert(played === 1 && toolbar, 'Space on a toolbar/card button should play/pause instead of re-clicking it');
  assert(!dialogButton, 'Space on a dialog button should keep native activation');
}

function testItemShortcutsDispatch() {
  const calls = [];
  initKeyboard({
    tags: () => calls.push('tags'),
    favorite: () => calls.push('favorite'),
    admin: () => calls.push('admin'),
    fullscreen: () => calls.push('fullscreen'),
  });
  for (const key of ['t', 'F', 'A', 'f']) pressKey(key);
  assert(calls.join(',') === 'tags,favorite,admin,fullscreen', `t/F/A/f should dispatch distinct handlers, got ${calls.join(',')}`);
}

function testBackspaceRespectsViewsAndTyping() {
  let closed = 0;
  let backed = 0;
  initKeyboard({ isLightboxOpen: () => true, closeLightbox: () => { closed += 1; } });
  assert(pressKey('Backspace'), 'Backspace in an image view should prevent browser navigation');
  assert(closed === 1, 'Backspace in an image view should leave the view');
  assert(!pressKey('Backspace', { tagName: 'INPUT' }), 'Backspace in an image-view text field must remain text editing');
  assert(closed === 1, 'Editing text must not close an image view');

  initKeyboard({ isModalOpen: () => true, backspace: () => { backed += 1; } });
  pressKey('Backspace');
  assert(backed === 0, 'Backspace must not navigate behind an open dialog');
  initKeyboard({ backspace: () => { backed += 1; } });
  pressKey('Backspace', { tagName: 'TEXTAREA' });
  assert(backed === 0, 'Backspace in a text field must remain text editing');
  pressKey('Backspace');
  assert(backed === 1, 'Backspace outside dialogs and text fields should reach navigation');
}

function testModeGuardsBeforeRouting() {
  const modes = [
    { isSharesOpen: () => true, sharesNavUp: record, sharesNavDown: record, sharesCopy: record, sharesDelete: record },
    { isMediaInfoOpen: () => true, mediaInfoScroll: record },
    { isLightboxOpen: () => true, lightboxPrev: record, lightboxNext: record, zoomIn: record, closeLightbox: record },
    { playPause: record, toggleSetSelect: record, isSidebarFocused: () => true },
    { playPause: record },
  ];
  let calls = 0;
  function record() { calls += 1; }
  for (const mode of modes) {
    initKeyboard(mode);
    for (const modifier of ['ctrlKey', 'metaKey', 'altKey']) {
      for (const key of ['j', 'k', 'h', 'l', 'ArrowDown', 'Enter', 'Delete', ' ', '+', 'Escape', 'Tab']) {
        assert(!pressKey(key, undefined, { [modifier]: true }), `${modifier}+${key} must keep browser default`);
      }
    }
    for (const target of [{ tagName: 'INPUT' }, { tagName: 'TEXTAREA' }, { tagName: 'SELECT' }, { tagName: 'SPAN', isContentEditable: true }]) {
      for (const key of ['j', 'k', 'h', 'l', 'ArrowDown', 'Enter', 'Delete', ' ', '+', 'Backspace']) {
        assert(!pressKey(key, target), `${key} on ${target.tagName} must keep native editing`);
      }
    }
    assert(calls === 0, 'Guarded keys must not call specialized modal, image, or playback handlers');
  }
  initKeyboard(modes[0]);
  pressKey('j'); pressKey('k'); pressKey('Enter'); pressKey('Delete');
  initKeyboard(modes[1]);
  pressKey('j'); pressKey('k');
  assert(calls === 6, 'Unmodified shares navigation/copy/delete and media-info scrolling must still act');
  initKeyboard(modes[3]);
  assert(pressKey(' '), 'Unmodified sidebar Space must prevent default');
  assert(calls === 7, 'Unmodified sidebar Space must still toggle selection');
}

function testNativeControlsBeforeSpecialModes() {
  let calls = 0;
  for (const mode of [
    { isSharesOpen: () => true, sharesCopy: () => { calls += 1; } },
    { isMediaInfoOpen: () => true },
    { isLightboxOpen: () => true },
  ]) {
    initKeyboard(mode);
    for (const tagName of ['BUTTON', 'A']) {
      const target = { tagName, closest: () => ({}) };
      assert(!pressKey('Enter', target), 'Special modes must preserve native Enter');
      assert(!pressKey(' ', target), 'Special modes must preserve native dialog Space');
    }
  }
  assert(calls === 0, 'Native controls must not invoke selected-share copy');
}

function testEditableEscapeBlursAndCloses() {
  let closed = 0;
  let blurred = 0;
  for (const mode of [{ isSharesOpen: () => true }, { isMediaInfoOpen: () => true }, { isLightboxOpen: () => true }, { isModalOpen: () => true }]) {
    initKeyboard({ ...mode, escape: () => { closed += 1; } });
    pressKey('Escape', { tagName: 'INPUT', blur: () => { blurred += 1; } });
  }
  assert(closed === 4 && blurred === 4, 'Editable Escape must blur and reach the shared escape handler in every mode');
}

function testImageControlsAndCropOwnership() {
  const calls = [];
  let crop = false;
  initKeyboard({
    isLightboxOpen: () => true,
    fullscreen: () => calls.push('fullscreen'),
    mediaInfo: () => calls.push('info'),
    toggleCrop: () => calls.push('crop'),
    cycleCropPosition: () => calls.push('anchor'),
    resetZoom: () => calls.push('reset'),
    toggleSlideshow: () => calls.push('slideshow'),
    lightboxPrev: () => calls.push('prev'),
    lightboxNext: () => calls.push('next'),
    panImage: (x, y) => calls.push(`pan:${x},${y}`),
    shiftCropPosition: (x, y) => { if (crop) calls.push(`crop:${x},${y}`); return crop; },
    navDown: () => calls.push('hidden-grid'),
    seekForward: () => calls.push('audio-seek'),
    sharesToggle: () => calls.push('shares'),
  });
  for (const key of ['f', 'i', 'c', 'x', '0', 'S', 'h', 'l', 'j', 'k', 'H', 'L']) {
    assert(pressKey(key, undefined, { shiftKey: /[SHL]/.test(key) }), `${key} should be owned by expanded images`);
  }
  assert(calls.join('|') === 'fullscreen|info|crop|anchor|reset|slideshow|prev|next|pan:0,40|pan:0,-40|pan:-40,0|pan:40,0', `Unexpected image dispatch: ${calls.join('|')}`);
  calls.length = 0;
  crop = true;
  for (const key of ['j', 'k', 'H', 'L']) pressKey(key);
  pressKey('ArrowRight', undefined, { shiftKey: true });
  assert(calls.join('|') === 'crop:0,10|crop:0,-10|crop:-10,0|crop:10,0|crop:10,0', `Crop position must win over pan/navigation: ${calls.join('|')}`);
  calls.length = 0;
  initKeyboard({ isLightboxOpen: () => true, isModalOpen: () => true,
    panImage: () => calls.push('pan'), fullscreen: () => calls.push('fullscreen'),
    escape: () => calls.push('escape'), closeLightbox: () => calls.push('close-image') });
  for (const key of ['f', 'j', 'L', '0', 'c']) pressKey(key);
  pressKey('Escape');
  assert(calls.join('|') === 'escape', 'An open dialog must own keys even when the image predicate is true');
}

function testAudioSeekUnchanged() {
  const calls = [];
  initKeyboard({ seekBackward: () => { calls.push('back'); return true; }, seekForward: () => { calls.push('forward'); return true; }, navLeft: () => calls.push('grid'), navRight: () => calls.push('grid') });
  for (const key of ['h', 'l', 'ArrowLeft', 'ArrowRight']) assert(pressKey(key), `${key} seeking must prevent default`);
  assert(calls.join(',') === 'back,forward,back,forward', 'Global audio/video seek dispatch must remain unchanged');
}

function testHelpSearchOwnership() {
  const calls = [];
  let helpOnly = true;
  initKeyboard({
    isModalOpen: () => true,
    isOnlyHelpOpen: () => helpOnly,
    helpSearch: () => calls.push('helpSearch'),
    search: () => calls.push('librarySearch'),
    help: () => calls.push('help'),
  });
  const button = { tagName: 'BUTTON', closest: () => ({}) };
  assert(pressKey('/', button), 'Help-local slash should prevent native default');
  assert(calls.join() === 'helpSearch', 'Slash in help should focus only its filter');
  for (const modifier of ['ctrlKey', 'metaKey', 'altKey']) {
    assert(!pressKey('/', button, { [modifier]: true }), 'Modified slash must stay native');
  }
  for (const tagName of ['INPUT', 'TEXTAREA', 'SELECT']) {
    assert(!pressKey('/', { tagName }), 'Slash in editable help controls must stay native');
    pressKey('?', { tagName });
  }
  assert(calls.length === 1, 'Typing and chords must not refocus or close help');
  helpOnly = false;
  pressKey('/', button);
  assert(calls.length === 1, 'Other dialogs must block both search destinations');
  initKeyboard({ isLightboxOpen: () => true, help: () => calls.push('imageHelp') });
  assert(pressKey('?'), 'Image help shortcut should prevent native default');
  assert(calls.at(-1) === 'imageHelp', 'Expanded/fullscreen image mode must expose help');
}

function testPictureLayoutOwnership() {
  let toggles = 0;
  const togglePictureLayout = () => { toggles++; };
  initKeyboard({ togglePictureLayout });
  assert(pressKey('g'), 'g should prevent default when toggling picture layout');
  assert(toggles === 1, 'g should toggle picture layout once');
  for (const tagName of ['INPUT', 'TEXTAREA', 'SELECT']) pressKey('g', { tagName });
  pressKey('g', { tagName: 'DIV', isContentEditable: true });
  for (const modifier of ['ctrlKey', 'metaKey', 'altKey']) pressKey('g', undefined, { [modifier]: true });
  pressKey('G', undefined, { shiftKey: true });
  assert(toggles === 1, 'Typing, uppercase G and browser chords must not toggle layout');
  for (const mode of ['isModalOpen', 'isSharesOpen', 'isMediaInfoOpen', 'isLightboxOpen']) {
    initKeyboard({ togglePictureLayout, [mode]: () => true });
    pressKey('g');
  }
  assert(toggles === 1, 'Dialogs and expanded images must block picture layout changes');
}

function testThumbnailSizeOwnership() {
  const calls = [];
  const handlers = {
    thumbnailLarger: () => calls.push('larger'),
    thumbnailSmaller: () => calls.push('smaller'),
    thumbnailReset: () => calls.push('reset'),
  };
  initKeyboard(handlers);
  for (const key of ['+', '=', '-', '0']) assert(pressKey(key), `${key} should resize library previews`);
  assert(calls.join() === 'larger,larger,smaller,reset', 'Preview keys should dispatch size changes');
  calls.length = 0;
  for (const key of ['+', '=', '-', '0']) {
    for (const tagName of ['INPUT', 'TEXTAREA', 'SELECT']) pressKey(key, { tagName });
    pressKey(key, { tagName: 'DIV', isContentEditable: true });
    for (const modifier of ['ctrlKey', 'metaKey', 'altKey']) pressKey(key, undefined, { [modifier]: true });
    for (const mode of ['isModalOpen', 'isSharesOpen', 'isMediaInfoOpen', 'isLightboxOpen']) {
      initKeyboard({ ...handlers, [mode]: () => true });
      pressKey(key);
    }
    initKeyboard(handlers);
  }
  assert(calls.length === 0, 'Editors, browser chords, dialogs and image view must own preview size keys');
}

function testQueueKeysPrecedeModalAndButtonBlur() {
  const calls = [];
  let focused = true;
  initKeyboard({ isQueueFocused: () => focused, isModalOpen: () => true,
    queueKey: e => { calls.push(e.key); e.preventDefault(); return true; },
    escape: () => calls.push('globalEscape'), navDown: () => calls.push('gridDown') });
  const button = { tagName:'BUTTON', closest:()=>({}), blur() { focused=false; calls.push('blur'); } };
  assert(pressKey('Escape',button), 'queue Escape is handled');
  assert(calls.join() === 'Escape', 'queue button Escape closes before generic blur and global routing');
  calls.length=0;
  for (const key of ['j','k','J','K','Delete','ContextMenu']) pressKey(key,{tagName:'LI'});
  assert(calls.join() === 'j,k,J,K,Delete,ContextMenu', 'queue local keys work inside narrow modal before global guards');
  calls.length=0;
  for (const key of ['Enter',' ']) assert(!pressKey(key,button), 'queue buttons retain native Enter/Space');
  pressKey('Q',{tagName:'INPUT'}); pressKey('Delete',{tagName:'SELECT'});
  assert(calls.length===0,'queue keys preserve typing and native selects');
  const wideButton = { tagName:'BUTTON', closest:selector=>selector==='#queue-panel' ? {} : null };
  for (const key of ['Enter',' ']) assert(!pressKey(key,wideButton), 'wide queue buttons retain native Enter/Space');
  assert(calls.length===0,'wide queue button activation must not route global playback');
  let opened=0;
  initKeyboard({toggleQueue:()=>opened++}); pressKey('Q');
  assert(opened===1,'Q opens queue globally');
}
function testInsertionShortcutAndToastOwnership() {
  const calls = [];
  initKeyboard({queueNext:()=>calls.push('next'),queueAppend:()=>calls.push('append'),playPause:()=>calls.push('play')});
  pressKey('e'); pressKey('E');
  assert(calls.join() === 'next,append','e/E dispatch explicit insertion');
  for (const tagName of ['INPUT','TEXTAREA','SELECT']) { pressKey('e',{tagName}); pressKey('E',{tagName}); }
  assert(calls.length === 2,'typing cannot enqueue songs');
  const undo = {tagName:'BUTTON',closest:selector=>selector==='#toast' ? {} : null};
  assert(!pressKey(' ',undo),'toast Undo keeps native Space activation');
  assert(calls.length===2,'toast Undo Space cannot play/pause the media');
  assert(pressKey(' ',{tagName:'BUTTON'}),'ordinary toolbar retains global Space');
  assert(calls[2]==='play','ordinary toolbar Space still toggles playback');
}
testInsertionShortcutAndToastOwnership();
testQueueKeysPrecedeModalAndButtonBlur();

testThumbnailSizeOwnership();
testPictureLayoutOwnership();
testHelpSearchOwnership();
let repeatCycles=0;
initKeyboard({cycleQueueRepeat:()=>repeatCycles++});
pressKey('Y');
pressKey('Y',{tagName:'BUTTON'});
assert(repeatCycles===2,'Y cycles queue repeat globally and from toolbar focus');
for(const tagName of ['INPUT','TEXTAREA','SELECT']) pressKey('Y',{tagName});
pressKey('Y',{tagName:'DIV',isContentEditable:true});
pressKey('Y',{tagName:'BODY'},{ctrlKey:true});
assert(repeatCycles===2,'typing/editing/browser chords cannot cycle repeat');
initKeyboard({isModalOpen:()=>true,cycleQueueRepeat:()=>repeatCycles++});
pressKey('Y');assert(repeatCycles===2,'modal owns Y');
console.log('Running keyboard Enter tests...');
testAudioSeekUnchanged();
testImageControlsAndCropOwnership();
testModeGuardsBeforeRouting();
testNativeControlsBeforeSpecialModes();
testEditableEscapeBlursAndCloses();
testEnterActivatesGridHandler();
testEnterLeavesFocusedButtonNative();
testEscapeStillWorksOnNativeControl();
testSharesEnterLeavesNativeControlsAlone();
testLettersReachShortcutsFromFocusedButton();
testTextInputStillSwallowsLetters();
testGridOpenButtonFollowsSelection();
testOpenDialogBlocksGlobalShortcuts();
testSpaceOnButtonsOutsideDialogsPlays();
testSpaceOnSidebarButtonPlays();
testItemShortcutsDispatch();
testBackspaceRespectsViewsAndTyping();

// Episode action keys are scoped by the supplied card handler; native Enter stays intact.
{
  const actions = [];
  initKeyboard({podcastAction: action => {actions.push(action);return true;}});
  assert(pressKey('v'), 'Listened toggle handles its key');
  assert(pressKey('Delete'), 'Episode deletion handles its key');
  assert(actions.join(',') === 'complete,delete-episode', 'Episode shortcuts reuse card actions');
  pressKey('Enter',{tagName:'BUTTON'});
  pressKey('v',{tagName:'INPUT'});
  initKeyboard({isModalOpen:()=>true,podcastAction:()=>{actions.push('behind-dialog');return true;}});
  pressKey('Delete');
  assert(actions.length === 2, 'Typing, dialogs and native button Enter stay intact');
}

if (failures.length) {
  console.error('FAILURES:');
  failures.forEach((m) => console.error('  - ' + m));
  process.exit(1);
} else {
  console.log('All keyboard Enter tests passed.');
  process.exit(0);
}
