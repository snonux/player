import assert from 'node:assert/strict';

// A partial browser test environment must not open Node's live channel and
// keep its process alive. Real browser account broadcasts must still work.
const channels=[];
globalThis.BroadcastChannel=class { constructor(){throw Error('Unexpected host channel');} };
globalThis.window={};
const withoutChannel=await import('../podcast-episode-state.js?without-window-channel');
withoutChannel.setPodcastAccount(1);
assert.equal(withoutChannel.podcastSyncStatus().account,1);

window.BroadcastChannel=class {
 constructor(name){this.name=name;this.messages=[];channels.push(this);}
 postMessage(value){this.messages.push(value);}
};
const episodes=await import('../podcast-episode-state.js?browser-window-channel');
episodes.setPodcastAccount(17);
assert.equal(channels.length,1);
const channel=channels[0];
assert.equal(channel.name,'player-podcast-account');
assert.deepEqual(channel.messages,[{account:17}]);
channel.onmessage({data:{account:29}});
assert.equal(episodes.podcastSyncStatus().account,29);
const count=channel.messages.length;
channel.onmessage({data:{account:29}});
assert.equal(channel.messages.length,count,'repeated broadcasts do not echo');
channel.onmessage({data:{account:null}});
assert.equal(episodes.podcastSyncStatus().account,null);
console.log('Browser account broadcasts and channel-free partial-window environments passed.');
