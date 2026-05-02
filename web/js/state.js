export const state = {
  user: null,
  sets: [],
  selectedSetId: null,
  selectedSetIds: [], // multi-selection
  media: [],
  filters: { type: '', search: '', favorites: false, tags: '', sort: 'name', minDuration: '', maxDuration: '' },
  isAdmin: false,
  folderPath: '',   // current subfolder path within the selected set
};

export function getState() { return state; }
export function setMedia(list) { state.media = list; }
