#!/usr/bin/env python3
"""M27 real native playlist selections, occurrence edits, find and persistence.

Requires the isolated music fixture, a current release APK, running API34
emulator and QEMU output WAV. Extends existing M26 helpers; no media mocks.
"""
import argparse
import json
from pathlib import Path
import re
import signal
import sys
import time
import traceback

import adrv
import app_ui
import music_e2e as baseline
import music_albums_e2e as albums
import music_playlists_e2e as playlists
from fixture_server import Client, PlayerServer, answers, stop_pid
from music_fixture import save_state, started_process


class Run(albums.Run):
    def record(self, scenario, ok, detail='', data=None, artifacts=(), result=None):
        result = result or ('pass' if ok else 'fail')
        self.evidence.record('M27', scenario, result, detail, data, artifacts)
        print(f'{result.upper():5} {scenario} [{detail}]', flush=True)
        if result == 'fail':
            self.failures += 1


def visible(node):
    size = re.search(r'Physical size: (\d+)x(\d+)', adrv.adb('shell', 'wm', 'size'))
    return node and size and 0 <= node['b'][1] < node['b'][3] <= int(size.group(2))


def control(pattern, up=True, attempts=40):
    for _ in range(attempts):
        hits = [node for node in adrv.nodes() if re.search(pattern, adrv.label(node)) and visible(node)]
        if hits:
            return hits[-1]
        app_ui.swipe(up=up)
    raise AssertionError((pattern, app_ui.labels()))


def tap(pattern, up=True):
    adrv.tap_node(control(pattern, up))


def search(value):
    for _ in range(40):
        if any(node['cls']=='EditText' and visible(node) for node in adrv.nodes()):
            break
        app_ui.swipe(up=False)
    else:
        raise AssertionError(('playlist search input not visible',app_ui.labels()))
    playlists.field(0, value)
    adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_BACK')
    time.sleep(.5)


def choice(title):
    hits = [node for node in adrv.nodes() if node['cls'] != 'EditText'
            and title in adrv.label(node) and node['click'] and visible(node)]
    assert hits, app_ui.labels()
    adrv.tap_node(hits[-1])


def row(number, title):
    ordinal = re.compile(r'(^| / )' + str(number) + r'($| / |\. )')
    for _ in range(40):
        hits = [node for node in adrv.nodes() if title in adrv.label(node)
                and ordinal.search(adrv.label(node)) and visible(node)]
        if hits:
            return max(hits, key=lambda node: (node['b'][2]-node['b'][0]) * (node['b'][3]-node['b'][1]))
        app_ui.swipe()
    raise AssertionError((number, title, app_ui.labels()))


def song_menu(number, title):
    target = row(number, title)
    hits = [node for node in adrv.nodes() if adrv.label(node) == 'Song actions for ' + title
            and visible(node) and target['b'][1] <= (node['b'][1]+node['b'][3])//2 <= target['b'][3]]
    assert hits, (target, app_ui.labels())
    adrv.tap_node(hits[-1])


def saved(run, pid):
    return run.viewer.request('GET', '/api/v1/playlists/' + pid)


def wait_count(run, pid, count):
    deadline = time.monotonic()+20
    while time.monotonic()<deadline:
        p=saved(run,pid)
        if p['song_count']==count:
            return p
        time.sleep(.3)
    raise AssertionError(p)


def edit(run, pid=None):
    if pid is None:
        name='M27 native long playlist '+str(int(time.time()))
        p=run.viewer.request('POST','/api/v1/playlists',{'name':name,'comment':'Native occurrence editing'})
        pid=p['playlist_id']
        album=next(a for a in run.viewer.request('GET','/api/v1/music/albums') if a['title']=='Night Transit')
        for _ in range(15):
            run.viewer.request('POST','/api/v1/playlists/'+pid+'/albums',{'album_key':album['album_key']})
    p=saved(run,pid)
    name=p['name']
    playlists.catalog()
    tap(re.escape(name))
    playlists.wait_detail(name)
    tap('^Add songs$',up=False)
    assert adrv.wait('.*',cls='EditText',timeout=30),app_ui.labels()
    playlists.field(0,'Harbour')
    adrv.adb('shell','input','keyevent','KEYCODE_BACK')
    choice('Harbour')
    playlists.field(0,'First Light')
    adrv.adb('shell','input','keyevent','KEYCODE_BACK')
    choice('First Light')
    assert adrv.wait('^2 selected$'),app_ui.labels()
    # Actual network cutoff must retain visible selections and query.
    port=run.state['port']
    previous=dict(run.state['server'])
    assert stop_pid(previous['pid'],previous['binary'],start=previous.get('start')), 'Owned server cutoff failed'
    assert answers(run.state['origin'],'/healthz') is None, 'Owned server still answers'
    adrv.adb('reverse','--remove',f'tcp:{port}')
    try:
        assert adrv.tap('^Add selected songs$'),app_ui.labels()
        assert adrv.wait('Could not add songs',timeout=35),app_ui.labels()
        assert adrv.find('First Light',cls='EditText'),app_ui.labels()
        assert adrv.find('^2 selected$'),app_ui.labels()
        run.record('actual offline add retains query and two selections without saved mutation',True,data={'id':pid},artifacts=[run.shot('m27-add-offline')])
    finally:
        server=PlayerServer(previous['binary'],run.fixture_dir,port)
        run.state['server'].update(started_process(server.start(detached=True)))
        run.state['server']['restarts']=run.state['server'].get('restarts',0)+1
        save_state(run.fixture_dir,run.state)
        ownership=Path('/home/paul/tmp/nl3-evidence/runtime-ownership.json')
        owned=json.loads(ownership.read_text());owned['server']=run.state['server'];ownership.write_text(json.dumps(owned,indent=2))
        adrv.adb('reverse',f'tcp:{port}',f'tcp:{port}')
        run.record('owned fixture restarted on same DB after verified offline cutoff',True,data={'previous':previous,'current':run.state['server']})
    assert saved(run,pid)['entries']==p['entries'], 'Offline save changed durable membership'
    shots=[]
    for theme,system in [('light','no'),('dark','yes')]:
        adrv.adb('shell','cmd','uimode','night',system)
        time.sleep(2)
        shots.append(run.shot('m27-add-'+theme))
    assert adrv.tap('^Add selected songs$'),app_ui.labels()
    p=wait_count(run,pid,47)
    assert [song['music']['title'] for song in p['songs'][-2:]]==['First Light','Harbour'],p
    assert len({entry['entry_id'] for entry in p['entries']})==47,p
    assert adrv.wait('.*',cls='EditText',timeout=20),app_ui.labels()
    search('Harbour')
    assert adrv.wait('^Showing 16 of 47 songs$'),app_ui.labels()
    original=p
    last=p['entries'][-1]['entry_id']
    song_menu(47,'Harbour')
    assert adrv.tap('^Move up$'),app_ui.labels()
    assert adrv.wait(r'^Song moved\.$'),app_ui.labels()
    moved=saved(run,pid)
    assert moved['entries'][45]['entry_id']==last and moved['entries'][46]['entry_id']==original['entries'][45]['entry_id'],moved
    # Let SnackBar leave before the next row menu near the bottom edge.
    time.sleep(5)
    song_menu(46,'Harbour')
    assert adrv.tap('^Remove from playlist$'),app_ui.labels()
    assert adrv.wait(r'^Song removed from playlist\.$'),app_ui.labels()
    removed=wait_count(run,pid,46)
    assert all(entry['entry_id']!=last for entry in removed['entries']),removed
    assert any(entry['entry_id']==original['entries'][2]['entry_id'] for entry in removed['entries']),removed
    assert adrv.tap('^Undo$'),app_ui.labels()
    restored=wait_count(run,pid,47)
    assert [song['id'] for song in restored['songs']]==[song['id'] for song in moved['songs']],restored
    assert restored['entries'][45]['entry_id']!=last,restored
    search('r/h/l/?')
    assert adrv.wait('No matching songs'),app_ui.labels()
    search('Harbour')
    for theme,system in [('light','no'),('dark','yes')]:
        adrv.adb('shell','cmd','uimode','night',system)
        time.sleep(2)
        shots.append(run.shot('m27-find-'+theme))
    # Back closes an actual Add songs dialog without changing saved membership.
    tap('^Add songs$',up=False)
    assert adrv.wait('.*',cls='EditText'),app_ui.labels()
    app_ui.back()
    assert adrv.wait('.*',cls='EditText'),app_ui.labels()
    assert saved(run,pid)['entries']==restored['entries']
    run.evidence.path('m27-checkpoint.json').write_text(json.dumps({'playlist_id':pid,'playlist':restored},indent=2))
    run.record('touch selected additions long list local Find later duplicate move remove Undo Back and themes',True,
               data={'original':original,'moved':moved,'removed':removed,'restored':restored},artifacts=shots)
    return pid


def original_queue_occurrence(number, title):
    assert adrv.wait('^Next track$'), app_ui.labels()
    previous = None
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        node = adrv.find('^Now Playing queue$')
        if node and node['b'] == previous:
            adrv.tap_node(node)
            break
        previous = node['b'] if node else None
        time.sleep(.3)
    else:
        raise AssertionError(('queue toolbar did not settle', app_ui.labels()))
    assert adrv.wait('^Now Playing$'), app_ui.labels()
    for _ in range(40):
        labels = app_ui.labels()
        if any(title in label and 'Now' in label and re.search(r'(^| / )' + str(number) + r'($| / )', label) for label in labels):
            app_ui.back()
            return labels
        app_ui.swipe()
    raise AssertionError(('original queue occurrence', number, title, app_ui.labels()))


def playback(run,pid):
    p=saved(run,pid)
    playlists.catalog();tap(re.escape(p['name']));playlists.wait_detail(p['name'])
    search('Harbour')
    target=row(46,'Harbour')
    adrv.tap_node(target)
    playlists.wait_playing('Harbour')
    playlists.pause_for_inspection('Harbour')
    original_queue_occurrence(46,'Harbour')
    playlists.verify_music_volume(run)
    mark=run.audio.mark()
    adrv.adb('shell','input','keyevent','KEYCODE_MEDIA_PLAY')
    playlists.wait_playing('Harbour')
    time.sleep(4)
    source=next(run.track(t['path']) for t in run.manifest['tracks'] if t['title']=='Harbour')
    wav,analysis,matches=albums.proof_audio(run,mark,'m27-filtered-later-source',[source])
    playlists.pause_for_inspection('Harbour')
    run.record('filtered later duplicate plays original full playlist occurrence46 with actual original source output',True,
               data={'playlist':p,'analysis':analysis,'matches':matches},artifacts=[wav,run.shot('m27-original-queue-source')])
    app_ui.back()
    return pid


def persist(run,pid):
    before=saved(run,pid)
    playlists.restart(run)
    playlists.catalog();tap(re.escape(before['name']));playlists.wait_detail(before['name'])
    after=saved(run,pid)
    assert after['entries']==before['entries'] and after['song_count']==47,after
    search('Harbour')
    assert adrv.wait('^Showing 16 of 47 songs$'),app_ui.labels()
    assert run.admin.call('POST','/api/v1/playlists/'+pid+'/songs',{'action':'remove','entry_id':after['entries'][0]['entry_id']})[0]==404
    run.record('force-stop/relaunch playlist order membership persisted and foreign admin mutation rejected',True,
               data={'before':before,'after':after},artifacts=[run.shot('m27-restarted')])
    app_ui.back()
    assert adrv.wait('^Music · Playlists$'),app_ui.labels()
    app_ui.back()
    assert adrv.wait('^Library$'),app_ui.labels()


def main(argv=None):
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('--apk')
    parser.add_argument('--emulator-wav',required=True)
    parser.add_argument('--host-alias',action='store_true')
    parser.add_argument('--hold-device',action='store_true',help='Keep native app/device state and reverse transport for root review; no teardown')
    parser.add_argument('--resume-playlist')
    parser.add_argument('--resume-device',action='store_true',help='Continue the already signed-in held device without reinstall/login')
    parser.add_argument('--resume-stage',choices=['edit','playback','persist'],default='edit')
    args=parser.parse_args(argv)
    assert not args.host_alias,'Offline acceptance requires isolated adb reverse transport'
    port=json.loads((Path(args.fixture_dir)/'state.json').read_text())['port']
    run=None
    signal.signal(signal.SIGTERM,lambda *_:sys.exit('M27 terminated'))
    try:
        baseline.prepare_device(args,port)
        run=Run(args);run.failures=0
        run.viewer=Client(run.state['origin']).login(**run.auth['user'])
        assert run.audio.available(),'QEMU WAV output required'
        if not args.resume_device:
            assert app_ui.connect_and_login(run.base,run.auth['user']['username'],run.auth['user']['password'],lambda name,ok,detail='':run.record('sign-in: '+name,ok,detail),run.shot)
        try:
            pid=args.resume_playlist
            if args.resume_stage=='edit':
                pid=edit(run,pid)
            assert pid,'Resume needs --resume-playlist'
            if args.resume_stage in ('edit','playback'):
                playback(run,pid)
            persist(run,pid)
        except Exception:
            trace=run.evidence.path('m27-failed-traceback.txt');trace.write_text(traceback.format_exc())
            run.record('native playlist song journey',False,traceback.format_exc(),artifacts=[run.shot('m27-failed'),trace])
    finally:
        if run and not args.hold_device:
            baseline.teardown(run,port)
        elif run:
            run.record('owned device held idle for root original log and state review before stop-only cleanup',True)
    return 1 if not run or run.failures else 0


if __name__=='__main__':
    sys.exit(main())
