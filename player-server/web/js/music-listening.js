import { escapeHtml as esc } from './utils.js';

// Always name the account scope; the global file play_count is unrelated.
export function listeningText(item = {}) {
 const count = Number(item.personal_play_count) || 0;
 const date = item.last_played_at ? new Date(item.last_played_at) : null;
 return `Your plays: ${count} · ${date && !Number.isNaN(date.getTime()) ? `Last played ${date.toLocaleString()}` : 'Not played yet'}`;
}
export function listeningDetails(item) {
 return `<span class="text-muted text-sm">${esc(listeningText(item))}</span>`;
}
