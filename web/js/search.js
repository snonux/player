let _onChange = () => {};
let _debounceTimer = null;

export function initSearch({ onChange, input, clearBtn }) {
  _onChange = onChange;
  if (!input) return;
  input.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
      input.value = '';
      input.blur();
      trigger('');
    }
  });
  input.addEventListener('input', () => {
    clearTimeout(_debounceTimer);
    _debounceTimer = setTimeout(() => trigger(input.value.trim()), 300);
  });
  if (clearBtn) {
    clearBtn.addEventListener('click', () => {
      input.value = '';
      input.focus();
      trigger('');
    });
  }
}

export function focusSearch() {
  const input = document.getElementById('search-input');
  if (input) input.focus();
}

export function trigger(query) {
  clearTimeout(_debounceTimer);
  _onChange(query);
}
