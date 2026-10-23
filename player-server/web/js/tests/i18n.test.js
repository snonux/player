import assert from 'node:assert/strict';
import { resolveLanguage, t, html, plural, initLanguage, applyTranslations } from '../i18n.js';
import { german } from '../i18n-de.js';

assert.equal(resolveLanguage('system','de-DE'),'de');
assert.equal(resolveLanguage('garbage','fr-FR'),'en');
assert.equal(resolveLanguage('en','de-DE'),'en');
assert.equal(t('Unknown future label',{},'de'),'Unknown future label');
assert.equal(t('Play {name}',{name:'English metadata'},'de'),'English metadata abspielen');
for(const [key,value] of Object.entries(german)) {
  const placeholders=text=>[...text.matchAll(/\{(\w+)\}/g)].map(match=>match[1]).sort();
  assert.deepEqual(placeholders(value),placeholders(key),`placeholder parity: ${key}`);
}
const elements=new Map();const handlers={};
const select={value:'',addEventListener(type,listener){this[type]=listener;}};
const status={dataset:{}};
elements.set('language-select',select);elements.set('language-status',status);
const tracked=[];
globalThis.document={documentElement:{},getElementById:id=>elements.get(id),querySelectorAll:selector=>selector==='[data-i18n]'?[status].filter(e=>e.dataset.i18n):tracked.filter(e=>selector===`[${e.attr}]`),dispatchEvent(){},createElement:()=>({set innerHTML(value){this.value=value.replaceAll('&lt;','<').replaceAll('&amp;','&');}})};
globalThis.window={addEventListener:(type,listener)=>handlers[type]=listener};
const stored=new Map([['player.language.v1','de']]);
globalThis.localStorage={getItem:key=>stored.get(key),setItem:(key,value)=>stored.set(key,value)};
initLanguage();assert.equal(select.value,'de');assert.equal(document.documentElement.lang,'de');
assert.equal(plural(1,'{count} album','{count} albums'),'1 Album');
assert.equal(plural(2,'{count} album','{count} albums'),'2 Alben');
const metadata='Play';
const markup=html`<button aria-label="Play ${metadata}">Play</button><h1>${metadata}</h1><option value="albums">Albums</option>`;
assert.match(markup,/Play abspielen/);assert.match(markup,/>Abspielen<\/span>/);assert.match(markup,/<h1>Play<\/h1>/,'metadata matching a UI key remains verbatim');
assert.match(markup,/<option[^>]*>Alben<\/option>/,'native select options contain plain text');
assert(!markup.includes('<option value="albums"><span'));
const textBinding={encoded:true,key:'Play {name}',args:{name:'Track &amp; artist'}};
tracked.push({attr:'data-i18n-text',getAttribute:()=>JSON.stringify(textBinding)});
applyTranslations();assert.equal(tracked[0].textContent,'Track & artist abspielen');
select.value='en';select.change();assert.equal(tracked[0].textContent,'Play Track & artist','mounted binding updates without replacing its control');
localStorage.setItem=()=>{throw Error('storage blocked');};select.value='de';select.change();
assert.equal(document.documentElement.lang,'de');assert.match(status.textContent,/Sitzung/);
stored.set('player.language.v1','invalid');handlers.storage({key:'player.language.v1'});assert.equal(select.value,'system');
console.log('Locale resolution, maintained catalog placeholders, metadata isolation, plural counts, mounted bindings and blocked storage passed.');

// Production templates: collection controls and the artist catalog heading must
// update their own UI prose while preserving arbitrary metadata and input nodes.
const { collectionButtons } = await import('../collection-actions.js');
const { musicArtistHeading } = await import('../views/music-catalog.js');
select.value='de';select.change();
const production = collectionButtons('Play album') + musicArtistHeading({roles:[]}, {artist:''});
function readBindings(markup) {
  return [...markup.matchAll(/data-i18n-text="([^"]+)"/g)].map(match=>{
    const data=match[1].replaceAll('&quot;','"').replaceAll('&lt;','<').replaceAll('&gt;','>').replaceAll('&amp;','&');
    return {attr:'data-i18n-text',getAttribute:()=>data};
  });
}
const mounted=readBindings(production);tracked.push(...mounted);
applyTranslations();
assert(mounted.some(element=>element.textContent==='Album abspielen'));
assert(mounted.some(element=>element.textContent==='Interpreten'));
select.value='en';select.change();
assert(mounted.some(element=>element.textContent==='Play album'),'actual collection button binding changes back to English');
assert(mounted.some(element=>element.textContent==='Artists'),'actual mounted catalog heading changes back to English');
assert(mounted.some(element=>element.textContent==='Back to Sets'));
console.log('Production collection controls and catalog heading keep mounted language bindings in both directions.');

const { musicSearchGroup } = await import('../music-search.js');
const { musicLayoutToolbar, normalizeMusicLayout } = await import('../music-layout.js');
const { playlistEditorContent, playlistDialogContent } = await import('../views/music-playlists.js');
function mountProductionBindings(markup) {
  const nodes=readBindings(markup);
  for(const match of markup.matchAll(/data-i18n="([^"]+)"/g)) {
    nodes.push({dataset:{i18n:match[1]},attr:'data-i18n'});
  }
  for(const node of nodes)tracked.push(node);
  return nodes;
}
// Include explicit option bindings as well as span bindings in the fake DOM.
document.querySelectorAll=selector=>selector==='[data-i18n]'
  ? [status,...tracked].filter(e=>e.dataset?.i18n)
  : tracked.filter(e=>selector===`[${e.attr}]` || e.hasBinding?.(selector.slice(1,-1)));
for(const initial of ['de','en']) {
  select.value=initial;select.change();
  for(const editing of [false,true]) {
    const existing=editing?{name:'Songs & Artists',comment:'Albums',visibility:'shared',owner_id:9,can_transfer:true}:null;
    const form=playlistDialogContent(editing?'Edit playlist':'Create playlist',playlistEditorContent(existing,[{id:9,username:'English user'}]));
    const source=musicSearchGroup('Songs',[], '')+musicLayoutToolbar('albums',normalizeMusicLayout({sort:'year',columns:['artist'],descending:true},'albums'))+form;
    const nodes=mountProductionBindings(source);
    // Materialize named controls from the actual editor markup. Attribute
    // updates are supported; replacing control contents is forbidden in this
    // narrow DOM double, so a broad form rewrite cannot silently pass.
    const controls={};
    for(const match of form.matchAll(/<(input|textarea|select)\b([^>]*\bname="([^"]+)"[^>]*)>/g)) {
      const [,tag,attrs,name]=match;
      const bindings=[...attrs.matchAll(/(data-i18n-(?:aria-label|placeholder|title))="([^"]+)"/g)];
      const node={value:'',attributes:new Map(bindings.map(binding=>[binding[1],binding[2].replaceAll('&quot;','"').replaceAll('&amp;','&')])),
        getAttribute(attr){return this.attributes.get(attr);},setAttribute(attr,value){this.attributes.set(attr,value);},
        set innerHTML(_){throw Error(`locale switch replaced ${tag} ${name}`);},
        set textContent(_){throw Error(`locale switch replaced ${tag} ${name}`);}};
      controls[name]=node;
      // An element can have multiple attribute bindings, so provide its real
      // attributes through selector matching rather than a single synthetic key.
      node.hasBinding=attr=>node.attributes.has(attr);
      tracked.push(node);
    }
    controls.name.value='Unsaved English name';controls.comment.value='My unsaved comment';controls.visibility.value='shared';
    const identities={...controls};
    const before=JSON.stringify(controls);
    for(const locale of initial==='de'?['en','de']:['de','en']) {
      select.value=locale;select.change();
      const text=nodes.map(n=>n.textContent);
      for(const key of ['Songs','Year','Artist','↓ Descending',editing?'Edit playlist':'Create playlist',editing?'Save changes':'Create playlist']) {
        assert(text.includes(t(key,{},locale)),`${initial}->${locale}: production binding for ${key}`);
      }
      assert.equal(JSON.stringify(controls),before,'mounted label changes do not replace draft values or select choices');
      for(const name of Object.keys(controls))assert.equal(controls[name],identities[name],'original production form controls remain mounted');
      if(editing)assert(form.includes('Songs &amp; Artists'),'playlist metadata stays verbatim');
    }
  }
}
console.log('Production search headings, layout options/columns/direction and create/edit playlist dialogs switch both directions with form state retained.');

// Smart editor passes the canonical title key to the shared production dialog.
const { openPlaylistRules } = await import('../views/music-playlist-rules.js');
for (const initial of ['de', 'en']) {
  select.value=initial;select.change();
  let titleKey;
  const stop=Symbol('captured smart editor template');
  assert.throws(()=>openPlaylistRules({rules:null}, {
    dialog:(title,content)=>{titleKey=title;throw stop;},
    current:()=>true,saved:()=>{},save:async()=>{},
  }),error=>error===stop);
  assert.equal(titleKey,'Smart playlist rules','dialog receives a canonical key, never pretranslated prose');
  const bindings=mountProductionBindings(playlistDialogContent(titleKey,''));
  for (const locale of ['de','en']) {
    select.value=locale;select.change();
    assert(bindings.some(node=>node.textContent===t('Smart playlist rules',{},locale)),`${initial}->${locale}: smart editor title updates`);
  }
}
console.log('Production smart editor title keeps a canonical binding in both locale directions.');


// Exercise the real keyboard handler and the real Undo click handler against the
// real queue, under German. Keyboard/action tokens never use translated prose.
const { insertQueue, queueState } = await import('../queue.js');
const { initQueuePanel, queueKey } = await import('../queue-panel.js');
const panelEvents={};
const panel={parentElement:{},classList:{contains:()=>false},addEventListener:(type,listener)=>panelEvents[type]=listener,querySelector:()=>({focus(){},scrollIntoView(){}})};
elements.set('queue-panel',panel);
document.addEventListener=()=>{};
await insertQueue([{id:101,type:'audio',file_name:'English Song',duration:10},{id:102,type:'audio',file_name:'German Song',duration:10}], 'append');
const originalQueue=queueState().entries.map(entry=>entry.id);
initQueuePanel();select.value='de';select.change();
let prevented=false;
assert(queueKey({key:'Delete',target:{closest:()=>null},preventDefault(){prevented=true;}}));
assert(prevented);assert.equal(queueState().entries.length,originalQueue.length-1,'physical Delete removes a real queue occurrence in German');
const undoAction={dataset:{queueAction:'undo'},closest:()=>null};
await panelEvents.click({target:{closest:()=>undoAction}});
assert.deepEqual(queueState().entries.map(entry=>entry.id),originalQueue,'real Undo click restores the exact queue occurrences');
console.log('German production queue Delete and Undo retain canonical keyboard/action tokens.');
