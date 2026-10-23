// Collection and mix reads share ownership before any asynchronous resolution.
int _revision = 0;
int beginMusicActionIntent() => ++_revision;
bool isMusicActionIntent(int intent) => intent == _revision;
