export const state = {
  user: null,
  music: null,
  sets: [],
  selectedSetId: null,
  selectedSetIds: [], // multi-selection
  virtualSet: '',
  media: [],
  filters: {
    type: '',
    search: '',
    favorites: false,
    tags: '',
    sort: '',
    minDuration: '',
    maxDuration: '',
    minFileSize: '',
    maxFileSize: '',
  },
  isAdmin: false,
  folderPath: '',   // current subfolder path within the selected set
  mediaPage: 0,
  sharesCurrentRow: -1,
};

export function getState() { return state; }
export function setMedia(list) { state.media = list; }

// Authenticated identity comes only from the server config, never local preferences.
export function setAuthenticatedUser(id) {
  state.user = Number.isSafeInteger(id) && id > 0 ? { id } : null;
}
