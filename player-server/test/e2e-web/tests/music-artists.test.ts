import { test, expect, request, type Page } from '@playwright/test';
import { Evidence, loadFixture, track, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, selectByKeys, expectPlaying, captureOutput, assertValidCapture, mainSegment } from './helpers/music-ui';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'Requires disposable music fixture');
let fixture:MusicFixture,evidence:Evidence;
test.beforeAll(async({browser},info)=>{fixture=loadFixture();evidence=new Evidence(fixture.evidenceDir,info.project.name,{browser_version:browser.version(),server_version:fixture.serverVersion,server_build:fixture.buildRevision,checkout:fixture.checkout});});
test.beforeEach(async({page})=>{await page.addInitScript(installAudioTap);await keyboardLogin(page,fixture.user);});
async function artists(page:Page) {
 await page.getByRole('button',{name:'Toggle sets sidebar',exact:true}).click();
 await page.getByRole('button',{name:'Artists',exact:true}).click();
 await expect(page.getByRole('heading',{name:'Artists',exact:true})).toBeVisible();
}

test('M03 artist roles, discography links, keyboard Back selection, themes and actual source',async({page})=>{
 const shots:string[]=[];
 try {
  await artists(page);
  await expect(page.getByRole('combobox',{name:'Role',exact:true})).toHaveValue('artists');
  await expect(page.getByRole('button',{name:'Open artist Composer; 東京 / A',exact:true})).toHaveCount(0);
  const list=await (await page.request.get('/api/v1/music/artists')).json();
  expect(list.some((p:any)=>p.name==='Secret Artist')).toBe(false);
  expect(list.some((p:any)=>p.name==='A & B; C/D')).toBe(true);
  expect(list.some((p:any)=>p.name==='A & B')).toBe(false);
  for(const [role,name] of [['track_artist','Neon Coast'],['album_artist','Various Artists'],['composer','Composer; 東京 / A'],['performer','Session Ensemble']]) {
   await page.getByRole('combobox',{name:'Role',exact:true}).selectOption(role);
   await expect(page.getByRole('button',{name:'Open artist '+name,exact:true})).toBeVisible();
   const people=await (await page.request.get('/api/v1/music/artists?role='+role)).json();
   expect(people.every((p:any)=>p.roles.includes(role))).toBe(true);
   expect(people.some((p:any)=>p.name.startsWith('Hidden '))).toBe(false);
  }
  await page.getByRole('button',{name:'Open artist Session Ensemble',exact:true}).click();
  await expect(page.getByRole('heading',{name:'Session Ensemble',exact:true})).toBeVisible();
  await expect(page.getByRole('button',{name:'Open album 東京 Nights (夜) by Ünïcødé Artïst',exact:true})).toBeVisible();
  await page.keyboard.press('Backspace');
  await page.getByRole('combobox',{name:'Role',exact:true}).selectOption('composer');
  await expect(page.getByRole('button',{name:'Open artist Composer; 東京 / A',exact:true})).toBeVisible();
  await selectByKeys(page,'Open artist Composer; 東京 / A',100);
  await page.keyboard.press('Enter');
  await expect(page.getByRole('heading',{name:'Composer; 東京 / A',exact:true})).toBeVisible();
  const composer=(await (await page.request.get('/api/v1/music/artists?role=composer')).json()).find((p:any)=>p.name==='Composer; 東京 / A');
  expect(composer.album_count).toBe(2);expect(composer.song_count).toBe(3);
  await selectByKeys(page,'Open album Synth Compilation by Various Artists',100);await page.keyboard.press('Enter');
  await expect(page.getByRole('heading',{name:'Synth Compilation',exact:true})).toBeVisible();
  await expect(page.getByRole('button',{name:'Album artist: Various Artists',exact:true})).toBeVisible();
  await expect(page.getByRole('button',{name:'Track artist: Neon Coast',exact:true})).toBeVisible();
  await page.keyboard.press('Backspace');
  await expect(page.getByRole('heading',{name:'Composer; 東京 / A',exact:true})).toBeVisible();
  await expect(page.locator('[data-album].selected')).toBeFocused();
  await expect(page.locator('[data-album].selected')).toHaveAttribute('aria-label','Open album Synth Compilation by Various Artists');
  await page.keyboard.press('Backspace');
  await expect(page.getByRole('combobox',{name:'Role',exact:true})).toHaveValue('composer');
  await expect(page.locator('[data-artist-card].selected')).toBeFocused();
  await expect(page.locator('[data-artist-card].selected')).toHaveAttribute('data-artist-card',composer.artist_key);
  // Structured people remain separate links; no display-string splitting.
  await page.getByRole('combobox',{name:'Role',exact:true}).selectOption('track_artist');
  await page.getByRole('button',{name:'Open artist Mika Sørensen',exact:true}).click();
  await page.getByRole('button',{name:'Open album 東京 Nights (夜) by Ünïcødé Artïst',exact:true}).click();
  await expect(page.getByRole('button',{name:'Track artist: Mika Sørensen',exact:true})).toBeVisible();
  await expect(page.getByRole('button',{name:'Track artist: Ünïcødé Artïst',exact:true})).toHaveCount(2);
  await page.getByRole('button',{name:'Track artist: Mika Sørensen',exact:true}).click();
  await expect(page.getByRole('heading',{name:'Mika Sørensen',exact:true})).toBeVisible();
  for(const [theme,width] of [['dark',1366],['light',1366],['dark',390],['light',390]] as const) {
   await page.setViewportSize({width,height:844});
   if(await page.locator('html').getAttribute('data-theme')!==theme) await page.getByRole('button',{name:'Toggle theme',exact:true}).click();
   await expect.poll(()=>page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
   const path=evidence.path(`m03-artist-${theme}-${width}.png`);await page.screenshot({path,fullPage:true,animations:'disabled'});shots.push(path);
  }
  await page.getByRole('button',{name:'Open album 東京 Nights (夜) by Ünïcødé Artïst',exact:true}).click();
  const song=track(fixture,'Music/Ünïcødé Artïst - 東京 Nights/02 - Партия для двоих.flac');
  await page.locator(`[data-id="${song.id}"]`).focus();await page.keyboard.press('i');
  await expect(page.locator('#media-info-modal')).toContainText('Composer');
  await expect(page.locator('#media-info-modal')).toContainText('Composer; 東京 / A, Mika Sørensen');
  await expect(page.locator('#media-info-modal')).toContainText('Performer');
  await expect(page.locator('#media-info-modal')).toContainText('Session Ensemble, Mika Sørensen');
  await page.keyboard.press('Escape');
  await expect(page.locator(`[data-id="${song.id}"]`)).toBeFocused();
  await page.locator(`[data-id="${song.id}"] .song-open`).click();await expectPlaying(page,song.id);
  const capture=await captureOutput(page,1400,evidence.path('m03-source.wav'));assertValidCapture(capture);
  const segment=mainSegment(capture.analysis);expect(segment?.duration_s).toBeGreaterThan(.8);expect(Math.abs(segment!.tone_hz!-song.tone_hz[0])).toBeLessThan(5);
  evidence.record({feature:'M03',scenario:'roles, structured people, discography Back focus, themes and original source',result:'pass',data:{composer,audio:segment},artifacts:[...shots,capture.wav]});
 } catch(error) {evidence.record({feature:'M03',scenario:'roles and discography',result:'fail',detail:String(error),artifacts:shots});throw error;}
});

test('M03 actual offline Retry, permission empty and artist denial',async({page,context})=>{
 await artists(page);
 const person=(await (await page.request.get('/api/v1/music/artists?role=composer')).json()).find((p:any)=>p.name==='Composer; 東京 / A');
 await page.getByRole('combobox',{name:'Role',exact:true}).selectOption('composer');
 await context.setOffline(true);
 await page.getByRole('button',{name:'Open artist Composer; 東京 / A',exact:true}).click();
 await expect(page.getByRole('alert')).toContainText('Could not load artists');
 await context.setOffline(false);await page.getByRole('button',{name:'Retry',exact:true}).click();
 await expect(page.getByRole('heading',{name:person.name,exact:true})).toBeVisible();
 await page.keyboard.press('Backspace');
 // / remains ordinary native text input; explicit Artists navigation cancels pending search.
 await page.keyboard.press('/');await page.locator('#search-input').fill('r/h/l/?');
 await expect(page.locator('#search-input')).toHaveValue('r/h/l/?');
 await page.keyboard.press('Escape');
 await artists(page);
 const admin=await request.newContext({baseURL:fixture.origin});
 let userId=0;
 try {
  expect((await admin.post('/api/v1/auth/login',{data:fixture.admin})).ok()).toBe(true);
  const users=await (await admin.get('/api/v1/admin/users')).json();
  userId=users.find((u:any)=>u.username===fixture.user.username).id;
  expect((await admin.delete('/api/v1/admin/permissions',{data:{set_id:fixture.setIds.Music,user_id:userId}})).status()).toBe(200);
  await page.reload();
  await expect(page.getByText('No artists available for this role.',{exact:false})).toBeVisible();
  expect((await page.request.get('/api/v1/music/artists/'+person.artist_key)).status()).toBe(404);
  evidence.record({feature:'M03',scenario:'real offline request Retry, typing, permission empty and unavailable artist',result:'pass'});
 } finally {
  if(userId) expect((await admin.post('/api/v1/admin/permissions',{data:{set_id:fixture.setIds.Music,user_id:userId,role:'viewer'}})).status()).toBe(200);
  await admin.dispose();
 }
});


test('M03 scrolled Artists and long discography restore exact visible position',async({page})=>{
 await page.setViewportSize({width:390,height:620});await artists(page);
 const people=await (await page.request.get('/api/v1/music/artists')).json();
 const person=people.find((p:any)=>p.name.startsWith('Zzz Catalog Artist'));
 const card=page.locator(`[data-artist-card="${person.artist_key}"]`);
 await card.scrollIntoViewIfNeeded();await card.focus();
 const before=await page.evaluate(()=>scrollY);expect(before).toBeGreaterThan(0);
 const cardY=(await card.boundingBox())!.y;
 await page.keyboard.press('Enter');
 await expect(page.getByRole('heading',{name:person.name,exact:true})).toBeVisible();
 const release=page.getByRole('button',{name:'Open album Catalog Release 14 by '+person.name,exact:true});
 await release.scrollIntoViewIfNeeded();await release.focus();
 const detailScroll=await page.evaluate(()=>scrollY);expect(detailScroll).toBeGreaterThan(0);
 const releaseY=(await release.boundingBox())!.y;
 await page.keyboard.press('Enter');
 await expect(page.getByRole('heading',{name:'Catalog Release 14',exact:true})).toBeVisible();
 await page.keyboard.press('Backspace');
 await expect(page.getByRole('heading',{name:person.name,exact:true})).toBeVisible();
 await expect(release).toBeFocused();
 await expect.poll(async()=>Math.abs((await page.evaluate(()=>scrollY))-detailScroll)).toBeLessThan(3);
 expect(Math.abs((await release.boundingBox())!.y-releaseY)).toBeLessThan(3);
 await page.keyboard.press('Backspace');await expect(card).toBeFocused();
 await expect.poll(async()=>Math.abs((await page.evaluate(()=>scrollY))-before)).toBeLessThan(3);
 expect(Math.abs((await card.boundingBox())!.y-cardY)).toBeLessThan(3);
 const shots:string[]=[];
 await page.keyboard.press('Enter');
 for(const theme of ['dark','light']) {
  if(await page.locator('html').getAttribute('data-theme')!==theme) await page.getByRole('button',{name:'Toggle theme',exact:true}).click();
  const path=evidence.path('m03-long-discography-'+theme+'.png');await page.screenshot({path,fullPage:true,animations:'disabled'});shots.push(path);
 }
 evidence.record({feature:'M03',scenario:'genuinely scrolled Artists and discography Back restores exact focus and position',result:'pass',data:{before,detailScroll,cardY,releaseY},artifacts:shots});
});
