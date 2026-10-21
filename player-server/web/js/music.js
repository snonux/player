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
 ['Album', m.album], ['Track', number(m.track, m.track_total)], ['Disc', number(m.disc, m.disc_total)],
 ['Date', m.date], ['Year', m.year || ''], ['Genres', m.genres?.join(', ')]];
}
