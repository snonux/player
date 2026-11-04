import { test, expect } from '@playwright/test';
test('podcast player displays safe notes and clears them on track change', async ({page}) => {
  const feed='https://notes.example/feed.xml', audio='https://notes.example/episode.wav';
  const wav=Buffer.alloc(44+8000*10*2);
  wav.write('RIFF'); wav.writeUInt32LE(wav.length-8,4); wav.write('WAVEfmt ',8); wav.writeUInt32LE(16,16);
  wav.writeUInt16LE(1,20); wav.writeUInt16LE(1,22); wav.writeUInt32LE(8000,24); wav.writeUInt32LE(16000,28);
  wav.writeUInt16LE(2,32); wav.writeUInt16LE(16,34); wav.write('data',36); wav.writeUInt32LE(wav.length-44,40);
  await page.route(audio,r=>r.fulfill({contentType:'audio/wav',headers:{'Access-Control-Allow-Origin':'*'},body:wav}));
  await page.route(feed,r=>r.fulfill({contentType:'application/rss+xml',headers:{'Access-Control-Allow-Origin':'*'},body:
    `<rss><channel><title>Notes podcast</title><item><guid>one</guid><title>Notes episode</title><description><![CDATA[<p>Episode summary.</p><script>window.notesUnsafe=true</script><p><a href="https://publisher.example/notes">Publisher link</a></p><p><a href="http://publisher.example/http-notes">HTTP link</a></p>${'<p>Long episode notes.</p>'.repeat(40)}]]></description><enclosure url="${audio}"/></item></channel></rss>`}));
  await page.goto('/index.html?local=podcasts');
  page.once('dialog',d=>d.accept(feed));
  await page.getByRole('button',{name:'Add feed URL',exact:true}).click();
  await page.getByRole('button',{name:'Notes podcast',exact:true}).click();
  await page.getByRole('button',{name:'Notes episode',exact:true}).click();
  const notes=page.getByRole('region',{name:'Episode show notes'});
  await expect(notes).toContainText('Episode summary.');
  await expect(notes.locator('details')).toHaveAttribute('open','');
  await expect(notes.getByRole('link',{name:'https://publisher.example/notes'})).toHaveAttribute('href','https://publisher.example/notes');
  for (const url of ['https://publisher.example/notes', 'http://publisher.example/http-notes']) {
    await page.context().route(url, route => route.fulfill({contentType:'text/html',body:'Publisher page'}));
    const popupPromise = page.waitForEvent('popup');
    await notes.getByRole('link',{name:url,exact:true}).click();
    const popup = await popupPromise;
    await expect(popup).toHaveURL(url);
    await popup.close();
  }
  expect(await page.evaluate(()=>(window as any).notesUnsafe)).toBeUndefined();
  await expect(notes.locator('script')).toHaveCount(0);
  for(const viewport of [{width:390,height:844},{width:844,height:390}]) {
    await page.setViewportSize(viewport);
    await expect(page.locator('#btn-play')).toBeVisible();
    const button=await page.locator('#btn-play').boundingBox();
    expect(button!.y+button!.height).toBeLessThanOrEqual(viewport.height);
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBeTruthy();
  }
  await notes.locator('summary').focus(); await page.keyboard.press('Enter');
  await expect(notes.locator('details')).not.toHaveAttribute('open','');
  await page.evaluate(async(url)=>{
    const player=await import('/js/player.js');
    player.loadMediaDirect({id:'ordinary',type:'audio',title:'Ordinary audio',file_name:'ordinary.wav'},url,'');
  },audio);
  await expect(notes).toHaveCount(0);
});
