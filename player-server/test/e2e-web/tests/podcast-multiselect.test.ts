import { test, expect } from '@playwright/test';

// Stock Firefox's BiDi mouse adapter sends every down/up as a separate action
// command and ignores clickCount. Keep both clicks in one browser-owned input
// action sequence so this regression observes a genuine trusted dblclick.
async function genuineDoubleClick(page: any, locator: any) {
  if (page.context().browser().browserType().name() !== 'firefox') return locator.dblclick();
  await locator.dblclick({trial:true});
  const delegate=page._connection?.toImpl?.(page)?.delegate;
  const session=delegate?._session;
  if (delegate?.constructor?.name!=='BidiPage' || !session?.send || !session.sessionId) throw new Error('Owned Firefox BiDi mouse transport unavailable');
  const box=await locator.boundingBox();
  if (!box) throw new Error('Double-click target is not visible');
  const context=session.sessionId;
  try {
    await session.send('input.performActions',{context,actions:[{type:'pointer',id:'player_episode_double_mouse',parameters:{pointerType:'mouse'},actions:[
      {type:'pointerMove',origin:'viewport',x:Math.round(box.x+box.width/2),y:Math.round(box.y+box.height/2),duration:0},
      {type:'pointerDown',button:0},{type:'pause',duration:60},{type:'pointerUp',button:0},
      {type:'pause',duration:100},{type:'pointerDown',button:0},{type:'pause',duration:60},{type:'pointerUp',button:0},
    ]}]});
  } finally { await session.send('input.releaseActions',{context}); }
}

for (const layout of ['List', 'Grid']) {
  test(`account podcast bulk state, failure/retry and delete in ${layout}`, async ({ page }) => {
    await page.goto('/login.html');
    await page.evaluate(async () => {
      document.body.innerHTML = '<main class="main-content"><div id="media-grid" class="media-grid"></div></main>';
      for (const name of ['theme', 'components', 'layout']) {
        const css = document.createElement('link'); css.rel = 'stylesheet'; css.href = `/css/${name}.css`; document.head.append(css);
        await new Promise(resolve => { css.onload = resolve; css.onerror = resolve; });
      }
      const { API } = await import('/js/api.js');
      const { state } = await import('/js/state.js');
      const episodeState = await import('/js/podcast-episode-state.js');
      episodeState.setPodcastAccount(42);
      state.virtualSet = 'podcasts'; state.podcastFeedKey = ''; state.podcastOverview = false; state.podcastFilter = 'uncompleted';
      const episodes = [0, 1, 2].map(i => ({id: i + 1, feed_key: 'feed:batch', episode_key: `guid:${i}`, title: `Batch episode ${i}`, completed: false, deleted: false}));
      (window as any).failSecond = true;
      (window as any).batchEpisodes = episodes;
      API.podcasts = async () => [];
      API.subscribedEpisodes = async ({filter}: any) => episodes.filter(ep => !ep.deleted && (filter === 'all' || ep.completed === (filter === 'completed'))).map(ep => ({...ep}));
      API.syncConfig = async () => ({user_id: 42, podcast_episode_deletion: true});
      API.syncPodcast = async ({episodes: edits = []}: any) => {
        if ((window as any).failSecond && edits.some((edit: any) => edit.episode_key === 'guid:1')) throw new Error('Temporary failure');
        for (const edit of edits) {
          const ep = episodes.find(ep => ep.episode_key === edit.episode_key)!;
          if (edit.completion) ep.completed = edit.completion.completed;
          if (edit.deletion) ep.deleted = edit.deletion.deleted;
        }
        return {episodes: edits, acked: edits.flatMap((edit: any) => ['completion', 'deletion'].map(key => edit[key]?.edit_id).filter(Boolean))};
      };
      const views = await import('/js/podcast-views.js');
      const render = () => views.renderPodcastVirtualView(document.getElementById('media-grid'));
      views.initPodcastViews({onLoadMedia: render});
      await render();
    });
    const toolbar = page.locator('.podcast-selection');
    await page.getByRole('group', {name:'Episode layout'}).getByRole('button',{name:layout,exact:true}).click();
    await toolbar.getByRole('button',{name:'Select episodes',exact:true}).click();
    await toolbar.getByRole('button',{name:'Select all visible',exact:true}).click();
    await expect(toolbar).toContainText('3 selected');
    await toolbar.getByRole('button',{name:'Apply',exact:true}).click();
    await expect(toolbar).toContainText('1 selected');
    await expect(page.getByRole('checkbox',{name:'Select Batch episode 1',exact:true})).toBeChecked();
    await expect(page.getByRole('checkbox',{name:'Select Batch episode 0',exact:true})).not.toBeChecked();
    expect(await page.evaluate(() => (window as any).batchEpisodes.map((ep:any)=>ep.completed))).toEqual([true,false,true]);
    await page.evaluate(() => { (window as any).failSecond = false; });
    await toolbar.getByRole('button',{name:'Apply',exact:true}).click();
    await expect(page.locator('.episode-card')).toHaveCount(0);
    await page.getByRole('group',{name:'Completion filter'}).getByRole('button',{name:'Completed',exact:true}).click();
    await expect(page.locator('.episode-card')).toHaveCount(3);
    await toolbar.getByRole('button',{name:'Select episodes',exact:true}).click();
    await toolbar.getByRole('button',{name:'Select all visible',exact:true}).click();
    await toolbar.getByRole('combobox').selectOption('uncompleted');
    await toolbar.getByRole('button',{name:'Apply',exact:true}).click();
    await expect(page.locator('.episode-card')).toHaveCount(0);
    await page.getByRole('group',{name:'Completion filter'}).getByRole('button',{name:'All',exact:true}).click();
    await expect(page.locator('.episode-card')).toHaveCount(3);
    await toolbar.getByRole('button',{name:'Select episodes',exact:true}).click();
    // Checkboxes retain native activation and their accessible name.
    await page.getByRole('checkbox',{name:'Select Batch episode 0',exact:true}).focus();
    await page.keyboard.press('Space');
    await expect(page.getByRole('checkbox',{name:'Select Batch episode 0',exact:true})).toBeChecked();
    await expect(toolbar).toContainText('1 selected');
    await toolbar.getByRole('combobox').selectOption('delete');
    page.once('dialog', dialog => dialog.dismiss());
    await toolbar.getByRole('button',{name:'Apply',exact:true}).click();
    expect(await page.evaluate(() => (window as any).batchEpisodes.some((ep:any)=>ep.deleted))).toBe(false);
    page.once('dialog', dialog => dialog.accept());
    await toolbar.getByRole('button',{name:'Apply',exact:true}).click();
    await expect(page.locator('.episode-card')).toHaveCount(2);
    expect(await page.evaluate(() => (window as any).batchEpisodes[0].deleted)).toBe(true);
    await page.setViewportSize({width:390,height:844});
    await toolbar.getByRole('button',{name:'Select episodes',exact:true}).click();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.getByRole('checkbox').first().focus();
    await page.keyboard.press('Escape');
    await expect(toolbar.getByRole('button',{name:'Select episodes',exact:true})).toBeVisible();
  });
}

test('device podcast batch writes survive offline list/grid and deletion', async ({page}) => {
  const feed = 'https://batch.example/feed';
  await page.route(feed, route => route.fulfill({contentType:'application/rss+xml',headers:{'Access-Control-Allow-Origin':'*'},body:
    `<rss><channel><title>Batch podcast</title>${[0,1,2].map(i=>`<item><guid>${i}</guid><title>Device batch ${i}</title><enclosure url="https://batch.example/${i}.mp3"/></item>`).join('')}</channel></rss>`}));
  await page.goto('/index.html?local=podcasts');
  page.once('dialog', dialog => dialog.accept(feed));
  await page.getByRole('button',{name:'Add feed URL',exact:true}).click();
  await page.getByRole('button',{name:'Batch podcast',exact:true}).click();
  const toolbar = page.locator('.podcast-selection');
  await toolbar.getByRole('button',{name:'Select episodes',exact:true}).click();
  await toolbar.getByRole('button',{name:'Select all visible',exact:true}).click();
  await page.getByRole('group',{name:'Episode layout'}).getByRole('button',{name:'Grid',exact:true}).click();
  await expect(toolbar).toContainText('3 selected');
  await page.context().setOffline(true);
  await toolbar.getByRole('button',{name:'Apply',exact:true}).click();
  await expect(page.locator('.local-files-row')).toHaveCount(0);
  await page.getByRole('group',{name:'Completion filter'}).getByRole('button',{name:'Completed',exact:true}).click();
  await expect(page.locator('.local-files-row')).toHaveCount(3);
  await expect(page.locator('.episode-listened')).toHaveCount(3);
  await toolbar.getByRole('button',{name:'Select episodes',exact:true}).click();
  await toolbar.getByRole('button',{name:'Select all visible',exact:true}).click();
  await toolbar.getByRole('combobox').selectOption('delete');
  page.once('dialog', dialog => dialog.accept());
  await toolbar.getByRole('button',{name:'Apply',exact:true}).click();
  await expect(page.locator('.local-files-row')).toHaveCount(0);
  const rows = await page.evaluate(()=>Object.keys(localStorage).filter(key=>key.startsWith('player-local-podcast-state:')).map(key=>JSON.parse(localStorage.getItem(key)!)));
  expect(rows).toHaveLength(3);
  expect(rows.every(row=>row.completion.completed && row.deletion.deleted)).toBe(true);
});

// Catalog metadata is fixed; all account registers below go through the real
// disposable server's sync handlers and SQLite. No live deployment is allowed.
test('real disposable server acknowledges batch completion, retry and deletion', async ({page}) => {
  const directory = process.env.PLAYER_BATCH_LIVE_FIXTURE;
  test.skip(!directory, 'Set PLAYER_BATCH_LIVE_FIXTURE to a marked disposable music fixture');
  const {readFileSync, existsSync} = await import('node:fs');
  const {resolve} = await import('node:path');
  expect(existsSync(`${directory}/.player-music-fixture`)).toBe(true);
  const fixture = JSON.parse(readFileSync(`${directory}/state.json`,'utf8'));
  const auth = JSON.parse(readFileSync(`${directory}/auth.json`,'utf8'));
  const origin = fixture.origin;
  expect(new URL(origin).hostname).toBe('127.0.0.1');
  await page.route(`${origin}/js/**`, route => route.fulfill({path:resolve(__dirname,'../../../web',new URL(route.request().url()).pathname.slice(1)),contentType:'application/javascript'}));
  await page.goto(`${origin}/login.html`);
  const login = await page.request.post(`${origin}/api/login`,{data:auth.admin});
  expect(login.ok()).toBe(true);
  await page.evaluate(async () => {
    document.body.innerHTML = '<main><div id="media-grid" class="media-grid"></div></main>';
    const {API} = await import('/js/api.js');
    const request = window.fetch.bind(window);
    let injectFailure = true;
    window.fetch = async (input, options) => {
      const body = JSON.parse(String(options?.body || '{}'));
      if (injectFailure && String(input).endsWith('/api/podcasts/sync') && body.episodes?.some((ep:any)=>ep.episode_key==='guid:1')) {
        injectFailure = false;
        return new Response(JSON.stringify({error:'Controlled temporary sync failure'}), {status:503,headers:{'Content-Type':'application/json'}});
      }
      return request(input,options);
    };
    const config = await API.syncConfig();
    const {setPodcastAccount} = await import('/js/podcast-episode-state.js'); setPodcastAccount(config.user_id);
    const {state} = await import('/js/state.js');
    state.virtualSet='podcasts'; state.podcastFeedKey=''; state.podcastFilter='all'; state.podcastOverview=false;
    const feed = `feed:batch.example/${Date.now()}`;
    (window as any).realBatchFeed=feed;
    const sync = API.syncPodcast;
    (window as any).readRealBatch = async () => Promise.all([0,1].map(async i => {
      const scope={feed_key:feed,episode_key:`guid:${i}`};
      const res=await sync({scope});
      return res.episodes?.find((ep:any)=>ep.episode_key===scope.episode_key) || {};
    }));
    API.subscribedEpisodes = async () => (await (window as any).readRealBatch()).map((row:any,i:number)=>({
      id:i+1,feed_key:feed,episode_key:`guid:${i}`,title:`Real batch ${i}`,completed:row.completion?.completed || false,
      deleted:row.deletion?.deleted || false,
    })).filter((ep:any)=>!ep.deleted);
    const views=await import('/js/podcast-views.js');
    const render=()=>views.renderPodcastVirtualView(document.getElementById('media-grid'));
    views.initPodcastViews({onLoadMedia:render}); await render();
  });
  const toolbar=page.locator('.podcast-selection');
  await toolbar.getByRole('button',{name:'Select episodes',exact:true}).click();
  await toolbar.getByRole('button',{name:'Select all visible',exact:true}).click();
  await toolbar.getByRole('button',{name:'Apply',exact:true}).click();
  await expect(toolbar).toContainText('1 selected');
  let real=await page.evaluate(()=>(window as any).readRealBatch());
  expect(real.map((ep:any)=>ep.completion?.completed || false)).toEqual([true,false]);
  await toolbar.getByRole('button',{name:'Apply',exact:true}).click();
  await expect(toolbar.getByRole('button',{name:'Select episodes',exact:true})).toBeVisible();
  real=await page.evaluate(()=>(window as any).readRealBatch());
  expect(real.every((ep:any)=>ep.completion.completed)).toBe(true);
  await toolbar.getByRole('button',{name:'Select episodes',exact:true}).click();
  await toolbar.getByRole('button',{name:'Select all visible',exact:true}).click();
  await toolbar.getByRole('combobox').selectOption('delete');
  page.once('dialog',dialog=>dialog.accept());
  await toolbar.getByRole('button',{name:'Apply',exact:true}).click();
  await expect(page.locator('.episode-card')).toHaveCount(0);
  real=await page.evaluate(()=>(window as any).readRealBatch());
  expect(real.every((ep:any)=>ep.completion.completed && ep.deletion.deleted)).toBe(true);
});

test('selected layout keeps long titles, enlarged text and narrow controls reachable', async ({page}, testInfo) => {
  await page.goto('/login.html');
  await page.evaluate(async () => {
    document.body.innerHTML='<main><div id="media-grid" class="media-grid"></div></main>';
    document.querySelector('link[href="/css/login.css"]')?.remove();
    for (const name of ['theme','components','layout']) {
      const css=document.createElement('link');css.rel='stylesheet';css.href=`/css/${name}.css`;document.head.append(css);
      await new Promise(resolve=>{css.onload=resolve;css.onerror=resolve;});
    }
    const {API}=await import('/js/api.js');
    const {state}=await import('/js/state.js');
    state.virtualSet='podcasts';state.podcastFeedKey='';state.podcastFilter='all';state.podcastOverview=false;
    API.subscribedEpisodes=async()=>[0,1].map(i=>({id:i+1,feed_key:'feed:layout.example/rss',episode_key:`guid:${i}`,
      title:`Long episode ${i} ${'with readable words '.repeat(12)}`,completed:false}));
    const {renderPodcastVirtualView}=await import('/js/podcast-views.js');
    await renderPodcastVirtualView(document.getElementById('media-grid'));
  });
  const toolbar=page.locator('.podcast-selection');
  for (const viewport of [{width:390,height:844},{width:844,height:390}]) {
    await page.setViewportSize(viewport);
    for (const scale of [1.6,2]) {
      await page.addStyleTag({content:`html {font-size:${scale*100}%;}`});
      for (const layout of ['List','Grid']) {
        await page.getByRole('group',{name:'Episode layout'}).getByRole('button',{name:layout,exact:true}).click();
        await toolbar.getByRole('button',{name:'Select episodes',exact:true}).click();
        const check=page.getByRole('checkbox').first();
        await check.scrollIntoViewIfNeeded();await check.check();
        await expect(check).toBeChecked();await expect(toolbar).toContainText('1 selected');
        await page.locator('.episode-card').first().hover();
        await expect(page.locator('.episode-card .card-actions').first()).toBeHidden();
        const box=(await check.boundingBox())!;expect(box.width).toBeGreaterThanOrEqual(44);expect(box.height).toBeGreaterThanOrEqual(44);
        await toolbar.getByRole('combobox').selectOption('delete');
        await toolbar.getByRole('button',{name:'Apply',exact:true}).scrollIntoViewIfNeeded();
        await expect(toolbar.getByRole('button',{name:'Apply',exact:true})).toBeVisible();
        for (const button of await toolbar.locator('button:visible').all()) expect((await button.boundingBox())!.height).toBeGreaterThanOrEqual(44);
        expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
        if (testInfo.project.name==='chrome' && viewport.width===390 && scale===1.6 && layout==='Grid') {
          await page.evaluate(()=>scrollTo(0,0));
          await page.screenshot({path:'/tmp/player-e34-selected-mode.png',fullPage:true});
        }
        if (layout==='Grid') {await check.focus();await page.keyboard.press('Escape');}
        else {await toolbar.getByRole('button',{name:'Cancel selection',exact:true}).click();}
        await expect(toolbar.getByRole('button',{name:'Select episodes',exact:true})).toBeVisible();
        await expect(page.getByRole('checkbox')).toHaveCount(0);
      }
    }
  }
});

test('production grid selection blocks double-click playback and releases old navigation handlers', async ({page}) => {
  await page.goto('/login.html');
  await page.evaluate(async () => {
    document.body.innerHTML='<main><span id="result-count"></span><div id="empty-hint"></div><div id="breadcrumb-bar"></div><div id="media-grid" class="media-grid"></div></main>';
    document.querySelector('link[href="/css/login.css"]')?.remove();
    for (const name of ['theme','components','layout']) {
      const css=document.createElement('link');css.rel='stylesheet';css.href=`/css/${name}.css`;document.head.append(css);
      await new Promise(resolve=>{css.onload=resolve;css.onerror=resolve;});
    }
    const {API}=await import('/js/api.js');
    const {state}=await import('/js/state.js');
    const episodeState=await import('/js/podcast-episode-state.js');episodeState.setPodcastAccount(42);
    state.virtualSet='podcasts';state.podcastFeedKey='';state.podcastFilter='all';state.podcastOverview=false;
    API.podcasts=async()=>[];
    API.syncPodcast=async()=>({subscriptions:[],episodes:[],epoch:'navigation-fixture',next_since:0,has_more:false,acked:[]});
    API.subscribedEpisodes=async()=>[0,1].map(i=>({id:i+1,feed_key:'feed:navigation.example/rss',episode_key:`guid:${i}`,
      title:`Production episode ${i}`,completed:false,server_copy:{media_id:100+i}}));
    const {initMediaGrid,loadMedia}=await import('/js/views/media-grid.js');
    const {initKeyboard}=await import('/js/keyboard.js');
    const {initSelection}=await import('/js/selection.js');
    const views=await import('/js/podcast-views.js');
    (window as any).selectionPlays=0;(window as any).selectionEscapes=0;(window as any).gridDoubleClicks=0;
    document.getElementById('media-grid').addEventListener('dblclick',event=>{if (!event.isTrusted) throw new Error('Double click must be browser input');(window as any).gridDoubleClicks++;},true);
    initMediaGrid({playSelected:()=>{(window as any).selectionPlays++;}});
    initKeyboard({escape:()=>{(window as any).selectionEscapes++;}});
    initSelection();
    views.initPodcastViews({onLoadMedia:loadMedia,playMediaById:async()=>{(window as any).selectionPlays++;return true;}});
    await loadMedia();
  });
  const toolbar=page.locator('.podcast-selection');
  await toolbar.getByRole('button',{name:'Select episodes',exact:true}).click();
  await page.getByRole('checkbox').first().check();
  await genuineDoubleClick(page,page.locator('.episode-card').first().locator('.title'));
  await expect.poll(()=>page.evaluate(()=>(window as any).gridDoubleClicks)).toBe(1);
  expect(await page.evaluate(()=>(window as any).selectionPlays)).toBe(0);
  await expect(toolbar).toContainText('1 selected');
  for (let navigation=0;navigation<3;navigation++) {
    await page.getByRole('group',{name:'Podcast view',exact:true}).getByRole('button',{name:'Podcasts',exact:true}).click();
    await expect(page.getByRole('heading',{name:'Podcasts',exact:true})).toBeVisible();
    await expect(toolbar).toHaveCount(0);
    await page.getByRole('group',{name:'Podcast view',exact:true}).getByRole('button',{name:'Episodes',exact:true}).click();
    await expect(page.locator('.episode-card')).toHaveCount(2);
    await toolbar.getByRole('button',{name:'Select episodes',exact:true}).click();
    const check=page.getByRole('checkbox').first();await check.check();await check.focus();
    await page.keyboard.press('Escape');
    await expect(toolbar.getByRole('button',{name:'Select episodes',exact:true})).toBeVisible();
    await expect(page.getByRole('checkbox')).toHaveCount(0);
    expect(await page.evaluate(()=>(window as any).selectionEscapes)).toBe(0);
    if (navigation<2) {await toolbar.getByRole('button',{name:'Select episodes',exact:true}).click();await page.getByRole('checkbox').first().check();}
  }
  // Playback becomes available again after leaving selection.
  await genuineDoubleClick(page,page.locator('.episode-card').first().locator('.title'));
  await expect.poll(()=>page.evaluate(()=>(window as any).gridDoubleClicks)).toBe(2);
  await expect.poll(()=>page.evaluate(()=>(window as any).selectionPlays)).toBe(1);
});
