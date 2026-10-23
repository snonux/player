import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { libraryPresenceSnapshot } from '../playback-presence.js';
const context = {media:{id:3,type:'audio'},source:{library:true,userId:1},account:1,transport:{readyState:4,paused:false,currentTime:33},reportProgress:true};
assert.deepEqual(libraryPresenceSnapshot(context),{media_id:3,state:'playing',position_seconds:33});
assert.equal(libraryPresenceSnapshot({...context,transport:{...context.transport,paused:true}}).state,'paused');
// The transport interface also belongs to decoded PCM; no native DOM/src fallback.
assert.equal(libraryPresenceSnapshot({...context,transport:{...context.transport,currentTime:62,currentSrc:'decoded'}}).position_seconds,62);
for (const override of [{account:2},{pending:true},{remote:true},{reportProgress:false},{source:{library:false,userId:1}},{source:{library:true}},{media:{id:3,type:'audio',live_radio:true}},{media:{id:3,type:'video'}},{media:{id:3,type:'audio',offlinePinURL:'blob:download'}},{transport:{...context.transport,error:{}}},{transport:{...context.transport,ended:true}},{transport:{...context.transport,readyState:0}},{transport:{...context.transport,currentTime:NaN}}]) assert.equal(libraryPresenceSnapshot({...context,...override}),null,JSON.stringify(override));
const source=readFileSync(new URL('../playback.js',import.meta.url),'utf8');
assert.match(source,/library: true, userId: state\.user\?\.id/,'load captures account synchronously before media preparation');
assert.match(source,/library: true, userId: currentSource\?\.userId/,'decoded successor retains prepared source owner instead of late current account');
console.log('playback presence ownership PASS');
