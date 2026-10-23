// Uses source ownership captured before preparation, and the actual transport.
export function libraryPresenceSnapshot({media,source,account,transport,pending,reportProgress,remote}) {
  if (!Number.isSafeInteger(account) || account <= 0 || source?.userId !== account || !source.library || !reportProgress || remote || pending || media?.type !== 'audio' || media.live_radio || media.offlinePinURL || !Number.isSafeInteger(media.id) || media.id <= 0 || !transport?.readyState || transport.error || transport.ended) return null;
  const position = transport.currentTime;
  if (!Number.isFinite(position) || position < 0) return null;
  return {media_id:media.id,state:transport.paused ? 'paused' : 'playing',position_seconds:position};
}
