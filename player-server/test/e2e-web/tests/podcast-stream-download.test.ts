import {test,expect} from '@playwright/test';
import {createServer,type ServerResponse} from 'node:http';
import {readFileSync,existsSync,readdirSync} from 'node:fs';
import {join} from 'node:path';
import {keyboardLogin} from './helpers/music-ui';
const fixture=process.env.PLAYER_PODCAST_STREAM_FIXTURE;
test.skip(!fixture,'Requires marked disposable music_fixture.py server');
const auth=fixture?JSON.parse(readFileSync(`${fixture}/auth.json`,'utf8')):{};
const state=fixture?JSON.parse(readFileSync(`${fixture}/state.json`,'utf8')):{};

function wav(){
 const n=16000*45,b=Buffer.alloc(44+n*2);
 b.write('RIFF',0);b.writeUInt32LE(b.length-8,4);b.write('WAVEfmt ',8);b.writeUInt32LE(16,16);b.writeUInt16LE(1,20);b.writeUInt16LE(1,22);b.writeUInt32LE(16000,24);b.writeUInt32LE(32000,28);b.writeUInt16LE(2,32);b.writeUInt16LE(16,34);b.write('data',36);b.writeUInt32LE(n*2,40);
 for(let i=0;i<n;i++)b.writeInt16LE(Math.round(Math.sin(i/16000*Math.PI*880)*2000),44+i*2);
 return b;
}

for (const view of ['subscription', 'set folder']) {
test(`${view}: stream during save, cancel cleans partial bytes and keeps playback, rerendered retry completes`,async({page},testInfo)=>{
 expect(existsSync(`${fixture}/.player-music-fixture`)).toBe(true);
 const token=`stream-${view.replace(' ', '-')}-${testInfo.project.name}-${Date.now()}`;
 const downloadLabel=view==='subscription'?'Download episode to server':'Download episode';
 const title=`Podcast ${token}`,episodeTitle=`Episode ${token}`,audio=wav();
 let origin='',downloads=0,cancelled=0,streams=0;
 const pending=new Set<ServerResponse>();
 const publisher=createServer((req,res)=>{
  if(req.url==='/rss'){
   res.writeHead(200,{'Content-Type':'application/rss+xml'});
   res.end(`<rss version="2.0"><channel><title>${title}</title><link>${origin}/</link><description>Streaming fixture</description><item><guid>${token}</guid><title>${episodeTitle}</title><enclosure url="${origin}/audio.wav" type="audio/wav" length="${audio.length}"/><duration>45</duration></item></channel></rss>`);return;
  }
  if(req.url!=='/audio.wav'){res.writeHead(404);res.end();return;}
  expect(req.headers.cookie).toBeUndefined();expect(req.headers.authorization).toBeUndefined();
  const range=req.headers.range;
  if(range){
   streams++;
   const match=/^bytes=(\d+)-(\d*)$/.exec(range);
   const start=Number(match?.[1]||0),end=Math.min(Number(match?.[2]||audio.length-1),audio.length-1);
   res.writeHead(206,{'Content-Type':'audio/wav','Accept-Ranges':'bytes','Content-Range':`bytes ${start}-${end}/${audio.length}`,'Content-Length':end-start+1});
   res.end(req.method==='HEAD'?undefined:audio.subarray(start,end+1));return;
  }
  downloads++;
  res.writeHead(200,{'Content-Type':'audio/wav','Content-Length':audio.length,'Accept-Ranges':'bytes'});
  res.write(audio.subarray(0,4096));pending.add(res);
  res.on('close',()=>{if(!res.writableFinished)cancelled++;pending.delete(res);});
 });
 await new Promise<void>(resolve=>publisher.listen(0,'127.0.0.1',resolve));
 origin=`http://127.0.0.1:${(publisher.address() as any).port}`;
 try{
  await keyboardLogin(page,auth.admin);
  const subscription=await page.request.post('/api/podcasts',{data:{feed_url:`${origin}/rss`,set_name:title}});expect(subscription.ok(),await subscription.text()).toBe(true);
  const feed=await subscription.json();
  const catalog=await(await page.request.get(`/api/podcasts/episodes?feed_key=${encodeURIComponent(feed.feed_key)}&filter=all`)).json();
  const episode=catalog.find((e:any)=>e.title===episodeTitle);expect(episode).toBeTruthy();expect(episode.server_copy).toBeNull();
  if(view==='subscription') {
   const all=page.getByRole('button',{name:'All podcast episodes',exact:true});if(!await all.isVisible())await page.getByRole('button',{name:'Toggle sets sidebar',exact:true}).click();await all.click();
  } else {
   await page.reload();
   await page.locator(`.set-card[data-set-id="${feed.set_id}"]`).click();
   await page.getByRole('link',{name:'Downloaded server files',exact:true}).click();
   await page.locator('.folder-card').filter({hasText:title}).click();
  }
  const rerender=async()=>{
   if(view==='subscription') {
    const list=page.getByRole('button',{name:'List',exact:true});
    const listActive=await list.getAttribute('aria-pressed');
    await page.getByRole('button',{name:listActive==='true'?'Grid':'List',exact:true}).click();
   } else await page.locator('#breadcrumb-bar .breadcrumb-part').last().click();
  };
  let card=page.locator('.episode-card').filter({hasText:episodeTitle});await expect(card).toBeVisible();
  await card.getByRole('button',{name:downloadLabel,exact:true}).click();
  await expect.poll(()=>downloads).toBe(1);await expect(card.getByRole('button',{name:'Cancel download',exact:true})).toBeVisible();
  await card.getByRole('button',{name:'Play episode',exact:true}).click();
  const audioElement=page.locator('#media-audio');
  await expect.poll(()=>audioElement.evaluate((a:HTMLAudioElement)=>!a.paused&&a.currentTime>.2)).toBe(true);
  expect(streams).toBeGreaterThan(0);expect(pending.size).toBe(1);
  const source=await audioElement.getAttribute('src');expect(source).toContain(`/podcasts/episodes/${episode.id}/stream`);
  await page.getByRole('button',{name:'More player controls',exact:true}).click();await page.locator('#btn-minimize').click();
  // Rerender/list-grid navigation must retain cancellation of this transfer.
  await rerender();card=page.locator('.episode-card').filter({hasText:episodeTitle});
  await page.setViewportSize({width:390,height:844});
  if (await page.locator('#sidebar.open').isVisible()) await page.getByRole('button',{name:'Toggle sets sidebar',exact:true}).click();
  const cancel=card.getByRole('button',{name:'Cancel download',exact:true});await expect(cancel).toBeVisible();expect((await cancel.boundingBox())!.height).toBeGreaterThanOrEqual(44);
  await cancel.click();await expect.poll(()=>cancelled).toBe(1);await expect(cancel).toHaveCount(0);
  const files=()=>readdirSync(state.media_root,{recursive:true}).map(String).filter(name=>name.includes(episodeTitle)&&/\.wav$/.test(name));
  await expect.poll(files).toEqual([]);
  const afterCancel=await audioElement.evaluate((a:HTMLAudioElement)=>a.currentTime);
  await expect.poll(()=>audioElement.evaluate((a:HTMLAudioElement)=>({playing:!a.paused,time:a.currentTime}))).toMatchObject({playing:true});
  await expect.poll(()=>audioElement.evaluate((a:HTMLAudioElement)=>a.currentTime)).toBeGreaterThan(afterCancel+.25);
  const unsaved=(await(await page.request.get(`/api/podcasts/episodes?feed_key=${encodeURIComponent(feed.feed_key)}&filter=all`)).json())[0];expect(unsaved.server_copy).toBeNull();
  await card.getByRole('button',{name:downloadLabel,exact:true}).click();await expect.poll(()=>downloads).toBe(2);
  await rerender();
  card=page.locator('.episode-card').filter({hasText:episodeTitle});
  await expect(card.getByRole('button',{name:'Cancel download',exact:true})).toBeVisible();
  for(const res of pending)res.end(audio.subarray(4096));
  if(view==='subscription') {
   await expect(card.getByText(/Downloaded on server/)).toBeVisible();
   await expect(card.getByRole('button',{name:downloadLabel,exact:true})).toHaveCount(0);
  } else {
   await expect(card).toHaveCount(0);
   await expect(page.locator('.media-card[data-id]').filter({hasText:episodeTitle})).toBeVisible();
  }
  await expect.poll(()=>files().length).toBe(1);
  expect(await audioElement.getAttribute('src')).toBe(source);
  expect(await audioElement.evaluate((a:HTMLAudioElement)=>!a.paused)).toBe(true);
  await page.setViewportSize({width:844,height:390});
  await page.addStyleTag({content:'html{font-size:160%}'});
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
 }finally{
  for(const response of pending)response.destroy();
  publisher.closeAllConnections();await new Promise<void>(resolve=>publisher.close(()=>resolve()));
 }
});

}
