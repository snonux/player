import { test, expect, request, type Page, type Locator } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { writeFileSync } from 'node:fs';
import { genuineTouchTap } from './helpers/genuine-touch.cjs';
import { join } from 'node:path';
import { Evidence, loadFixture, SUPPORT_DIR, track, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, selectByKeys, expectPlaying, audioState, captureOutput, assertValidCapture, mainSegment } from './helpers/music-ui';
import { settleMusicViewport } from './helpers/music-viewport';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE,'Requires disposable local music fixture');
let fixture:MusicFixture,evidence:Evidence;
test.beforeAll(async({browser},info)=>{fixture=loadFixture();evidence=new Evidence(fixture.evidenceDir,info.project.name,{browser_version:browser.version(),server_version:fixture.serverVersion,server_build:fixture.buildRevision,checkout:fixture.checkout});});
test.beforeEach(async({page})=>{await page.addInitScript(installAudioTap);await keyboardLogin(page,fixture.user);});
async function search(page:Page,text:string) {
 await page.keyboard.press('/');await expect(page.locator('#search-input')).toBeFocused();
 await page.locator('#search-input').fill(text);await page.keyboard.press('Enter');
 await expect(page.getByRole('heading',{name:'Search results',exact:true})).toBeVisible();
 await expect(page.getByRole('heading',{name:/^Songs \(/})).toBeVisible();
}
async function albums(page:Page) {
 await page.getByRole('button',{name:'Toggle sets sidebar',exact:true}).click();
 await page.getByRole('button',{name:'Albums',exact:true}).click();
 await expect(page.locator('[data-album]').first()).toBeVisible();
}

async function minimizePlayingView(page:Page,touch=false) {
 const player=page.locator('#player');
 await expect(player).toHaveClass(/\bopen\b/);await expect(player).not.toHaveClass(/\bhidden\b/);
 if(!((await player.getAttribute('class')) || '').split(/\s+/).includes('minimized')) {
  const control=page.getByRole('button',{name:'Minimize player (Shift+C)',exact:true});
  await expect(control).toBeVisible();
  if(touch)await genuineTouchTap(page,control);else await control.click();
 }
 await expect(player).toHaveClass(/\bminimized\b/);
}

test('M07 title, artist and album grouped entities; keyboard/pointer source playback and Files',async({page})=>{
 const artifacts:string[]=[];
 try {
  const song=track(fixture,'Music/Tag Verification/source_001.flac');
  await albums(page);
  const original=page.getByRole('button',{name:'Open album Metadata Album by Album Ensemble',exact:true});
  await selectByKeys(page,'Open album Metadata Album by Album Ensemble',200);await page.keyboard.press('Enter');
  await expect(page.getByRole('heading',{name:'Metadata Album',exact:true})).toBeVisible();
  await search(page,'A Different Music Title');
  await expect(page.getByRole('heading',{name:'Songs (1)',exact:true})).toBeVisible();
  await expect(page.locator('[data-search-kind=song].selected')).toBeFocused();
  await page.keyboard.press('Enter');await expectPlaying(page,song.id);
  const capture=await captureOutput(page,1400,evidence.path('m07-metadata-source.wav'));assertValidCapture(capture);
  const segment=mainSegment(capture.analysis);expect(Math.abs(segment!.tone_hz!-song.tone_hz[0])).toBeLessThan(5);artifacts.push(capture.wav);
  await page.keyboard.press('p');await minimizePlayingView(page);
  await search(page,'A & B; C/D');
  await expect(page.getByRole('heading',{name:'Songs (1)',exact:true})).toBeVisible();
  await expect(page.getByRole('button',{name:'Open artist A & B; C/D',exact:true})).toBeVisible();
  await selectByKeys(page,'Open artist A & B; C/D',20);await page.keyboard.press('Enter');
  await expect(page.getByRole('heading',{name:'A & B; C/D',exact:true})).toBeVisible();
  await page.keyboard.press('Backspace');await expect(page.locator('[data-artist-card].selected')).toBeFocused();
  await expect(page.locator('#media-grid')).toContainText('A & B; C/D');
  await search(page,'Metadata Album');
  await page.getByRole('button',{name:'Open album Metadata Album by Album Ensemble',exact:true}).click();
  await expect(page.getByRole('heading',{name:'Metadata Album',exact:true})).toBeVisible();
  await page.keyboard.press('Backspace');await expect(page.locator('[data-album].selected')).toBeFocused();
  for(const [theme,width] of [['dark',1366],['light',390]] as const) {
   await settleMusicViewport(page,width);
   if(await page.locator('html').getAttribute('data-theme')!==theme)await page.getByRole('button',{name:'Toggle theme',exact:true}).click();
   await search(page,'Long music title');
   await expect(page.locator('[data-search-kind=song]')).toHaveCount(1);
   await expect.poll(()=>page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
   const path=evidence.path(`m07-${theme}-${width}.png`);await page.screenshot({path,fullPage:true});artifacts.push(path);
  }
  const retained=await audioState(page);expect(retained.src).toMatch(new RegExp(`/media/${song.id}/`));expect(retained.paused).toBe(true);
  await search(page,'source_001');
  await expect(page.getByRole('heading',{name:'Songs (0)',exact:true})).toBeVisible();
  await expect(page.getByRole('heading',{name:'Files (1)',exact:true})).toBeVisible();
  await page.locator('[data-search-kind=file]').click();await expectPlaying(page,song.id);
  await page.keyboard.press('p');await minimizePlayingView(page);
  await page.keyboard.press('Escape');
  await expect(page.getByRole('heading',{name:'Metadata Album',exact:true})).toBeVisible();
  await page.keyboard.press('Backspace');await expect(original).toBeVisible();
  evidence.record({feature:'M07',scenario:'grouped metadata entities, restored detail selection, source/audio and Files compatibility',result:'pass',data:{song,audio:segment},artifacts});
 }catch(error){evidence.record({feature:'M07',scenario:'grouped entities and playback',result:'fail',detail:String(error),artifacts});throw error;}
});

test('M07 Unicode, literal queries, no results, fast typing, URL refresh and file syntax',async({page})=>{
 try {
  await albums(page);
  await search(page,'ПАРТИЯ');await expect(page.locator('[data-search-kind=song]')).toHaveCount(1);
  await expect(page.locator('[data-search-kind=song]')).toContainText('Партия для двоих');
  await search(page,'artist:"Mika Sørensen" album:"東京 Nights"');await expect(page.locator('[data-search-kind=song]')).toHaveCount(1);
  await page.reload();await expect(page.locator('[data-search-kind=song]')).toHaveCount(1);
  await expect(page.locator('[data-search-kind=song]')).toContainText('Партия для двоих');
  await search(page,'Kei');await expect(page.getByRole('button',{name:'Open artist Aurora Lane feat. Kei',exact:true})).toBeVisible();
  await expect(page.getByRole('button',{name:'Open artist Kei',exact:true})).toHaveCount(0);
  let releaseOld:()=>void=()=>{};
  let oldStarted:()=>void=()=>{};
  let oldFinished:()=>void=()=>{},oldFailed:(error:unknown)=>void=()=>{};
  const held=new Promise<void>(resolve=>releaseOld=resolve),started=new Promise<void>(resolve=>oldStarted=resolve);
  const fulfilled=new Promise<void>((resolve,reject)=>{oldFinished=resolve;oldFailed=reject;});
  await page.route('**/api/music/search?**',async route=>{
   if(new URL(route.request().url()).searchParams.get('q')!=='Old held request')return route.continue();
   try {const response=await route.fetch();oldStarted();await held;await route.fulfill({response});oldFinished();}
   catch(error){oldFailed(error);}
  });
  await page.keyboard.press('/');await page.locator('#search-input').fill('Old held request');await started;
  await page.locator('#search-input').fill('A Different Music Title');await page.keyboard.press('Enter');
  await expect(page.locator('[data-search-kind=song]')).toHaveCount(1);
  releaseOld();await fulfilled;await page.unroute('**/api/music/search?**');
  await expect(page.locator('[data-search-kind=song]')).toContainText('A Different Music Title');
  await search(page,'100%_no-match');await expect(page.getByText('Nothing matches these filters.',{exact:false})).toBeVisible();
  await page.keyboard.press('/');await page.locator('#search-input').fill('r/h/l/?');
  await expect(page.locator('#search-input')).toBeFocused();await expect(page.locator('#search-input')).toHaveValue('r/h/l/?');
  await page.locator('#search-input').fill('discard me');await page.keyboard.press('Escape');
  // The original route after refresh was itself a search; Escape has a safe Library fallback.
  await expect(page.getByRole('heading',{name:'Search results',exact:true})).toHaveCount(0);
  await page.keyboard.press('/');await page.locator('#search-input').fill('type:audio source_001');await page.keyboard.press('Enter');
  await expect(page.getByRole('heading',{name:'Search results',exact:true})).toHaveCount(0);
  await expect(page.locator('#media-grid [data-id]')).toHaveCount(1);
  evidence.record({feature:'M07',scenario:'Unicode scopes, real artist punctuation, no-results, immediate Enter/typing/Escape, refreshed URL and file syntax',result:'pass'});
 }catch(error){evidence.record({feature:'M07',scenario:'query/state boundaries',result:'fail',detail:String(error)});throw error;}
});

test('M07 real offline Retry, revoked permissions and same-database server restart',async({page,context})=>{
 const admin=await request.newContext({baseURL:fixture.origin});let permission:any;
 try {
  await search(page,'Metadata Album');
  const before=await (await page.request.get('/api/v1/music/search?q=Metadata%20Album')).json();
  await context.setOffline(true);await searchFailure(page,'Different');
  await context.setOffline(false);await page.getByRole('button',{name:'Retry',exact:true}).click();
  await expect(page.locator('[data-search-kind=song]')).toHaveCount(1);
  expect((await admin.post('/api/v1/auth/login',{data:fixture.admin})).ok()).toBe(true);
  const users=await(await admin.get('/api/v1/admin/users')).json();
  const uid=users.find((user:any)=>user.username===fixture.user.username).id;
  permission={set_id:fixture.setIds.Music,user_id:uid};
  expect((await admin.delete('/api/v1/admin/permissions',{data:permission})).ok()).toBe(true);
  await search(page,'Metadata Album');await expect(page.getByText('Nothing matches these filters.',{exact:false})).toBeVisible();
  const denied=await(await page.request.get('/api/v1/music/search?q=Secret')).json();expect(denied).toEqual({songs:[],albums:[],artists:[]});
  expect((await admin.post('/api/v1/admin/permissions',{data:{...permission,role:'viewer'}})).ok()).toBe(true);permission=null;
  execFileSync('python3',[join(SUPPORT_DIR,'music_fixture.py'),'restart',fixture.dir]);
  await search(page,'Metadata Album');await expect(page.locator('[data-search-kind=song]')).toHaveCount(1);
  const after=await(await page.request.get('/api/v1/music/search?q=Metadata%20Album')).json();
  expect(after.songs.map((song:any)=>song.id)).toEqual(before.songs.map((song:any)=>song.id));expect(after.albums.map((album:any)=>album.album_key)).toEqual(before.albums.map((album:any)=>album.album_key));
  evidence.record({feature:'M07',scenario:'real offline Retry, immediate permission exclusion and restart identity',result:'pass',data:{before,after}});
 }catch(error){evidence.record({feature:'M07',scenario:'offline/permissions/restart',result:'fail',detail:String(error)});throw error;}
 finally{await context.setOffline(false);if(permission)await admin.post('/api/v1/admin/permissions',{data:{...permission,role:'viewer'}});await admin.dispose();}
});
async function searchFailure(page:Page,text:string) {
 await page.keyboard.press('/');await page.locator('#search-input').fill(text);await page.keyboard.press('Enter');
 await expect(page.getByRole('alert')).toContainText('Could not search music');
 await expect(page.getByRole('button',{name:'Retry',exact:true})).toBeVisible();
}

test.describe('M07 genuine touch Files activation',()=>{
 test.use({hasTouch:true});
 test('Files touch button and existing row action menu both open the actual file',async({page})=>{
  const song=track(fixture,'Music/Tag Verification/source_001.flac');
  const requests:unknown[]=[],targets:unknown[]=[];
  let phase='foreground';
  const relevant=(url:string)=>/\/api\/(?:v1\/)?(?:media\/|progress)/.test(new URL(url).pathname);
  page.on('request',request=>{if(requests.length<60 && relevant(request.url()))requests.push({event:'request',path:new URL(request.url()).pathname,method:request.method()});});
  page.on('response',response=>{if(requests.length<60 && relevant(response.url()))requests.push({event:'response',path:new URL(response.url()).pathname,status:response.status()});});
  const diagnostic=async()=>{
   let state:unknown;
   let timer:ReturnType<typeof setTimeout>|undefined;
   try {state=await Promise.race([page.evaluate(()=>{
    const audio=document.getElementById('media-audio') as HTMLAudioElement;
    return {events:(window as any).m07TouchEvents || [],visibility:document.visibilityState,focus:document.hasFocus(),active:document.activeElement?.outerHTML.slice(0,240),audio:{src:audio?.currentSrc,paused:audio?.paused,time:audio?.currentTime,error:audio?.error?.message},activation:{active:navigator.userActivation?.isActive,ever:navigator.userActivation?.hasBeenActive}};
   }),new Promise((_,reject)=>{timer=setTimeout(()=>reject(new Error('Touch diagnostic page read timed out')),4000);})]);}catch(error){state={diagnosticError:String(error)};}finally{clearTimeout(timer);}
   const path=evidence.path('m07-real-touch-diagnostic.json');
   writeFileSync(path,JSON.stringify({phase,requests,targets,state},null,2));return path;
  };
  const tap=async(locator:Locator,name:string)=>{
   phase=name;
   targets.push(await locator.evaluate(element=>{
    const rect=element.getBoundingClientRect(),hit=document.elementFromPoint(rect.x+rect.width/2,rect.y+rect.height/2);
    return {label:element.getAttribute('aria-label') || element.textContent?.slice(0,160),rect:{x:rect.x,y:rect.y,width:rect.width,height:rect.height},hit:hit?.outerHTML.slice(0,240),visibility:document.visibilityState,focus:document.hasFocus()};
   }));
   await genuineTouchTap(page,locator);
  };
  try {
   if(page.context().browser()?.browserType().name()==='firefox')await settleMusicViewport(page,page.viewportSize()!.width);
   await search(page,'source_001');
   await expect(page.getByRole('heading',{name:'Files (1)',exact:true})).toBeVisible();
   const file=page.locator('[data-search-kind=file]');
   await page.evaluate(()=>{
    (window as any).m07TouchEvents=[];
    // Window capture precedes the shared document-capture row-action handler.
    for(const type of ['pointerdown','pointerup','touchstart','touchend','click'])window.addEventListener(type,event=>{
     const events=(window as any).m07TouchEvents;if(events.length>=80)return;
     const pointer=event as PointerEvent,target=event.target as Element;
     events.push({event:type,trusted:event.isTrusted,type:pointer.pointerType || (type.startsWith('touch')?'touch':''),target:target?.getAttribute?.('aria-label') || target?.textContent?.slice(0,160),tag:target?.tagName,visibility:document.visibilityState,focus:document.hasFocus()});
    },true);
   });
   const open=file.getByRole('button',{name:'Open file source_001.flac',exact:true});
   await expect(open).toBeVisible();
   await tap(open,'direct file open');
   await expectPlaying(page,song.id);
   await page.keyboard.press('p');
   phase='minimize';await minimizePlayingView(page,true);
   await tap(file.locator('.text-muted'),'row metadata opens menu');
   const actions=page.getByRole('group',{name:'Media actions',exact:true});
   await expect(actions).toBeVisible();
   expect((await audioState(page)).paused).toBe(true);
   await expect(actions.getByRole('button',{name:'Open file source_001.flac',exact:true})).toBeVisible();
   await tap(actions.getByRole('button',{name:'Open file source_001.flac',exact:true}),'menu file open');
   await expect(actions).toHaveCount(0);await expectPlaying(page,song.id);
   const clicks=await page.evaluate(()=>(window as any).m07TouchEvents.filter((event:any)=>event.event==='click' && event.type==='touch'));
   expect(clicks.length).toBeGreaterThanOrEqual(4);
   expect(clicks.every((event:any)=>event.trusted && event.type==='touch')).toBe(true);
   phase='complete';const artifact=await diagnostic();
   evidence.record({feature:'M07',scenario:'trusted browser touch opens Files directly and through existing row action menu',result:'pass',data:{clicks,source:await audioState(page)},artifacts:[artifact]});
  }catch(error){const artifact=await diagnostic();evidence.record({feature:'M07',scenario:'trusted touch Files activation',result:'fail',detail:String(error),artifacts:[artifact]});throw error;}
 });
});
