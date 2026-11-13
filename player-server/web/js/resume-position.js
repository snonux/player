import { API } from './api.js';

// savedResumePosition returns where a library item starts when the caller
// gave no explicit position: the account's saved progress, or the start
// when it was finished. The lookup is bounded so a slow server never holds
// playback back; on a timeout or error the given position is kept.
export async function savedResumePosition(media, resumeFrom, timeoutMS = 3000) {
  if (resumeFrom || !(Number(media?.id) > 0)) return resumeFrom;
  let timer;
  const lookup = API.mediaDetail(media.id)
    .then(detail => detail?.progress?.finished ? 0 : (detail?.progress?.position_seconds ?? resumeFrom))
    .catch(() => resumeFrom);
  try {
    return await Promise.race([lookup, new Promise(resolve => { timer = setTimeout(() => resolve(resumeFrom), timeoutMS); })]);
  } finally { clearTimeout(timer); }
}
