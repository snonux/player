import { expect, type Page } from '@playwright/test';

// System Firefox BiDi resize may finish before foreground painting resumes.
export async function settleMusicViewport(page:Page,width:number,diagnostic?:(reason:string)=>Promise<void>) {
 await page.bringToFront();
 await page.setViewportSize({width,height:860});
 await page.bringToFront();
 await expect.poll(()=>page.evaluate(()=>[innerWidth,innerHeight])).toEqual([width,860]);
 await expect.poll(()=>page.evaluate(()=>document.visibilityState)).toBe('visible');
 const frames=await page.evaluate(()=>new Promise<number>(resolve=>{
  let count=0;
  const timer=setTimeout(()=>resolve(count),4000);
  requestAnimationFrame(()=>{count++;requestAnimationFrame(()=>{count++;clearTimeout(timer);resolve(count);});});
 }));
 if(frames!==2) await diagnostic?.(`Only ${frames}/2 foreground frames after viewport resize`);
 expect(frames,'actual foreground painting frames after viewport resize').toBe(2);
}
