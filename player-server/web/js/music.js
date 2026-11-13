// Embedded tags never replace filesystem identity.
export function mediaTitle(media) {
 const name = media?.file_name || '';
 if (media?.type !== 'audio') return name;
 return media.music?.title?.trim() || name.replace(/\.[^.]+$/, '') || name;
}
export function musicSubtitle(media) {
 if (media?.type !== 'audio' || !media.music) return '';
 return [media.music.artist || media.music.artists?.join(', '), media.music.album].filter(Boolean).join(' · ');
}
export function musicRows(media) {
 const m = media?.type === 'audio' ? media.music : null;
 if (!m) return [];
 const number = (n, total) => n ? `${n}${total ? ` / ${total}` : ''}` : '';
 return [['Artist', m.artist || m.artists?.join(', ')], ['Album artist', m.album_artist],
 ['Composer', m.composers?.join(', ')], ['Performer', m.performers?.join(', ')], ['Album', m.album], ['Disc subtitle', m.disc_subtitle], ['Compilation', m.compilation ? 'Yes' : ''], ['Track', number(m.track, m.track_total)], ['Disc', number(m.disc, m.disc_total)],
 ['Tempo', audioTempo(m.bpm)], ['Tagged date', m.date !== releaseDate(m) ? m.date : ''], ['Year', showMusicYear(m) ? m.year : ''], ['Genres', m.genres?.join(', ')]];
}

const releaseDate = m => m.release_date || m.date || (m.year ? String(m.year) : '');
function showMusicYear(m) {
 const prefix = releaseDate(m).match(/^[1-9]\d{3}(?:\D|$)/)?.[0].slice(0, 4);
 return m.year && String(m.year) !== prefix;
}
export function musicDetailGroups(media) {
 const m = media?.type === 'audio' ? media.music : null;
 if (!m) return [];
 const roles = {conductor:'Conductor',lyricist:'Lyricist',arranger:'Arranger',producer:'Producer',director:'Director',engineer:'Engineer',mixer:'Mixer',remixer:'Remixer',djmixer:'DJ mixer'};
 return [['Release details', [
  ['Release date', releaseDate(m)], ['Original date', m.original_date], ['Recording date', m.recording_date],
  ['Version', m.release_version], ['Release type', m.release_types?.join('\n')], ['Label', m.labels?.join('\n')],
  ['Catalog number', m.catalog_numbers?.join('\n')], ['Media', m.media?.join('\n')]
 ]], ['Additional music details', [
  ['Mood', m.moods?.join('\n')], ['Grouping', m.grouping], ['Comment', m.comment],
  ...(m.credits || []).map(credit => [Object.hasOwn(roles, credit.role) ? roles[credit.role] : credit.role, credit.names?.join('\n')])
 ]], ['Advanced audio', musicAudioRows(m)]].map(([title, rows]) => [title, rows.filter(([, value]) => value)]).filter(([, rows]) => rows.length);
}

export const artistRoles = {artists: "Artists (track and album)", track_artist: "Track artist", album_artist: "Album artist", composer: "Composer", performer: "Performer"};

// Headings describe discs; song indices always refer to the unchanged songs array.
export function albumDiscLayout(album) {
 const songs=album.songs || [], discs=album.discs || [];
 const grouped=discs.length>1 || discs.some(d=>d.number>1 || d.subtitle) || songs.some(s=>(s.music?.disc_total || 0)>1);
 const multi=discs.length>1 || discs.some(d=>d.number>1) || songs.some(s=>(s.music?.disc_total || 0)>1);
 return {grouped,multi,heads:new Map(discs.filter(d=>d.song_ids?.length).map(d=>[d.song_ids[0],d]))};
}
export function albumSongNumber(song, multi) {
 const m=song.music || {};
 return `${multi ? `${m.disc || '—'}-` : ''}${m.track || '—'}`;
}
export function discTitle(disc) {
 return `${disc.number ? `Disc ${disc.number}` : 'Unnumbered disc'}${disc.subtitle ? ` — ${disc.subtitle}` : ''}`;
}

const audioValue = value => typeof value === 'number' && Number.isFinite(value);
const audioTempo = value => audioValue(value) && value > 0 ? `${value} BPM (beats/min)` : '';
export function musicAudioRows(m) {
 const gain = value => audioValue(value) ? `${value} dB` : '';
 const peak = value => audioValue(value) && value >= 0 ? `${value} (1 = full scale)` : '';
 return [
  ['Sample rate', Number.isInteger(m.sample_rate) && m.sample_rate > 0 ? `${m.sample_rate / 1000} kHz (${m.sample_rate} Hz)` : ''],
  ['Bit depth', Number.isInteger(m.bit_depth) && m.bit_depth > 0 ? `${m.bit_depth} bits` : ''],
  ['Channels', Number.isInteger(m.channels) && m.channels > 0 ? `${m.channels}${m.channels === 1 ? ' (mono)' : m.channels === 2 ? ' (stereo)' : ''}` : ''],
  ['Track ReplayGain', gain(m.replaygain_track_gain_db)], ['Album ReplayGain', gain(m.replaygain_album_gain_db)],
  ['Track peak', peak(m.replaygain_track_peak)], ['Album peak', peak(m.replaygain_album_peak)]
 ].filter(([, value]) => value !== '');
}
