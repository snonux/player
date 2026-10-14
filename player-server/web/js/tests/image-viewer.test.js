import { initImageViewer, zoomIn, zoomOut, resetZoom, panImage, toggleCrop, clearCropMode } from '../imageViewer.js';

const failures = [];
function assert(condition, message) { if (!condition) failures.push(message); }
const image = { style: {}, addEventListener() {} };
const player = { classList: { toggle() {}, remove() {} }, style: { setProperty() {} } };
globalThis.window = { addEventListener() {} };
globalThis.document = { fullscreenElement: null };
let imageMode = true;
initImageViewer({ els: () => ({ image, player }), isImageMode: () => imageMode });
resetZoom();
assert(!panImage(40, 40), 'Fitted images must not pan');
zoomIn();
assert(panImage(40, 40), 'Zoomed images must pan');
assert(image.style.transform === 'translate(-40px, -40px) scale(1.25)', 'Pan directions must move the viewport down and right');
resetZoom();
assert(image.style.transform === 'translate(0px, 0px) scale(1)', '0 reset must fit and center the image');
zoomOut();
assert(!panImage(40, 40), 'Below-fit images must not pan');
resetZoom(); zoomIn();
imageMode = false;
assert(!panImage(40, 40), 'Audio/video modes must not pan images');
imageMode = true;
document.fullscreenElement = player;
toggleCrop();
assert(!panImage(40, 40), 'Display crop must retain ownership over zoomed-image pan');
clearCropMode();
assert(panImage(-40, -40), 'Pan resumes after crop is cleared');
resetZoom();
if (failures.length) { failures.forEach(message => console.error(message)); process.exit(1); }
console.log('All image viewer tests passed.');
