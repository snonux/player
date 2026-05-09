import { enable, isOn, revision, toggle } from '../shuffle.js';

const failures = [];
const button = {
  active: false,
  classList: {
    toggle(name, enabled) {
      if (name === 'active') button.active = enabled;
    },
  },
};

globalThis.document = {
  getElementById(id) {
    return id === 'shuffle-toggle' ? button : null;
  },
};

function assert(cond, msg) {
  if (!cond) failures.push(msg || 'assertion failed');
}

function testEnableTurnsShuffleOn() {
  enable();
  assert(isOn(), 'enable should turn shuffle on');
  assert(button.active, 'enable should update the shuffle button active state');
}

function testRepeatedEnableKeepsShuffleOnAndAdvancesRevision() {
  const before = revision();
  enable();
  assert(isOn(), 'repeated enable should keep shuffle on');
  assert(revision() > before, 'repeated enable should advance revision for a fresh random load');
}

function testToggleCanStillTurnShuffleOff() {
  toggle();
  assert(!isOn(), 'toggle should still turn shuffle off for the toolbar button');
  assert(!button.active, 'toggle should update the shuffle button inactive state');
}

console.log('Running shuffle tests...');
testEnableTurnsShuffleOn();
testRepeatedEnableKeepsShuffleOnAndAdvancesRevision();
testToggleCanStillTurnShuffleOff();

if (failures.length) {
  console.error('FAILURES:');
  failures.forEach((m) => console.error('  - ' + m));
  process.exit(1);
} else {
  console.log('All shuffle tests passed.');
  process.exit(0);
}
