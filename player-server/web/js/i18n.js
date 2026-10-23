import { german } from './i18n-de.js';

const storageKey = 'player.language.v1';
export const languages = ['system', 'en', 'de'];
let choice = 'system';
export function resolveLanguage(value, device = globalThis.navigator?.language || 'en') {
  const preferred = languages.includes(value) ? value : 'system';
  return (preferred === 'system' ? device : preferred).toLowerCase().split(/[-_]/)[0] === 'de' ? 'de' : 'en';
}
export function language() { return resolveLanguage(choice); }
export function t(key, values = {}, locale = language()) {
  const message = locale === 'de' ? german[key] || key : key;
  return message.replace(/\{(\w+)\}/g, (match, name) => Object.hasOwn(values, name) ? String(values[name]) : match);
}
export function plural(count, singular, multiple, values = {}) {
  return t(count === 1 ? singular : multiple, { ...values, count });
}
const escape = value => String(value).replaceAll('&', '&amp;').replaceAll('"', '&quot;').replaceAll('<', '&lt;').replaceAll('>', '&gt;');
const marker = /\uE000(\d+)\uE001/g;
const patterns = Object.keys(german).sort((a,b) => b.length-a.length).map(key => {
  const names = [];
  const source = key.replace(/[.*+?^${}()|[\]\\]/g, '\\$&').replace(/\\\{(\w+)\\\}/g, (_, name) => { names.push(name); return '(\\uE000\\d+\\uE001)'; });
  return {key, names, expression: new RegExp(`(?<![\\p{L}])${source}(?![\\p{L}])`, 'gu')};
});
function message(source, values) {
  for (const {key,names,expression} of patterns) {
    expression.lastIndex=0;
    const match=expression.exec(source);
    // Only entire UI fragments are translated; runtime metadata stays opaque.
    if (!match || !['', '←', '▶'].includes(source.slice(0,match.index).trim()) || source.slice(match.index+match[0].length).trim()) continue;
    const args=Object.fromEntries(names.map((name,i)=>[name,values[Number(match[i+1].slice(1,-1))]]));
    return {key,args,encoded:true,prefix:source.slice(0,match.index),suffix:source.slice(match.index+match[0].length)};
  }
  return null;
}
function replaceMarkers(source, values) { return source.replace(marker, (_, at) => values[Number(at)] ?? ''); }
function payload(value) { return escape(JSON.stringify(value)); }
// Explicit tagged UI templates keep interpolated metadata opaque to translation.
// Bindings let mounted dialogs and forms change language without losing input.
export function html(parts, ...values) {
  let source=parts.map((part,i)=>part+(i<values.length?`\uE000${i}\uE001`:'')).join('');
  source=source.replace(/\b(aria-label|title|placeholder|alt)="([^"]*)"/g,(whole,attr,label)=>{
    const translated=message(label,values);
    return translated ? `${attr}="${translated.prefix}${t(translated.key,translated.args)}${translated.suffix}" data-i18n-${attr}="${payload(translated)}"` : whole;
  });
  source=source.replace(/(<option\b[^>]*>)([^<]*)(<\/option>)/g,(whole,open,label,close)=>{
    const translated=message(label,values);
    return translated ? `${open.slice(0,-1)} data-i18n-text="${payload(translated)}">${translated.prefix}${t(translated.key,translated.args)}${translated.suffix}${close}` : whole;
  });
  source=source.replace(/>([^<]+)(?=<)/g,(whole,label,at,all)=>{
    if (all.slice(0,at).match(/<option\b[^>]*$/)) return whole;
    const translated=message(label,values);
    return translated ? `><span data-i18n-text="${payload(translated)}">${translated.prefix}${t(translated.key,translated.args)}${translated.suffix}</span>` : whole;
  });
  return replaceMarkers(source,values);
}
export function ui(key, args = {}) {
  return `<span data-i18n-text="${payload({key,args})}">${escape(t(key,args))}</span>`;
}
export function uiCount(count, singular, multiple) {
  return ui(count === 1 ? singular : multiple, {count});
}
function decoded(value) {
  const field=document.createElement('textarea'); field.innerHTML=value; return field.value;
}
export function applyTranslations(root = document) {
  for (const attr of ['text','aria-label','title','placeholder','alt']) {
    root.querySelectorAll(`[data-i18n-${attr}]`).forEach(element=>{
      try {
        const data=JSON.parse(element.getAttribute(`data-i18n-${attr}`));
        const args=Object.fromEntries(Object.entries(data.args || {}).map(([name,value])=>[name,data.encoded ? decoded(String(value)) : String(value)]));
        const text=(data.prefix || '')+t(data.key,args)+(data.suffix || '');
        if(attr==='text') element.textContent=text; else element.setAttribute(attr,text);
      } catch { /* A stale binding must not prevent the rest of the UI updating. */ }
    });
  }
  root.querySelectorAll('[data-i18n]').forEach(element=>element.textContent=t(element.dataset.i18n));
  document.documentElement.lang=language();
}
export function initLanguage() {
  try { const saved=localStorage.getItem(storageKey); choice=languages.includes(saved)?saved:'system'; } catch { choice='system'; }
  const select=document.getElementById('language-select');
  if(select) {
    select.value=choice;
    select.addEventListener('change',()=>{
      choice=languages.includes(select.value)?select.value:'system';
      let status='Language saved on this device.';
      try { localStorage.setItem(storageKey,choice); } catch { status='Language changed for this session. Device storage is unavailable.'; }
      const feedback=document.getElementById('language-status');
      if(feedback) { feedback.dataset.i18n=status; feedback.textContent=t(status); }
      applyTranslations();
      document.dispatchEvent(new Event('language:changed'));
    });
  }
  window.addEventListener('languagechange',()=>{if(choice==='system')applyTranslations();});
  window.addEventListener('storage',event=>{
    if(event.key!==storageKey && event.key!==null)return;
    try { const saved=localStorage.getItem(storageKey);choice=languages.includes(saved)?saved:'system'; } catch { choice='system'; }
    if(select) select.value=choice;
    applyTranslations();document.dispatchEvent(new Event('language:changed'));
  });
  applyTranslations();
}
