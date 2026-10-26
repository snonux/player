import { test, expect, request } from '@playwright/test';
import { loadFixture, Evidence, type MusicFixture } from './helpers/music-fixture';
import { keyboardLogin, openSidebarMusic } from './helpers/music-ui';
import { genuineTouchTap } from './helpers/genuine-touch.cjs';
import { settleMusicViewport } from './helpers/music-viewport';

test.use({hasTouch:true});
test.skip(!process.env.PLAYER_MUSIC_FIXTURE,'Requires disposable local music fixture');
let fixture:MusicFixture,evidence:Evidence;
test.beforeAll(async({browser},info)=>{fixture=loadFixture();evidence=new Evidence(fixture.evidenceDir,info.project.name,{browser_version:browser.version(),server_build:fixture.buildRevision,checkout:fixture.checkout});});
test.beforeEach(async({page})=>{await keyboardLogin(page,fixture.user);await page.goto(fixture.origin+'/?music=albums');await expect(page.locator('[data-album]').first()).toBeVisible();});
test('M08 same-song facets, chips, keyboard Back, refresh, literal release metadata and Clear',async({page})=>{
 const artifacts:string[]=[];
 try {
  const initial=await page.locator('[data-album]').count();expect(initial).toBeGreaterThan(2);
  await page.getByRole('combobox',{name:'Genre',exact:true}).selectOption('Ambient');
  await page.getByRole('combobox',{name:'Year / decade',exact:true}).selectOption('2021');
  await expect(page.locator('[data-album]')).toHaveCount(1);await expect(page.locator('[data-album]')).toContainText('Night Transit');
  await page.getByText('More filters',{exact:false}).click();await page.getByRole('combobox',{name:'Role',exact:true}).selectOption('composer');
  await expect(page.locator('[data-album]')).toHaveCount(0);await expect(page.getByText('Nothing matches these filters.',{exact:false})).toBeVisible();
  await page.getByRole('button',{name:'Remove Role: Composer',exact:true}).click();
  await expect(page.locator('[data-album]')).toHaveCount(1);
  await page.locator('[data-album]').focus();await page.keyboard.press('Enter');
  await expect(page.getByRole('heading',{name:'Night Transit',exact:true})).toBeVisible();
  await expect(page.locator('.media-row[data-id]')).toHaveCount(3);
  await page.keyboard.press('Backspace');await expect(page.locator('[data-album]')).toHaveCount(1);
  await page.reload();await expect(page.locator('[data-album]')).toHaveCount(1);
  await genuineTouchTap(page,page.getByRole('button',{name:'Clear filters',exact:true}));
  await expect(page.locator('[data-album]')).toHaveCount(initial);
  if(!await page.locator('.music-facets details').evaluate((element:HTMLDetailsElement)=>element.open))await page.locator('.music-facets summary').click();await page.getByRole('combobox',{name:'Label',exact:true}).selectOption('Label A; B');
  await expect(page.locator('[data-album]')).toHaveCount(1);await expect(page.locator('[data-album]')).toContainText('Release Metadata');
  await page.getByRole('combobox',{name:'Year / decade',exact:true}).selectOption('2024');
  await expect(page.locator('[data-album]')).toHaveCount(1);
  for(const [theme,width] of [['dark',1366],['light',390]] as const){await settleMusicViewport(page,width);if(await page.locator('html').getAttribute('data-theme')!==theme)await page.getByRole('button',{name:'Toggle theme',exact:true}).click();const path=evidence.path(`m08-${theme}-${width}.png`);await page.screenshot({path,fullPage:true});artifacts.push(path);expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);}
  evidence.record({feature:'M08',scenario:'facet combination, no cross-song match, full detail, Back/refresh, Clear, release label/year and themes',result:'pass',artifacts});
 }catch(error){evidence.record({feature:'M08',scenario:'facet journey',result:'fail',detail:String(error),artifacts});throw error;}
});
test('M08 extended facets: simultaneous release constraint, decade and rich release controls with focus',async({page})=>{
 try {
  const select=async(name:string,value:string)=>{const field=page.getByRole('combobox',{name,exact:true});await field.focus();await expect(field).toBeFocused();await field.selectOption(value);await expect(field).toBeFocused();};
  const more=page.locator('.music-facets summary');
  await more.focus();await page.keyboard.press('Enter');await expect(page.locator('.music-facets details')).toHaveAttribute('open','');
  await page.keyboard.press('Escape');await expect(more).toBeFocused();await expect(page.locator('.music-facets details')).not.toHaveAttribute('open','');
  await page.keyboard.press('Space');await expect(page.locator('.music-facets details')).toHaveAttribute('open','');
  await select('Genre','Ambient');await select('Year / decade','2021');await select('Role','track_artist');await select('Label','Fixture Records');
  await expect(page.locator('[data-album]')).toHaveCount(1);await expect(page.locator('[data-album]')).toContainText('Night Transit');
  await expect(page.locator('[data-album]')).toContainText('1 matching songs');
  await expect(page.locator('[data-remove-facet]')).toHaveCount(4);
  await select('Release type','album');await expect(page.locator('[data-album]')).toHaveCount(0);
  await page.getByRole('button',{name:'Remove Release type: album',exact:true}).click();await expect(page.locator('[data-album]')).toHaveCount(1);
  await page.locator('[data-album]').focus();await page.keyboard.press('Enter');await expect(page.locator('.media-row[data-id]')).toHaveCount(3);
  await page.keyboard.press('Backspace');await expect(page.locator('[data-album].selected')).toBeFocused();await expect(page.locator('[data-remove-facet]')).toHaveCount(4);
  await page.locator('[data-clear-facets]').click();await expect(page.locator('[data-remove-facet]')).toHaveCount(0);
  await select('Genre','Synthwave');await select('Year / decade','2010-2019');
  if(!await page.locator('.music-facets details').evaluate((el:HTMLDetailsElement)=>el.open))await more.click();
  await select('Compilation','true');await expect(page.locator('[data-album]')).toContainText('Synth Compilation');
  await select('Compilation','false');await expect(page.locator('[data-album]')).toHaveCount(0);
  await page.locator('[data-clear-facets]').click();
  if(!await page.locator('.music-facets details').evaluate((el:HTMLDetailsElement)=>el.open))await more.click();
  for(const [name,value] of [['Year / decade','2024'],['Role','track_artist'],['Release type','live'],['Compilation','false'],['Mood','Reflective'],['Grouping','Suite / One'],['Label','Label A; B']])await select(name,value);
  await expect(page.locator('[data-album]')).toHaveCount(1);await expect(page.locator('[data-album]')).toContainText('Release Metadata');
  await expect(page.locator('[data-remove-facet]')).toHaveCount(7);
  await page.locator('[data-clear-facets]').click();await expect(page.locator('[data-remove-facet]')).toHaveCount(0);
  evidence.record({feature:'M08',scenario:'four simultaneous facets and explicit release-type exclusion, decade/marked compilation, rich release controls and keyboard focus',result:'pass'});
 }catch(error){evidence.record({feature:'M08',scenario:'extended facet controls/focus',result:'fail',detail:String(error)});throw error;}
});

test('M08 Search modifier deletion and Artists facet navigation restore selection',async({page})=>{
 try {
  const input=page.locator('#search-input');
  const search=async(value:string)=>{
   // Blur selects/buttons first — a focused SELECT swallows `/`.
   await page.keyboard.press('Escape');
   await page.keyboard.press('/');
   await expect(input).toBeVisible();
   await input.fill(value);
   await expect(input).toHaveValue(value);
   // press() targets the input even if a prior suite case stole document focus.
   await input.press('Enter');
   await expect(page.getByRole('heading',{name:'Search results',exact:true})).toBeVisible();
  };
  await search('Harbour genre:Ambient year:2022');await expect(page.locator('[data-search-kind=song]')).toHaveCount(0);
  await search('Harbour genre:Ambient');await expect(page.locator('[data-search-kind=song]')).toHaveCount(1);await expect(page.locator('[data-search-kind=song]')).toContainText('Harbour');
  await expect(page.locator('[data-remove-facet=year]')).toHaveCount(0);await expect(page.locator('[data-remove-facet=genre]')).toBeVisible();
  await page.locator('[data-remove-facet=genre]').click();await expect(input).toHaveValue('Harbour');await expect(page.locator('[data-search-kind=song]')).toHaveCount(1);
  await page.getByRole('combobox',{name:'Year / decade',exact:true}).selectOption('2022');await expect(page.locator('[data-search-kind=song]')).toHaveCount(0);
  await page.getByRole('combobox',{name:'Year / decade',exact:true}).selectOption('2021');await expect(page.locator('[data-search-kind=song]')).toHaveCount(1);
  await page.locator('[data-clear-facets]').click();await expect(input).toHaveValue('Harbour');await expect(page.locator('[data-search-kind=song]')).toHaveCount(1);
  // Blur facet selects first — a focused SELECT swallows `/`.
  await page.keyboard.press('Escape');
  await page.keyboard.press('/');
  await expect(input).toBeVisible();
  await input.fill('r/h/l/?');
  await expect(input).toBeFocused();
  await expect(input).toHaveValue('r/h/l/?');
  await page.keyboard.press('Escape');
  await openSidebarMusic(page,'Artists');
  // Artists heading Role is the visible control; facet Role lives under More.
  await page.locator('.music-heading').getByRole('combobox',{name:'Role',exact:true}).selectOption('composer');
  await page.getByRole('combobox',{name:'Genre',exact:true}).selectOption('Synthwave');
  await page.getByRole('combobox',{name:'Year / decade',exact:true}).selectOption('2019');
  const card=page.locator('[data-artist-card]');
  await expect(card).toHaveCount(1);
  await expect(card).toContainText('Composer; 東京 / A');
  await expect(card).toContainText('2 matching songs');
  await card.focus();await page.keyboard.press('Enter');
  await expect(page.getByRole('heading',{name:'Composer; 東京 / A',exact:true})).toBeVisible();
  await expect(page.locator('[data-album]').filter({hasText:'Synth Compilation'})).toHaveCount(1);
  await page.keyboard.press('Backspace');
  await expect(page.locator('[data-artist-card].selected')).toBeFocused();
  await expect(page.locator('.music-heading').getByRole('combobox',{name:'Role',exact:true})).toHaveValue('composer');
  await expect(page.getByRole('combobox',{name:'Genre',exact:true})).toHaveValue('Synthwave');
  await expect(page.getByRole('combobox',{name:'Year / decade',exact:true})).toHaveValue('2019');
  await page.locator('[data-clear-facets]').click();await expect(page.locator('[data-remove-facet]')).toHaveCount(0);
  evidence.record({feature:'M08',scenario:'typed year/genre deletion, typing ownership and composer Artists facets/detail/Back selection',result:'pass'});
 }catch(error){evidence.record({feature:'M08',scenario:'Search/Artists facets',result:'fail',detail:String(error)});throw error;}
});
test('M08 options/results ACL and connection Retry retain chips',async({page})=>{
 try {
  const admin=await request.newContext({baseURL:fixture.origin});try{expect((await admin.post('/api/v1/auth/login',{data:fixture.admin})).ok()).toBe(true);expect((await(await admin.get('/api/music/facets')).json()).genre).toContain('Restricted only');}finally{await admin.dispose();}
  const response=await page.request.get(fixture.origin+'/api/music/facets');expect(response.ok()).toBe(true);const options=await response.json();expect(options.genre).not.toContain('Restricted only');
  await page.getByRole('combobox',{name:'Genre',exact:true}).selectOption('Ambient');
  await page.route('**/api/music/albums?**',route=>route.abort('failed'));
  await page.reload();await expect(page.getByRole('button',{name:'Retry',exact:true})).toBeVisible();
  await expect(page.getByRole('button',{name:'Remove Genre: Ambient',exact:true})).toBeVisible();
  await page.unroute('**/api/music/albums?**');await page.getByRole('button',{name:'Retry',exact:true}).click();await expect(page.locator('[data-album]').first()).toBeVisible();
  evidence.record({feature:'M08',scenario:'authorized options and offline retry preserve applied facets',result:'pass'});
 }catch(error){evidence.record({feature:'M08',scenario:'ACL and retry',result:'fail',detail:String(error)});throw error;}
});
