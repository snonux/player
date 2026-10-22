"""Read fresh Android AccessibilityNodeInfo hints omitted by old UI XML dumps."""
import json
import os
from pathlib import Path
import subprocess

REMOTE_JAR = '/data/local/tmp/zk3-accessibility-probe.jar'


def build_install(directory):
    directory = Path(directory).resolve()
    classes, dex = directory/'classes', directory/'dex'
    classes.mkdir(parents=True, exist_ok=True); dex.mkdir(exist_ok=True)
    sdk = Path(os.environ.get('ANDROID_HOME','/home/paul/Android/Sdk'))
    platform = sdk/'platforms/android-34/android.jar'
    source = Path(__file__).with_name('MusicAccessibilityProbe.java')
    commands = [
        ['javac','-source','8','-target','8','-cp',str(platform),'-d',str(classes),str(source)],
        [str(sdk/'build-tools/34.0.0/d8'),'--lib',str(platform),'--output',str(dex),str(classes/'MusicAccessibilityProbe.class')],
        ['jar','cf',str(directory/'music-accessibility-probe.jar'),'-C',str(dex),'classes.dex'],
        ['adb','push',str(directory/'music-accessibility-probe.jar'),REMOTE_JAR],
    ]
    (directory/'commands.json').write_text(json.dumps(commands,indent=2))
    for command in commands:
        subprocess.run(command,check=True,timeout=30)


def snapshot(path):
    command=['adb','shell',f'CLASSPATH={REMOTE_JAR}:/system/framework/uiautomator.jar',
             'app_process','/system/bin','MusicAccessibilityProbe']
    result=subprocess.run(command,capture_output=True,text=True,timeout=20)
    Path(path).write_text(json.dumps({'command':command,'exit':result.returncode,
        'stdout':result.stdout,'stderr':result.stderr},indent=2))
    lines=[line for line in result.stdout.splitlines() if line.startswith('{')]
    if result.returncode or not lines:
        raise RuntimeError(('fresh Android accessibility probe failed',result.returncode,result.stderr))
    nodes=json.loads(lines[0])['nodes']
    if not nodes or not any(node.get('editable') for node in nodes):
        raise RuntimeError('Fresh native field tree unavailable after bounded root polling')
    return nodes


def editable_field(nodes, label):
    found=[node for node in nodes if node.get('editable') and node.get('visible')
           and node.get('enabled') and node.get('hint')==label]
    if len(found)!=1:
        raise AssertionError(('expected one fresh visible enabled editable hint',label,found))
    bounds=found[0].get('bounds',[])
    if len(bounds)!=4 or bounds[2]<=bounds[0] or bounds[3]<=bounds[1]:
        raise AssertionError(('invalid native field bounds',found[0]))
    return found[0]
