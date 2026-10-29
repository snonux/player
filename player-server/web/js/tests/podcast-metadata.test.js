import assert from 'node:assert/strict';
import { publicationDate, showNotesText, showNotesHTML, episodeArtworkHTML, bindEpisodeArtwork, bindPodcastMetadata } from '../podcast-metadata.js';
import { episodeCardHtml } from '../podcast-views.js';
import { episodeSetFolderCardHtml } from '../podcasts.js';
import { mediaTitle } from '../music.js';
assert.match(publicationDate('2026-10-28T12:00:00Z'),/^Published /);
for(const value of [null,0,'bad'])assert.equal(publicationDate(value),'Publication date unavailable');
const raw='<p>Intro &amp; notes</p><p><a href="https://example.com/notes?a=1&amp;b=2">Read more</a></p><script>alert(1)</script><img src="x" onerror="alert(2)">';
assert.equal(showNotesText(raw),'Intro & notes\nRead more https://example.com/notes?a=1&b=2');
const notes=showNotesHTML(raw);assert.match(notes,/<summary>Show notes<\/summary>/);assert.match(notes,/href="https:\/\/example.com\/notes\?a=1&amp;b=2"/);assert(!notes.includes('<script'));assert(!notes.includes('<img'));assert(!notes.includes('onerror'));
assert(!showNotesHTML('<a href="javascript:alert(1)">bad</a>').includes('href='));
assert(!showNotesHTML('&lt;img src=x onerror=alert(1)&gt;').includes('<img'));
assert.equal(showNotesHTML(' '),'');assert.equal(showNotesText('&#x110000; &#0;'),'� �');
for(const render of [episodeCardHtml,episodeSetFolderCardHtml]){
 const html=render({id:1,title:'Episode',image_url:'https://example.com/item.jpg',feed_image_url:'https://example.com/feed.jpg',published_at:'2026-10-28',description:raw});
 assert(html.includes('src="https://example.com/item.jpg"'));assert(html.includes('data-feed-artwork="https://example.com/feed.jpg"'));assert(html.includes('Published '));assert(html.includes('Show notes'));
 const fallback=render({title:'Episode',image_url:'javascript:bad',feed_image_url:'https://example.com/feed.jpg'});
 assert(fallback.includes('src="https://example.com/feed.jpg"'));assert(fallback.includes('Publication date unavailable'));assert(!fallback.includes('javascript:'));
}
let fail;const image={dataset:{feedArtwork:'https://example.com/feed.jpg'},hidden:false,addEventListener(name,fn){assert.equal(name,'error');fail=fn;}};
bindEpisodeArtwork({querySelectorAll(){return[image];}});fail();assert.equal(image.src,'https://example.com/feed.jpg');assert.equal(image.hidden,false);fail();assert.equal(image.hidden,true);
const tags={title:'Title Placeholder Episode ID: 128221282'};assert.equal(mediaTitle({type:'audio',file_name:'keep.mp3',music:tags,podcast:{title:'Publisher title'}}),'Publisher title');assert.equal(tags.title,'Title Placeholder Episode ID: 128221282');assert.equal(mediaTitle({type:'audio',file_name:'keep.mp3',music:tags}),tags.title);
console.log('Podcast artwork preference/fallback, dates, safe show notes and canonical media titles passed.');

const embedded=episodeArtworkHTML({title:'Downloaded'}, {thumbnailURL:'/api/media/42/thumbnail'});
assert(embedded.includes('src="/api/media/42/thumbnail"'));
assert(!episodeArtworkHTML({}, {thumbnailURL:'https://evil.example/api/media/42/thumbnail'}).includes('<img'));
const allFallbacks=episodeArtworkHTML({image_url:'https://example.com/item.jpg',feed_image_url:'https://example.com/feed.jpg'}, {thumbnailURL:'/api/media/42/thumbnail'});
assert(allFallbacks.includes('data-thumbnail-artwork="/api/media/42/thumbnail"'));
image.hidden=false;image.dataset.feedArtwork='https://example.com/feed.jpg';image.dataset.thumbnailArtwork='/api/media/42/thumbnail';
fail();assert.equal(image.src,'https://example.com/feed.jpg');fail();assert.equal(image.src,'/api/media/42/thumbnail');fail();assert.equal(image.hidden,true);
const events={};let stopped=0;const notesNode={addEventListener(type,handler){events[type]=handler;}};
bindPodcastMetadata({querySelectorAll(selector){return selector==='.episode-description'?[notesNode]:[];}});
events.click({stopPropagation(){stopped++;}});for(const key of ['Enter',' '])events.keydown({key,stopPropagation(){stopped++;}});assert.equal(stopped,3);
console.log('Embedded same-origin artwork fallback and native notes activation isolation passed.');
