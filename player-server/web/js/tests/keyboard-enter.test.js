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

console.log('Running keyboard Enter tests...');
testEnterActivatesGridHandler();
testEnterLeavesFocusedButtonNative();
testEscapeStillWorksOnNativeControl();
testSharesEnterLeavesNativeControlsAlone();

if (failures.length) {
  console.error('FAILURES:');
  failures.forEach((m) => console.error('  - ' + m));
  process.exit(1);
} else {
  console.log('All keyboard Enter tests passed.');
  process.exit(0);
}
