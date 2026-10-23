import { t, html, ui } from '../i18n.js';
import { escapeHtml as esc } from '../utils.js';

export const ruleFields = {
 title: 'Title', artist: 'Artist', album: 'Album', genre: 'Genre', year: 'Year',
 plays: 'Your plays', rating: 'Your rating', starred: 'Your favorite',
 added: 'File added', last_played: 'Your last play',
};
const operatorLabels = { contains: 'contains', is: 'is', eq: 'equals', gte: 'at least', lte: 'at most', before: 'before date (UTC)', after: 'after date (UTC)', within_days: 'within the last N days' };
const sorts = ['title', 'artist', 'album', 'year', 'plays', 'rating', 'added', 'last_played'];
const personalNote = 'Your plays, favorites and ratings use your account. Shared smart playlists can show different songs to each viewer.';
export function ruleOperators(field) {
 if (['title','artist','album','genre'].includes(field)) return ['contains','is'];
 if (field === 'starred') return ['is'];
 if (['added','last_played'].includes(field)) return ['before','after','within_days'];
 return ['eq','gte','lte'];
}
export function playlistRulesSummary(rules) {
 if (!rules) return t('Manual playlist');
 const conditions = rules.conditions.map(c => c.operator === 'within_days' ? `${t(ruleFields[c.field])} ${t('within the last {days} days',{days:c.value})}` : `${t(ruleFields[c.field] || c.field)} ${t(operatorLabels[c.operator] || c.operator)} ${c.field === 'starred' ? t(c.value === 'true' ? 'Yes' : 'No') : c.value}`).join('; ');
 return t('Match {mode}: {conditions}. Order: {sort}, {direction}; up to {limit} songs.', {
  mode:t(rules.match === 'all' ? 'all' : 'any'), conditions,
  sort:t(ruleFields[rules.sort]),direction:t(rules.descending ? 'Descending' : 'Ascending'),limit:rules.limit,
 });
}
const options = (values, selected, labels = ruleFields) => values.map(value => html`<option data-i18n="${esc(labels[value] || value)}" value="${esc(value)}" ${value === selected ? 'selected' : ''}>${esc(t(labels[value] || value))}</option>`).join('');

// The existing playlist modal supplies focus trapping, Escape and opener restoration.
// Save is injected so this editor shares the normal authenticated API client.
export function openPlaylistRules(playlist, {dialog, current, saved, save, enable = Boolean(playlist.rules)}) {
 const initial = playlist.rules || {match:'all',conditions:[{field:'genre',operator:'contains',value:''}],sort:'title',descending:false,limit:100};
 const rules = JSON.parse(JSON.stringify(initial));
 const {modal,close,alive} = dialog('Smart playlist rules',html`<form data-rule-form><label><input type="checkbox" data-enabled ${enable ? 'checked' : ''}>Enable smart rules</label><p>${ui(personalNote)}</p><p>${ui('Dates use UTC. File added means when the file entered this library.')}</p><fieldset data-fields><label>Match<select data-match>${options(['all','any'],rules.match,{all:'All rules',any:'Any rule'})}</select></label><div data-conditions></div><button class="btn" type="button" data-add-rule>Add rule</button><label>Order by<select data-sort>${options(sorts,rules.sort)}</select></label><label>Direction<select data-direction>${options(['ascending','descending'],rules.descending?'descending':'ascending',{ascending:'Ascending',descending:'Descending'})}</select></label><label>Maximum songs<input type="number" data-limit min="1" max="1000" required value="${rules.limit}"></label></fieldset><p class="playlist-comment" data-summary role="status"></p><p role="alert" data-rule-error></p><div class="playlist-actions"><button type="button" class="btn" data-cancel>Cancel</button><button class="btn btn-primary" type="submit">Save rules</button></div></form>`);
 const form=modal.querySelector('[data-rule-form]'), rows=form.querySelector('[data-conditions]');
 let busy=false;
 const enabled=form.querySelector('[data-enabled]');
 const update=()=>{
  rules.match=form.querySelector('[data-match]').value;
  rules.sort=form.querySelector('[data-sort]').value;
  rules.descending=form.querySelector('[data-direction]').value==='descending';
  rules.limit=Number(form.querySelector('[data-limit]').value);
  form.querySelector('[data-fields]').disabled=!enabled.checked||busy;
  form.querySelector('[data-summary]').textContent=playlistRulesSummary(enabled.checked?rules:null);
  form.querySelector('[data-add-rule]').disabled=rules.conditions.length>=20;
 };
 const draw=()=>{
  rows.innerHTML=rules.conditions.map((c,index)=>html`<fieldset data-row="${index}"><legend>${ui('Rule {number}',{number:index+1})}</legend><label>Field<select data-field aria-label="Rule field">${options(Object.keys(ruleFields),c.field)}</select></label><label>Comparison<select data-operator aria-label="Rule comparison">${options(ruleOperators(c.field),c.operator,operatorLabels)}</select></label><label>Value${c.field==='starred'?html`<select data-value aria-label="Rule value">${options(['true','false'],c.value,{true:'Yes',false:'No'})}</select>`:html`<input data-value aria-label="Rule value" type="${['added','last_played'].includes(c.field)&&c.operator!=='within_days'?'date':['year','plays','rating'].includes(c.field)||c.operator==='within_days'?'number':'text'}" value="${esc(c.value)}" maxlength="300" required>`}</label><button class="btn" type="button" data-remove ${rules.conditions.length===1?'disabled':''}>Remove rule</button></fieldset>`).join('');
  rows.querySelectorAll('[data-row]').forEach(row=>{
   const index=Number(row.dataset.row),c=rules.conditions[index];
   row.querySelector('[data-field]').onchange=e=>{c.field=e.target.value;c.operator=ruleOperators(c.field)[0];c.value=c.field==='starred'?'true':'';draw();rowFocus(index);};
   row.querySelector('[data-operator]').onchange=e=>{c.operator=e.target.value;c.value=c.field==='starred'?'true':'';draw();rowFocus(index,'[data-operator]');};
   row.querySelector('[data-value]').oninput=e=>{c.value=e.target.value;update();};
   row.querySelector('[data-remove]').onclick=()=>{rules.conditions.splice(index,1);draw();rowFocus(Math.min(index,rules.conditions.length-1));};
  });
  update();
 };
 const rowFocus=(index,selector='[data-field]')=>rows.querySelector(`[data-row="${index}"] ${selector}`)?.focus();
 form.querySelector('[data-add-rule]').onclick=()=>{rules.conditions.push({field:'genre',operator:'contains',value:''});draw();rowFocus(rules.conditions.length-1);};
 enabled.onchange=update;
 form.querySelectorAll('[data-match],[data-sort],[data-direction],[data-limit]').forEach(control=>control.oninput=update);
 form.querySelector('[data-cancel]').onclick=close;
 form.onsubmit=async event=>{
  event.preventDefault();if(busy)return;update();busy=true;
  const error=form.querySelector('[data-rule-error]');error.textContent='';
  form.querySelector('[type=submit]').disabled=true;enabled.disabled=true;update();
  try {const result=await save(playlist.playlist_id,enabled.checked?rules:null);if(!alive()||!current())return;close();saved(result);}
  catch(e){if(alive()&&current())error.textContent=e.message||t('Could not save rules. Check your connection and retry.');}
  finally {if(alive()){busy=false;enabled.disabled=false;form.querySelector('[type=submit]').disabled=false;update();}}
 };
 const translated=()=>{if(alive()&&current())update();};
 const observer=new MutationObserver(()=>{if(!alive()){document.removeEventListener('language:changed',translated);observer.disconnect();}});
 document.addEventListener('language:changed',translated);
 observer.observe(modal,{attributes:true,attributeFilter:['class'],childList:true});
 draw();enabled.focus();
}
