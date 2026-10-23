import { test, expect, request } from '@playwright/test';
import { loadFixture, Evidence, type MusicFixture } from './helpers/music-fixture';
import { keyboardLogin } from './helpers/music-ui';
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
