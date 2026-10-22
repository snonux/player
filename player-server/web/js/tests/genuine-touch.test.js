import assert from 'node:assert/strict';
import { genuineTouchTap } from '../../../test/e2e-web/tests/helpers/genuine-touch.cjs';
const calls=[];
class BidiPage {
 constructor(session) {this._session=session;}
}
const session={sessionId:'owned-context',send:async(method,params)=>calls.push({method,params})};
const page={context:()=>({browser:()=>({browserType:()=>({name:()=> 'firefox'})})}),_connection:{toImpl:()=>({delegate:new BidiPage(session)})}};
const taps=[],locator={tap:async options=>taps.push(options),boundingBox:async()=>({x:12,y:30,width:100,height:44})};
await genuineTouchTap(page,locator);
assert.deepEqual(taps,[{trial:true}]);
assert.equal(calls[0].method,'input.performActions');assert.equal(calls[0].params.context,'owned-context');
const pointer=calls[0].params.actions[0];assert.equal(pointer.parameters.pointerType,'touch');
assert.deepEqual(pointer.actions.map(action=>action.type),['pointerMove','pointerDown','pause','pointerUp']);
assert.deepEqual(pointer.actions[0],{type:'pointerMove',origin:'viewport',x:62,y:52,duration:0});
assert.deepEqual(calls[1],{method:'input.releaseActions',params:{context:'owned-context'}});
const before=calls.length;
await assert.rejects(genuineTouchTap({...page,_connection:{}},locator),/transport unavailable/);
assert.equal(calls.length,before,'missing transport cannot send input or silently use mouse/DOM');
await assert.rejects(genuineTouchTap(page,{...locator,tap:async()=>{throw new Error('obscured');}}),/obscured/);
assert.equal(calls.length,before,'failed actionability cannot send input');
await assert.rejects(genuineTouchTap(page,{...locator,boundingBox:async()=>null}),/visible bounds/);
assert.equal(calls.length,before);
const failureSession={sessionId:'owned-context',send:async(method)=>{calls.push({method});if(method==='input.performActions')throw new Error('input failed');}};
await assert.rejects(genuineTouchTap({...page,_connection:{toImpl:()=>({delegate:new BidiPage(failureSession)})}},locator),/input failed/);
assert.equal(calls.at(-1).method,'input.releaseActions','failed action still releases only the owned context');
const chromium={context:()=>({browser:()=>({browserType:()=>({name:()=> 'chromium'})})})};
await assert.rejects(genuineTouchTap({context:()=>({browser:()=>null})},locator),/Unknown browser/);
const prior=taps.length;await genuineTouchTap(chromium,locator);assert.equal(taps.length,prior+1);assert.equal(taps.at(-1),undefined,'Chrome retains its original Locator.tap path');
console.log('Genuine BiDi touch transport/actions, fail-closed guards, cleanup and Chrome preservation tests passed.');
