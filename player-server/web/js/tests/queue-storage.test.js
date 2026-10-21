import assert from 'node:assert/strict';
import { queueStorageKey, readQueueSnapshot } from '../queue-storage.js';
const snapshot = {version:1,entries:[{id:1,mediaId:5},{id:2,mediaId:5}],index:1,position:7.25,label:'Repeated',upNext:[2]};
assert.deepEqual(readQueueSnapshot(JSON.stringify(snapshot)),snapshot);
assert.equal(readQueueSnapshot(null),null);
for(const value of ['bad','{}',JSON.stringify({...snapshot,version:2}),JSON.stringify({...snapshot,index:2}),JSON.stringify({...snapshot,position:-1}),JSON.stringify({...snapshot,entries:[{id:1,mediaId:5},{id:1,mediaId:6}]}),JSON.stringify({...snapshot,upNext:[99]})]) assert.throws(()=>readQueueSnapshot(value));
assert.notEqual(queueStorageKey('http://a',1),queueStorageKey('http://b',1));
assert.notEqual(queueStorageKey('http://a',1),queueStorageKey('http://a',2));
console.log('Queue snapshot validation passed.');
