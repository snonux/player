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

function pressKey(key, target = { tagName: 'BODY', isContentEditable: false }) {
  let prevented = false;
  keydownHandler?.({
    key,
    code: key.length === 1 ? `Key${key.toUpperCase()}` : key,
    target,
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

console.log('Running keyboard Enter tests...');
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

if (failures.length) {
  console.error('FAILURES:');
  failures.forEach((m) => console.error('  - ' + m));
  process.exit(1);
} else {
  console.log('All keyboard Enter tests passed.');
  process.exit(0);
}
