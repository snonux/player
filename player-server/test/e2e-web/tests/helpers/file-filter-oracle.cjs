// Pure acceptance predicate, shared by the actual browser runner and Node checks.
function completedFilterResult(state, expected, freshResponse) {
  if (!freshResponse || state.loading || state.error) return false;
  const actual = [...state.ids].sort((a, b) => a - b);
  const wanted = [...expected].sort((a, b) => a - b);
  if (JSON.stringify(actual) !== JSON.stringify(wanted)) return false;
  return wanted.length > 0 || (state.emptyText === 'No results.' && state.resultCount === '0 items');
}
module.exports = { completedFilterResult };
