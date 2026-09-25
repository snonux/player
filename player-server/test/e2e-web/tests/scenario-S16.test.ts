import { test, expect, admin } from './helpers/scenario-fixture';
test('S16: real pagination, tolerant parsing, search, filters and sort order', async () => {
  const { api, media } = await admin();
  const page1 = await api('GET', '/media?limit=2'); const page2 = await api('GET', '/media?limit=2&offset=2');
  expect(page1).toHaveLength(2); expect(page2).toHaveLength(2);
  expect(new Set([...page1, ...page2].map(m => m.id)).size).toBe(4);
  for (const query of ['limit=1000000', 'limit=-5', 'limit=foo', 'limit=0', 'offset=-1', 'offset=foo']) expect(await api('GET', `/media?${query}`)).toEqual(media);
  const search = media[0].file_name.slice(0, 4);
  const found = await api('GET', `/media?search=${encodeURIComponent(search)}`); expect(found.length).toBeGreaterThan(0);
  for (const item of found) expect(`${item.file_name} ${item.rel_path}`.toLowerCase()).toContain(search.toLowerCase());
  const audio = await api('GET', '/media?type=audio'); expect(audio.length).toBeGreaterThan(0); expect(audio.every((m: any) => m.type === 'audio')).toBe(true);
  const item = media[0]; const favorites = await api('GET', '/media?favorites=true');
  await api('POST', `/media/${item.id}/favorite`);
  const added = await api('GET', '/media?favorites=true'); expect(added).toHaveLength(favorites.length + 1); expect(added.some((m: any) => m.id === item.id)).toBe(true);
  // Combined favorites + search + set: the favorites JOIN argument once bound to the search pattern and returned nothing.
  const combined = await api('GET', `/media?favorites=true&set_id=${item.set_id}&search=${encodeURIComponent(item.file_name)}`);
  expect(combined.map((m: any) => m.id)).toEqual([item.id]);
  const other = media.find((m: any) => m.id !== item.id && !`${m.file_name} ${m.rel_path}`.includes(item.file_name));
  expect(await api('GET', `/media?favorites=true&search=${encodeURIComponent(other.file_name)}`)).toEqual([]);
  const sizeHit = await api('GET', `/media?filesize_min=${item.file_size_bytes}&filesize_max=${item.file_size_bytes}`);
  expect(sizeHit.length).toBeGreaterThan(0); expect(sizeHit.every((m: any) => m.file_size_bytes === item.file_size_bytes)).toBe(true);
  expect(await api('GET', `/media?filesize_min=${Math.max(...media.map((m: any) => m.file_size_bytes)) + 1}`)).toEqual([]);
  await api('POST', `/media/${item.id}/favorite`); expect(await api('GET', '/media?favorites=true')).toEqual(favorites);
  const setMedia = await api('GET', `/media?set_id=${item.set_id}`); expect(setMedia.length).toBeGreaterThan(0); expect(setMedia.every((m: any) => m.set_id === item.set_id)).toBe(true);
  for (const [sort, field, desc] of [['duration', 'duration', false], ['date', 'created_at', true], ['name', 'file_name', false]] as const) {
    const sorted = await api('GET', `/media?sort=${sort}`);
    for (let i = 1; i < sorted.length; i++) expect(desc ? sorted[i-1][field] >= sorted[i][field] : sorted[i-1][field] <= sorted[i][field], sort).toBe(true);
  }
});
