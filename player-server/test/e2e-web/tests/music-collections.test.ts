import { test, expect, request, type Page } from '@playwright/test';
import { Evidence, loadFixture, type MusicFixture } from './helpers/music-fixture';
import { installAudioTap } from './helpers/audio-tap';
import { keyboardLogin, expectPlaying, audioState, captureOutput, assertValidCapture, mainSegment } from './helpers/music-ui';

test.skip(!process.env.PLAYER_MUSIC_FIXTURE, 'Requires disposable local Music fixture');
let f:MusicFixture,e:Evidence;
const attempt=Date.now();
const artifact=(name:string)=>e.path(`m15-${attempt}-${name}`);
test.beforeAll(async({browser},info)=>{f=loadFixture();e=new Evidence(f.evidenceDir,info.project.name,{browser_version:browser.version(),server_version:f.serverVersion,server_build:f.buildRevision,checkout:f.checkout});});
test.beforeEach(async({page})=>{await page.addInitScript(installAudioTap);await keyboardLogin(page,f.user);});
const grid=(page:Page)=>page.locator('#media-grid');
async function queue(page:Page) {
 return page.evaluate(async()=>{const s=(await import('/js/queue.js' as string)).queueState();return {ids:s.entries.map((entry:any)=>entry.media.id),occurrences:s.entries.map((entry:any)=>entry.id),index:s.index,label:s.label,active:s.active,loading:s.loading};});
}
async function pause(page:Page) {
 // Native global p remains available when collection Play retains a minimized
 // player. Focus a visible non-input control before dispatching the real key.
 await page.getByRole('button',{name:'Log out',exact:true}).focus();
 await page.keyboard.press('p');await expect.poll(async()=>(await audioState(page)).paused).toBe(true);
}
async function logout(page:Page) {
 await page.getByRole('button',{name:'Log out',exact:true}).click();
 await page.waitForURL(url=>url.pathname==='/login.html');
 await expect(page.locator('#username')).toBeVisible();
}
async function ready(page:Page) {
 await expect.poll(async()=>(await queue(page)).loading).toBe(false);
}
async function playCollection(page:Page,label:string) {
 const button=grid(page).getByRole('button',{name:'▶ '+label,exact:true});
 await expect(button).toBeVisible();await ready(page);await button.click();
}
async function visibleQueue(page:Page,expected:number[]) {
 await page.keyboard.press('Q');await expect(page.locator('#queue-panel')).toHaveClass(/open/);
 await expect(page.locator('#queue-panel .queue-track')).toHaveCount(expected.length);
 expect((await queue(page)).ids).toEqual(expected);
 await page.getByRole('button',{name:'Close queue',exact:true}).click();
}
async function audible(page:Page,id:number,name:string) {
 await expectPlaying(page,id);
 const capture=await captureOutput(page,1200,artifact(name+'.wav'));assertValidCapture(capture);
 const segment=mainSegment(capture.analysis),expected=f.tracks.find(t=>f.mediaIds[t.path]===id)!;
 expect(segment!.duration_s).toBeGreaterThan(.7);expect(Math.abs(segment!.tone_hz!-expected.tone_hz[0])).toBeLessThan(5);
 await pause(page);await page.keyboard.press('Escape');
 return {id,audio:segment,wav:capture.wav};
}
async function enqueue(page:Page,action:string) {
 await ready(page);
 await grid(page).getByRole('button',{name:'Queue actions ▾',exact:true}).click();
 await page.getByRole('group',{name:'Collection queue actions',exact:true}).getByRole('button',{name:action,exact:true}).click();
 await expect(grid(page).locator('[data-collection-status]').filter({hasText:/Added/})).toBeVisible();
}
async function fixtures(page:Page) {
 const albums=await(await page.request.get('/api/v1/music/albums')).json();
 const a=albums.find((item:any)=>item.title==='Night Transit');
 const b=albums.find((item:any)=>item.title==='Synth Compilation');
 expect(a&&b).toBeTruthy();
 const details=await Promise.all([a,b].map(async item=>(await page.request.get('/api/v1/music/albums/'+item.album_key)).json()));
 const artists=await(await page.request.get('/api/v1/music/artists')).json();
 const artist=artists.find((item:any)=>item.name==='Aurora Lane');expect(artist).toBeTruthy();
 const make=async(name:string,ids:number[])=>{const p=await(await page.request.post('/api/v1/playlists',{data:{name,comment:'M15 isolated acceptance fixture'}})).json();await page.request.post(`/api/v1/playlists/${p.playlist_id}/songs`,{data:{action:'add',media_ids:ids}});return {...p,ids};};
 const playlists=[await make('M15 Alpha '+Date.now(),[details[0].songs[2].id,details[0].songs[0].id,details[0].songs[2].id]),await make('M15 Beta '+Date.now(),[details[0].songs[1].id])];
 return {a,b,details,artist,playlists};
}
async function artistSongs(page:Page,key:string) {
 const person=await(await page.request.get('/api/v1/music/artists/'+key)).json(),seen=new Set<number>(),ids:number[]=[];
 for(const album of person.albums){const detail=await(await page.request.get('/api/v1/music/albums/'+album.album_key)).json();for(const song of detail.songs)if(!seen.has(song.id)&&detail.song_credits[String(song.id)]?.some((p:any)=>p.artist_key===key)){seen.add(song.id);ids.push(song.id);}}
 return ids;
}

test('M15 album artist playlist detail Play replaces; next and append preserve queue/current source and whole-batch Undo',async({page})=>{
 const setup=await fixtures(page),proof:any[]=[];
 try{
  const targets=[{kind:'album',key:setup.a.album_key,label:'Play album',ids:setup.details[0].songs.map((s:any)=>s.id)},
   {kind:'artist',key:setup.artist.artist_key,label:'Play artist',ids:await artistSongs(page,setup.artist.artist_key)},
   {kind:'playlist',key:setup.playlists[0].playlist_id,label:'Play playlist',ids:setup.playlists[0].ids}];
  for(const target of targets){
   await page.goto(`/?music=${target.kind}&id=${encodeURIComponent(target.key)}`);
   await playCollection(page,target.label);
   proof.push(await audible(page,target.ids[0],'m15-detail-'+target.kind));
   await visibleQueue(page,target.ids);const before=await audioState(page);
   await enqueue(page,'Play next');
   const next=[target.ids[0],...target.ids,...target.ids.slice(1)];await visibleQueue(page,next);
   const after=await audioState(page);expect(after.src).toBe(before.src);expect(after.paused).toBe(true);expect(after.time).toBeCloseTo(before.time,1);
   await enqueue(page,'Add to queue');await visibleQueue(page,[...next,...target.ids]);
   await page.locator('#toast').getByRole('button',{name:'Undo',exact:true}).click();await visibleQueue(page,next);
   await playCollection(page,target.label);await expectPlaying(page,target.ids[0]);
   expect((await queue(page)).ids).toEqual(target.ids);await pause(page);await page.keyboard.press('Escape');
  }
  e.record({feature:'M15',scenario:'all collection details replacement/enqueue/Undo and original source output',result:'pass',data:{proof}});
 }catch(error){e.record({feature:'M15',scenario:'collection details',result:'fail',detail:String(error),data:{proof}});throw error;}
 finally{for(const p of setup.playlists)await page.request.delete('/api/v1/playlists/'+p.playlist_id);}
});

test('M15 album artist playlist catalog reverse selection follows visible order; keyboard/native controls and themes',async({page})=>{
 const setup=await fixtures(page),artifacts:string[]=[],proof:any[]=[];
 try{
  const people=await(await page.request.get('/api/v1/music/artists')).json();
  const selectedArtists=people.filter((p:any)=>['Aurora Lane','Neon Coast'].includes(p.name));expect(selectedArtists.length).toBe(2);
  for(const target of [{kind:'album',keys:[setup.a.album_key,setup.b.album_key]},
   {kind:'artist',keys:selectedArtists.map((p:any)=>p.artist_key)},
   {kind:'playlist',keys:setup.playlists.map(p=>p.playlist_id)}]){
   await page.goto('/?music='+target.kind+'s');
   await grid(page).getByRole('button',{name:'Select '+target.kind+'s',exact:true}).click();
   const visible=await grid(page).locator('[data-collection-key]').evaluateAll(nodes=>nodes.map(n=>(n as HTMLElement).dataset.collectionKey!));
   const selected=visible.filter(key=>target.keys.includes(key));expect(selected.length).toBe(2);
   for(const key of [...selected].reverse())await grid(page).locator(`[data-collection-key="${key}"]`).getByRole('checkbox').check();
   await expect(grid(page).locator('[data-batch-count]')).toHaveText('2 selected');
   const ids:number[]=[];
   for(const key of selected){if(target.kind==='artist')ids.push(...await artistSongs(page,key));else{const path=target.kind==='album'?'/api/v1/music/albums/':'/api/v1/playlists/';ids.push(...(await(await page.request.get(path+key)).json()).songs.map((s:any)=>s.id));}}
   await playCollection(page,'Play selected');proof.push(await audible(page,ids[0],'m15-selected-'+target.kind));
   await visibleQueue(page,ids);
   await enqueue(page,'Play next');const inserted=[ids[0],...ids,...ids.slice(1)];await visibleQueue(page,inserted);
   await enqueue(page,'Add to queue');await visibleQueue(page,[...inserted,...ids]);
   if(target.kind==='album')for(const [theme,width]of [['dark',1366],['light',1366],['dark',390],['light',390]] as const){
    await page.setViewportSize({width,height:844});if(await page.locator('html').getAttribute('data-theme')!==theme)await page.getByRole('button',{name:'Toggle theme',exact:true}).click();
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
    const shot=artifact(`m15-selection-${theme}-${width}.png`);await page.screenshot({path:shot,fullPage:true});artifacts.push(shot);
   }
   await page.keyboard.press('Escape');await expect(grid(page).getByRole('button',{name:'Select '+target.kind+'s',exact:true})).toBeVisible();
   await expect(grid(page).locator('[data-collection-check]:visible')).toHaveCount(0);
   await page.setViewportSize({width:1366,height:844});
   const first=grid(page).locator('[data-collection-key]').first();await first.focus();await page.keyboard.press('j');await page.keyboard.press('k');await page.keyboard.press('Enter');
   await expect(grid(page).getByRole('button',{name:'▶ Play '+target.kind,exact:true})).toBeVisible();
  }
  // Native typing remains owned by the playlist search input.
  await page.getByRole('searchbox',{name:'Find playlist songs'}).fill('e E j k r /');await expect(page.getByRole('searchbox',{name:'Find playlist songs'})).toHaveValue('e E j k r /');
  const saved=await queue(page);await page.reload();
  await expect.poll(async()=>(await queue(page)).occurrences).toEqual(saved.occurrences);
  expect((await queue(page)).index).toBe(saved.index);expect((await audioState(page)).paused).toBe(true);
  await logout(page);await keyboardLogin(page,f.admin);
  await page.keyboard.press('Q');await expect(page.locator('#queue-panel')).toContainText('Queue is empty');await page.keyboard.press('Escape');
  await logout(page);await keyboardLogin(page,f.user);
  await expect.poll(async()=>(await queue(page)).occurrences).toEqual(saved.occurrences);
  expect((await queue(page)).index).toBe(saved.index);expect((await audioState(page)).paused).toBe(true);
  await page.keyboard.press('Q');await page.getByRole('button',{name:'Resume queue',exact:true}).click();await expectPlaying(page,saved.ids[saved.index]);
  await page.getByRole('button',{name:'Close queue',exact:true}).click();await pause(page);
  proof.push({sameAccountReloadAndAccountRoundtrip:saved});
  e.record({feature:'M15',scenario:'three collection catalogs stable reverse selection all actions keyboard focus narrow themes',result:'pass',data:{proof},artifacts});
 }catch(error){e.record({feature:'M15',scenario:'catalog collection selection',result:'fail',detail:String(error),data:{proof},artifacts});throw error;}
 finally{for(const p of setup.playlists)await page.request.delete('/api/v1/playlists/'+p.playlist_id);}
});

test('M15 pending shortcut single submission and visible offline error preserve queue; late source cannot replace newer playback',async({page,context})=>{
 const setup=await fixtures(page);
 try{
  await page.goto('/?music=album&id='+setup.a.album_key);await playCollection(page,'Play album');await expectPlaying(page,setup.details[0].songs[0].id);await pause(page);await page.keyboard.press('Escape');
  await page.goto('/?music=albums');const card=grid(page).locator(`[data-collection-key="${setup.a.album_key}"]`);await card.focus();
  await ready(page);const before=await queue(page);await context.setOffline(true);await page.keyboard.press('e');
  const status=grid(page).locator('[data-collection-status]');await expect(status).toContainText('Could not load songs');await expect(status).toBeVisible();expect((await queue(page)).ids).toEqual(before.ids);
  await context.setOffline(false);
  const admin=await request.newContext({baseURL:f.origin});
  await admin.post('/api/v1/auth/login',{data:f.admin});
  const user=(await(await admin.get('/api/v1/admin/users')).json()).find((u:any)=>u.username===f.user.username);
  const grant={set_id:f.setIds.Music,user_id:user.id,role:'viewer'};
  try {
   expect((await admin.delete('/api/v1/admin/permissions',{data:grant})).ok()).toBe(true);
   await card.focus();await page.keyboard.press('e');await expect(status).toContainText('Could not load songs');await expect(status).toBeVisible();
   expect((await queue(page)).ids).toEqual(before.ids);
  } finally {await admin.post('/api/v1/admin/permissions',{data:grant});await admin.dispose();}
  // Delay a real fetch in the page; no fake metadata or successful playback response.
  await page.evaluate(()=>{const w=window as any,original=window.fetch.bind(window);w.__m15calls=0;w.__m15release=null;w.__m15delay=true;window.fetch=async(...args)=>{if(w.__m15delay&&String(args[0]).includes('/music/albums/')){w.__m15delay=false;w.__m15calls++;await new Promise<void>(resolve=>w.__m15release=resolve);}return original(...args);};});
  await card.focus();await page.keyboard.press('e');await expect(status).toHaveText('Loading songs…');await expect(status).toBeVisible();await page.keyboard.press('e');
  expect(await page.evaluate(()=>(window as any).__m15calls)).toBe(1);
  await page.evaluate(()=>(window as any).__m15release());await expect(status).toContainText('Added');expect((await queue(page)).ids.length).toBe(before.ids.length+setup.details[0].songs.length);
  // A second pending action is superseded by a real saved playlist source.
  await page.evaluate(()=>(window as any).__m15delay=true);
  await card.focus();await page.keyboard.press('E');await expect(status).toHaveText('Loading songs…');
  await page.getByRole('button',{name:'Open album Night Transit by Aurora Lane',exact:true}).click();
  await page.locator('.media-row').nth(1).locator('.song-open').click();await expectPlaying(page,setup.details[0].songs[1].id);
  const newer=await queue(page);await page.evaluate(()=>(window as any).__m15release());await page.waitForTimeout(200);expect((await queue(page)).ids).toEqual(newer.ids);
  e.record({feature:'M15',scenario:'visible shortcut offline/loading single submission and late source ownership',result:'pass',data:{before,newer}});
 }catch(error){e.record({feature:'M15',scenario:'collection boundaries',result:'fail',detail:String(error)});throw error;}
 finally{await context.setOffline(false);for(const p of setup.playlists)await page.request.delete('/api/v1/playlists/'+p.playlist_id);}
});
