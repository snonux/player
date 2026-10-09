#!/usr/bin/env python3
"""Tiny adb/uiautomator driver for the Player Android app (Flutter semantics)."""
import re, subprocess, sys, time, html

def adb(*a, binary=False):
    r = subprocess.run(["adb", *a], capture_output=True)
    return r.stdout if binary else r.stdout.decode(errors="replace")

def nodes():
    for _ in range(4):
        adb("shell", "uiautomator", "dump", "/sdcard/ui.xml")
        x = adb("exec-out", "cat", "/sdcard/ui.xml")
        if "<node" in x: break
        time.sleep(1)
    out = []
    for m in re.finditer(r"<node[^>]*>", x):
        n = m.group(0)
        g = lambda k: html.unescape(re.search(k + r'="([^"]*)"', n).group(1))
        b = [int(v) for v in re.findall(r"\d+", g("bounds"))]
        out.append(dict(text=g("text"), desc=g("content-desc"), cls=g("class").split(".")[-1], b=b,
                        click=g("clickable") == "true", checked=g("checked"), sel=g("selected"), focused=g("focused")))
    return out

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
    # `input text` needs shell escaping; %s is its encoding for a space.
    esc = "".join("%s" if c == " " else ("\\" + c if c in "\\'\"`$&|;<>()*?!#~{}[]" else c) for c in s)
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
