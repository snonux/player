import assert from 'node:assert/strict';
import { editShare, localShareExpiry } from '../share-edit.js';
import { API } from '../api.js';
import { state } from '../state.js';

assert.equal(localShareExpiry('invalid'), '');
assert.match(localShareExpiry('2030-01-02T03:04:56Z'), /^2030-01-0[12]T\d\d:\d\d$/);
function container() {
  const elements = {description: {focus() {}, value: ''}, expiry: {value: ''}};
  const cancel = {}, status = {dataset:{}}, controls = [cancel, ...Object.values(elements)];
  const form = {elements, isConnected: true, querySelector: key => key === '[data-status]' ? status : cancel,
    querySelectorAll: () => controls, addEventListener: (name, handler) => { form[name] = handler; }};
  return {querySelector: () => form, form, status};
}
const user = {}; state.user = user;
const original = {token:'unchanged', expires_at:'2020-01-02T03:04:56Z', description:'Old'};
let patch, token, closed;
API.updateShare = async (value, body) => {token=value;patch=body;return {...original,...body};};
const el = container();
editShare(el, original, updated => {closed=updated;});
assert.match(el.innerHTML, /data-i18n-text/);
assert.match(el.innerHTML, /data-i18n-aria-label/);
assert.ok(!el.innerHTML.includes('data-i18n-text="Original'), 'metadata stays opaque');
el.form.elements.description.value='Changed <script>opaque</script>';
await el.form.onsubmit({preventDefault(){}});
assert.equal(token, original.token);
assert.equal(patch.description, 'Changed <script>opaque</script>');
assert.equal(Object.hasOwn(patch,'expires_at'), false, 'description-only leaves exact expired instant untouched');
assert.equal(closed.token, original.token);
const failed = container(); closed = false;
API.updateShare=async()=>{throw Error('offline');};
editShare(failed,original,()=>{closed=true;});
failed.form.elements.description.value='Draft';
await failed.form.onsubmit({preventDefault(){}});
assert.equal(failed.form.elements.description.value,'Draft');
assert.equal(failed.form.elements.description.disabled,false);
assert.match(failed.status.textContent,/Could not save/);
failed.form.keydown({key:'Escape',stopPropagation(){},preventDefault(){}});
assert.equal(closed,true,'Escape cancels locally');
const invalid=container(); closed=false;
editShare(invalid,original,()=>{closed=true;});
invalid.form.elements.expiry.value='2001-01-01T00:00';
await invalid.form.onsubmit({preventDefault(){}});
assert.match(invalid.status.textContent,/future expiry/);
assert.equal(closed,false);
console.log('share edit UI tests passed');
