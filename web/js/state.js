export const state = {
  user: null,
  sets: [],
  selectedSetId: null,
  selectedSetIds: [], // multi-selection
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
