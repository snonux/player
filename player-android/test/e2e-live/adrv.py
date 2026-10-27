#!/usr/bin/env python3
"""Tiny adb/uiautomator driver for the Player Android app (Flutter semantics)."""
import re, subprocess, sys, time, uuid
import xml.etree.ElementTree as ET

# A hung device command (uiautomator after a host suspend) must fail the step
# instead of blocking a whole run.
ADB_TIMEOUT = 120

def adb(*a, binary=False):
    r = subprocess.run(["adb", *a], capture_output=True, timeout=ADB_TIMEOUT)
    return r.stdout if binary else r.stdout.decode(errors="replace")

def _ui_diagnostic(result):
    return (f"command={result.args!r}, exit={result.returncode}, "
            f"stdout={result.stdout!r}, stderr={result.stderr!r}")

def _ui_command(*args):
    command = ["adb", *args]
    try:
        result = subprocess.run(command, capture_output=True, timeout=ADB_TIMEOUT)
    except subprocess.TimeoutExpired as error:
        raise RuntimeError(f"UI command timed out: command={command!r}, "
                           f"stdout={error.stdout!r}, stderr={error.stderr!r}") from error
    except OSError as error:
        raise RuntimeError(f"UI command could not run: command={command!r}: {error}") from error
    if result.returncode != 0:
        raise RuntimeError("UI command failed: " + _ui_diagnostic(result))
    return result

def nodes():
    """Read one fresh complete screen, or raise before selectors can use it."""
    # Playback/animations often make uiautomator report "could not get idle state".
    last_error = None
    for attempt in range(6):
        remote = f"/sdcard/adrv-{uuid.uuid4().hex}.xml"
        try:
            try:
                dump = _ui_command("shell", "uiautomator", "dump", remote)
            except RuntimeError as error:
                # Android can kill the short-lived dump process while the UI
                # remains healthy. A fresh dump is safe to retry in that case.
                if "exit=137" in str(error) and attempt < 5:
                    last_error = error
                    time.sleep(0.6 + attempt * 0.3)
                    continue
                raise
            marker = f"UI hierchary dumped to: {remote}"
            text_out = dump.stdout.decode(errors="replace")
            text_err = dump.stderr.decode(errors="replace")
            retryable = ("could not get idle state" in text_err.lower()
                         or "null root node returned" in text_err.lower())
            if (marker not in text_out.splitlines()
                    or b"ERROR" in dump.stdout.upper() or b"ERROR" in dump.stderr.upper()):
                last_error = RuntimeError("Fresh UI dump failed: " + _ui_diagnostic(dump))
                if retryable and attempt < 5:
                    time.sleep(0.6 + attempt * 0.3)
                    continue
                raise last_error
            output = _ui_command("exec-out", "cat", remote)
            try:
                root = ET.fromstring(output.stdout)
                if root.tag != "hierarchy":
                    raise ValueError("expected hierarchy root")
                out = []
                for node in root.iter("node"):
                    g = node.attrib.__getitem__
                    bounds = re.fullmatch(r"\[([+-]?\d+),([+-]?\d+)\]\[([+-]?\d+),([+-]?\d+)\]", g("bounds"))
                    if bounds is None:
                        raise ValueError(f"invalid node bounds: {g('bounds')!r}")
                    out.append(dict(text=g("text"), desc=g("content-desc"), cls=g("class").split(".")[-1],
                                    b=[int(v) for v in bounds.groups()], click=g("clickable") == "true",
                                    checked=g("checked"), sel=g("selected"), focused=g("focused")))
                return out
            except (ET.ParseError, ValueError, KeyError) as error:
                raise RuntimeError(f"Invalid fresh UI dump: {error}; " + _ui_diagnostic(output)) from error
        finally:
            primary = sys.exc_info()[1]
            try:
                _ui_command("shell", "rm", "-f", remote)
            except RuntimeError as error:
                if primary is None and last_error is None:
                    raise
                print(f"UI dump cleanup also failed: {error}", file=sys.stderr)
    raise last_error or RuntimeError("Fresh UI dump failed after retries")

def label(n): return (n["desc"] or n["text"]).replace("\n", " / ")

def show():
    for n in nodes():
        if n["desc"] or n["text"] or n["cls"] == "EditText":
            print(f'{n["cls"]:<12}| {label(n)[:110]:<110} | {n["b"]}{" click" if n["click"] else ""}{" sel" if n["sel"]=="true" else ""}{" chk" if n["checked"]=="true" else ""}')

def find(pat, cls=None, nth=0):
    hits = [n for n in nodes() if re.search(pat, label(n)) and (cls is None or n["cls"] == cls)]
    return hits[nth] if len(hits) > nth else None

def wait(pat, timeout=20, cls=None):
    end = time.time() + timeout
    while time.time() < end:
        n = find(pat, cls)
        if n: return n
        time.sleep(0.7)
    return None

def tap_node(n):
    x, y = (n["b"][0] + n["b"][2]) // 2, (n["b"][1] + n["b"][3]) // 2
    adb("shell", "input", "tap", str(x), str(y))

def tap(pat, cls=None, nth=0, timeout=15):
    end = time.time() + timeout
    while time.time() < end:
        n = find(pat, cls, nth)
        if n: tap_node(n); return True
        time.sleep(0.7)
    return False

def type_text(s):
    # `adb shell` joins args into a device sh -c line. Escape shell metacharacters
    # and URL punctuation so schemes like http:// survive intact (%s = space).
    special = "\\'\"`$&|;<>()*?!#~{}[]:/"
    esc = "".join("%s" if c == " " else ("\\" + c if c in special else c) for c in s)
    adb("shell", "input", "text", esc)

def shot(name):
    open(name, "wb").write(adb("exec-out", "screencap", "-p", binary=True))

if __name__ == "__main__":
    cmd, args = sys.argv[1], sys.argv[2:]
    if cmd == "show": show()
    elif cmd == "tap": print(tap(args[0], nth=int(args[1]) if len(args) > 1 else 0)); time.sleep(1.5); show()
    elif cmd == "text": type_text(args[0]); time.sleep(1); show()
    elif cmd == "key": adb("shell", "input", "keyevent", args[0]); time.sleep(1); show()
    elif cmd == "shot": shot(args[0])
