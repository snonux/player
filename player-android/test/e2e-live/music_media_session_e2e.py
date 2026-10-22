#!/usr/bin/env python3
"""M54 real notification/keyguard metadata, OS artwork bitmap and transport."""
import argparse
import json
from pathlib import Path
import re
import signal
import subprocess
import xml.etree.ElementTree as ET
import sys
import time
import traceback
from PIL import Image
import adrv
import app_ui
import music_e2e as baseline
import music_sequence_e2e as sequence
import music_playlists_e2e as playlists
import music_playlist_songs_e2e as edits
import media_session_probe as probe
from fixture_server import Client


class Run(baseline.Run):
    def record(self, scenario, ok, detail='', data=None, artifacts=(), result=None):
        result = result or ('pass' if ok else 'fail')
        self.evidence.record('M54', scenario, result, detail, data, artifacts)
        print(f'{result.upper():5} {scenario} [{detail}]', flush=True)
        if result == 'fail':
            self.failures += 1


def os_state(run, song, name, color=None, playing=None):
    deadline = time.monotonic() + 20
    art = run.evidence.path(name + '-os-art.png')
    history = []
    while time.monotonic() < deadline:
        current = probe.snapshot(art)
        history.append(current)
        tags = song.get('tags') or {}
        if (current.get('title') == song['title'] and
                current.get('artist') == (tags.get('artist') or 'Unknown artist') and
                ((current.get('album') or '') == (tags.get('album') or '')) and
                str(current.get('mediaId', '')).endswith(':' + str(song['id'])) and
                (playing is None or current.get('state') == (3 if playing else 2)) and
                (not color or current.get('artwork'))):
            if color:
                image = Image.open(art).convert('RGB'); image.load()
                pixel = image.getpixel((image.width // 2, image.height // 2))
                if any(abs(pixel[i] - color[i]) >= 8 for i in range(3)):
                    time.sleep(.25); continue
                current['decoded'] = {'size': image.size, 'pixel': pixel}
            elif color == []:
                assert not current.get('artwork'), current
            path = run.evidence.path(name + '-os-state.json')
            path.write_text(json.dumps({'current': current, 'history': history}, indent=2))
            return current
        time.sleep(.25)
    raise AssertionError(('wrong actual OS metadata/state/artwork', song, history[-8:]))


def system_tap(run, name, scene):
    patterns = {'next': r'(?i)^(?:skip to |skip )?next(?: track)?$',
                'previous': r'(?i)^(?:skip to |skip )?previous(?: track)?$',
                'play': r'(?i)^play$', 'pause': r'(?i)^pause$'}
    if run.coordinate_controls:
        shot = run.shot(f'm54-{scene}-{name}-button')
        coordinates = run.evidence.path(f'm54-{scene}-{name}-coordinate.json')
        print(f'VISIBLE_CONTROL {name} {shot} {coordinates}',flush=True)
        deadline=time.monotonic()+300
        while not coordinates.exists() and time.monotonic()<deadline: time.sleep(.5)
        data=json.loads(coordinates.read_text())
        size=Image.open(shot).size
        assert data['screenshot']==str(shot) and data['dimensions']==list(size)
        x,y=data['x'],data['y'];assert 0<x<size[0] and 0<y<size[1]
        adrv.adb('shell','input','tap',str(x),str(y))
        return shot,coordinates
    remote = f'/sdcard/qk3-{time.time_ns()}.xml'
    result = subprocess.run(['adb','shell','uiautomator','dump',remote],capture_output=True,text=True)
    dump_log = run.evidence.path(f'm54-{scene}-{name}-dump.json')
    dump_log.write_text(json.dumps({'exit':result.returncode,'stdout':result.stdout,'stderr':result.stderr},indent=2))
    assert result.returncode == 0 and 'UI hierchary dumped to:' in result.stdout, (result.stdout,result.stderr)
    xml = adrv.adb('exec-out','cat',remote)
    ui = run.evidence.path(f'm54-{scene}-{name}-system-ui.xml')
    ui.write_text(xml)
    adrv.adb('shell','rm','-f',remote)
    root = ET.fromstring(xml)
    matches = [n for n in root.iter('node') if n.get('package') == 'com.android.systemui'
               and re.search(patterns[name], n.get('content-desc') or n.get('text') or '')]
    assert len(matches) == 1, (scene,name,[n.attrib for n in root.iter('node') if n.get('content-desc')])
    bounds = [int(n) for n in re.findall(r'\d+',matches[0].get('bounds'))]
    shot = run.shot(f'm54-{scene}-{name}-button')
    adrv.tap_node({'b':bounds})
    return shot, ui


def proof_audio(run, song, mark, name):
    time.sleep(7)
    wav = run.audio.save(mark, run.evidence.path(name + '.wav'))
    analysis = sequence.audio_trace.analyze(wav)
    segment = baseline.main_segment(analysis)
    assert segment and segment['duration_s'] > 2 and baseline.tone_matches(segment, song), analysis
    return wav, analysis


def saved(run, song):
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        detail = run.viewer.request('GET', f'/api/v1/media/{song["id"]}')
        if detail.get('progress', {}).get('position_seconds', 0) >= 2:
            assert detail['media']['id'] == song['id']
            return detail
        time.sleep(.5)
    raise AssertionError(('no fresh progress for actual source', song, detail))


def journey(run):
    a = run.track(f'Music/{baseline.ALBUM}/01 - First Light.mp3')
    b = run.track('Music/Tag Verification/source_001.flac')
    c = run.track('Music/Untagged/track_0042.mp3')
    blue, orange = [43, 76, 126], [210, 90, 32]
    assert all(song['duration_s'] == 45 for song in (a, b, c)), '--session-artwork fixture required'
    pid=None
    if not run.from_boundaries and not run.offline_only:
        for song in (a, b, c): baseline.reset_progress(run.viewer, song['id'])
        name = 'M54 OS controls ' + str(time.time_ns())
        pid = run.viewer.request('POST', '/api/v1/playlists', {'name': name})['playlist_id']
        run.viewer.request('POST', '/api/v1/playlists/' + pid + '/songs', {'action': 'add', 'media_ids': [a['id'], b['id'], c['id']]})
    try:
        if not run.from_boundaries and not run.offline_only:
            playlists.catalog(); playlists.tap_visible(re.escape(name)); playlists.wait_detail(name)
            adrv.tap_node(edits.row(1, a['title'])); sequence.wait_track(a['title'])
            for _ in range(15): adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_VOLUME_UP')
            sequence.ensure_music_volume(run)
            # Initial full player -> Back -> mini -> current full player, preserving
            # source and position. No Play selection is used to reopen it.
            if not run.skip_mini:
                full = run.shot('m54-initial-full')
                app_ui.back(); time.sleep(.5)
                mini = adrv.find(r'^Open player: First Light, Aurora Lane,?')
                if not mini: mini = adrv.find(r'^Open player: First Light, Aurora Lane')
                assert mini, app_ui.labels()
                before = os_state(run, a, 'm54-before-mini-open', blue)
                adrv.tap_node(mini); time.sleep(.5)
                after = os_state(run, a, 'm54-after-mini-open', blue)
                assert after['position'] >= before['position'], (before, after)
                run.record('full and mini share rich metadata; mini opens existing source without position reset', True,
                           data={'before': before, 'after': after}, artifacts=[full, run.shot('m54-reopened-full')])
                app_ui.back()
            else:
                app_ui.back()
            adrv.adb('shell', 'cmd', 'statusbar', 'expand-notifications');time.sleep(1)
            system_tap(run, 'pause', 'notification-initial')
            os_state(run, a, 'm54-notification-initial-paused', blue, False)
            for scene in ['notification', 'keyguard']:
                if scene == 'keyguard':
                    adrv.adb('shell', 'cmd', 'statusbar', 'collapse')
                    adrv.adb('shell', 'locksettings', 'set-disabled', 'false')
                    adrv.adb('shell', 'settings', 'put', 'secure', 'lock_screen_show_notifications', '1')
                    adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_SLEEP')
                    adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_WAKEUP');time.sleep(1)
                    policy=adrv.adb('shell','dumpsys','window','policy')+adrv.adb('shell','dumpsys','window')
                    assert re.search(r'mKeyguardShowing=true|mShowingLockscreen=true|isKeyguardShowing=true',policy) or re.search(r'KeyguardServiceDelegate[\s\S]*?showing=true',policy), 'Actual keyguard must be showing'
                    run.evidence.path('m54-keyguard-policy.txt').write_text(policy)
                for action, song, color in [('next', b, orange), ('previous', a, blue)]:
                    baseline.reset_progress(run.viewer, song['id'])
                    mark = run.audio.mark()
                    artifacts = list(system_tap(run, action, scene))
                    state = os_state(run, song, f'm54-{scene}-{action}', color, True)
                    wav, analysis = proof_audio(run, song, mark, f'm54-{scene}-{action}')
                    artifacts += [wav, Path(state['art_path']), run.shot(f'm54-{scene}-{action}-actual-art')]
                    artifacts += list(system_tap(run, 'pause', scene+'-'+action))
                    paused = os_state(run, song, f'm54-{scene}-{action}-paused', color, False)
                    progress = saved(run, song)
                    # Play/Pause controls must act on this source, not reload it.
                    artifacts += list(system_tap(run, 'play', scene+'-'+action))
                    playing = os_state(run, song, f'm54-{scene}-{action}-played', color, True)
                    assert playing['position'] >= paused['position'], (paused,playing)
                    artifacts += list(system_tap(run, 'pause', scene+'-'+action+'-again'))
                    os_state(run, song, f'm54-{scene}-{action}-paused-again', color, False)
                    run.record(f'{scene} visible Next/Previous/Play/Pause control actual rich session, bitmap, distinct source audio and fresh progress',True,
                               data={'action':action,'session':state,'paused':paused,'resumed':playing,'audio':analysis,'progress':progress},artifacts=artifacts)
        elif not run.offline_only:
            os_state(run,a,'m54-boundaries-resume',blue,False)
            if adrv.find('^Music visuals$'): app_ui.back()
        if not run.offline_only:
            adrv.adb('shell','wm','dismiss-keyguard');adrv.adb('shell','locksettings','set-disabled','true')
            adrv.adb('shell','cmd','statusbar','collapse')
            for theme, mode in [('light','no'),('dark','yes')]:
                adrv.adb('shell','cmd','uimode','night',mode);time.sleep(.5)
                mini = adrv.find(r'^Open player: First Light, Aurora Lane')
                assert mini, app_ui.labels()
                mini_shot=run.shot('m54-mini-'+theme)
                adrv.tap_node(mini);time.sleep(.5)
                assert adrv.find(r'First Light, Aurora Lane · Night Transit'), app_ui.labels()
                full_shot=run.shot('m54-full-'+theme)
                os_state(run,a,'m54-theme-'+theme,blue,False)
                app_ui.back()
                run.record(f'{theme} native full/mini rich metadata and artwork',True,artifacts=[mini_shot,full_shot])
            adrv.adb('shell','cmd','statusbar','expand-notifications');time.sleep(1)
            system_tap(run,'next','fallback-step1');os_state(run,b,'m54-fallback-known',orange,True)
            system_tap(run,'next','fallback-step2');os_state(run,c,'m54-untagged',[],True)
            system_tap(run,'pause','untagged')
            run.record('untagged filename title and Unknown artist clear previous album artwork in actual OS session',True,
                       artifacts=[run.shot('m54-untagged-notification')])
        else:
            mini=adrv.find(r'^Open player:')
            assert mini,app_ui.labels()
            adrv.tap_node(mini)
            current=probe.snapshot(run.evidence.path('m54-offline-setup-art.png'))
            while current.get('title') != c['title']:
                assert current.get('title') in [a['title'],b['title']],current
                assert adrv.tap('^Next track$'),app_ui.labels()
                time.sleep(1)
                current=probe.snapshot(run.evidence.path('m54-offline-setup-art.png'))
            if adrv.find('^Pause$'): assert adrv.tap('^Pause$')
            os_state(run,c,'m54-offline-setup-paused',[],False)
            app_ui.back()
            adrv.adb('shell','cmd','statusbar','expand-notifications');time.sleep(1)
        assert not run.host_alias, 'M54 outage requires isolated adb reverse transport'
        port=run.state['port']
        baseline.reset_progress(run.viewer,b['id'])
        reverse=adrv.adb('reverse','--list')
        assert f'tcp:{port}' in reverse, reverse
        adrv.adb('reverse','--remove',f'tcp:{port}')
        try:
            system_tap(run,'previous','actual-offline-target')
            adrv.adb('shell','cmd','statusbar','collapse')
            mini=adrv.find(r'^Open player: A Different Music Title, A & B; C/D')
            assert mini, app_ui.labels()
            adrv.tap_node(mini)
            interim=probe.snapshot(run.evidence.path('m54-offline-preparing-art.png'))
            run.evidence.path('m54-offline-preparing-state.json').write_text(json.dumps(interim,indent=2))
            run.shot('m54-offline-preparing')
            print('OFFLINE awaiting actual visible Retry with reverse still removed',flush=True)
            assert adrv.wait('^Retry$',timeout=65),app_ui.labels()
            offline=probe.snapshot(run.evidence.path('m54-offline-target-art.png'))
            assert not offline.get('present') or (offline.get('title')==b['title'] and offline.get('artist')==b['tags']['artist'] and offline.get('album')==b['tags']['album'] and not offline.get('artwork')),offline
            run.evidence.path('m54-offline-target-state.json').write_text(json.dumps(offline,indent=2))
            error_shot=run.shot('m54-offline-full-retry')
        finally:
            adrv.adb('reverse',f'tcp:{port}',f'tcp:{port}')
        mark=run.audio.mark()
        assert adrv.tap('^Retry$'),app_ui.labels()
        recovered=os_state(run,b,'m54-offline-recovered',orange,True)
        wav,analysis=proof_audio(run,b,mark,'m54-offline-recovered')
        recovered_progress=saved(run,b)
        adrv.adb('shell','cmd','statusbar','expand-notifications');time.sleep(1)
        system_tap(run,'pause','offline-recovered')
        paused=os_state(run,b,'m54-recovered-paused',orange,False)
        system_tap(run,'play','offline-recovered')
        played=os_state(run,b,'m54-recovered-played',orange,True)
        assert played['position']>=paused['position'],(paused,played)
        system_tap(run,'pause','offline-recovered-again')
        run.record('actual reverse cutoff keeps failed target identity without stale artwork; visible Retry recovers then system Play/Pause controls recovered source',True,
                   detail='Existing Android system Play while failed remains a no-op; full-player Retry is required.',
                   data={'reverse_before':reverse,'offline':offline,'recovered':recovered,'audio':analysis,'progress':recovered_progress},artifacts=[error_shot,wav])
        adrv.adb('shell','cmd','statusbar','collapse')
        app_ui.back();assert app_ui.go_home(),app_ui.labels()
        assert adrv.tap('^Settings$'),app_ui.labels()
        playlists.tap_visible('^Log Out$')
        assert adrv.wait('^Welcome back$|^Choose a server$',timeout=30),app_ui.labels()
        deadline=time.monotonic()+15
        while time.monotonic()<deadline:
            cleared=probe.snapshot(run.evidence.path('m54-logout-art.png'))
            if not cleared.get('present') or not cleared.get('title'):break
            time.sleep(.25)
        assert not cleared.get('present') or not cleared.get('title'),cleared
        run.record('logout clears actual OS music identity and removes obsolete artwork',True,data={'session':cleared},artifacts=[run.shot('m54-logged-out')])
    finally:
        if pid: run.viewer.request('DELETE','/api/v1/playlists/'+pid)


def main(argv=None):
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir');parser.add_argument('--apk');parser.add_argument('--emulator-wav',required=True)
    parser.add_argument('--host-alias',action='store_true');parser.add_argument('--probe-jar',required=True)
    parser.add_argument('--resume-device',action='store_true')
    parser.add_argument('--skip-mini',action='store_true')
    parser.add_argument('--coordinate-controls',action='store_true')
    parser.add_argument('--from-boundaries',action='store_true')
    parser.add_argument('--offline-only',action='store_true')
    parser.add_argument('--evidence-dir')
    args=parser.parse_args(argv);run=None
    signal.signal(signal.SIGTERM,lambda *_:sys.exit('M54 interrupted; root owns stop-only cleanup'))
    try:
        port=json.loads((Path(args.fixture_dir)/'state.json').read_text())['port']
        baseline.prepare_device(args,port);probe.install(args.probe_jar)
        run=Run(args);run.failures=0;run.host_alias=args.host_alias;run.skip_mini=args.skip_mini;run.coordinate_controls=args.coordinate_controls;run.from_boundaries=args.from_boundaries;run.offline_only=args.offline_only
        if args.evidence_dir:
            run.evidence=baseline.Evidence(args.evidence_dir,'android',baseline.device_versions(args,run.base))
        run.viewer=Client(run.state['origin']).login(**run.auth['user'])
        assert run.audio.available(),'Actual QEMU output required'
        if args.resume_device:
            assert app_ui.go_home(),app_ui.labels()
        else:
            assert app_ui.connect_and_login(run.base,run.auth['user']['username'],run.auth['user']['password'],
                lambda name,ok,detail='':run.record('sign-in: '+name,ok,detail),run.shot)
        journey(run)
    except Exception:
        if not run:raise
        trace=run.evidence.path('m54-failure.txt');trace.write_text(traceback.format_exc())
        run.record('native actual session journey',False,traceback.format_exc(),artifacts=[trace,run.shot('m54-failed')])
    finally: print('Runtime retained for root evidence audit and stop-only cleanup.',flush=True)
    return 1 if run.failures else 0


if __name__=='__main__':sys.exit(main())
