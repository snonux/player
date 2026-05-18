#!/usr/bin/env node
/**
 * mock-rss-server.js — minimal Node.js HTTP server that serves a valid podcast
 * RSS 2.0 feed at http://localhost:8888/feed.xml.
 *
 * Used by scenario S02 to provide a controllable RSS endpoint so the e2e tests
 * do not depend on external network access or a live podcast feed.
 *
 * Usage:
 *   node mock-rss-server.js          # listens on port 8888 (default)
 *   PORT=9999 node mock-rss-server.js
 *
 * The server serves:
 *   GET /feed.xml  — podcast RSS feed with two episodes
 *   GET /episode1.mp3  — tiny fake audio blob (not real audio, just non-empty)
 *   GET /episode2.mp3  — tiny fake audio blob (not real audio, just non-empty)
 *
 * All other paths return 404.
 */

"use strict";

const http = require("http");

const PORT = parseInt(process.env.PORT || "8888", 10);
const BASE_URL = `http://localhost:${PORT}`;

// A minimal valid RSS 2.0 podcast feed with two episodes.
// Duration values use HH:MM:SS format as accepted by most podcast parsers.
function buildFeed() {
  return `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"
     xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"
     xmlns:content="http://purl.org/rss/modules/content/">
  <channel>
    <title>Test Podcast — E2E Fixture</title>
    <link>${BASE_URL}</link>
    <description>A mock podcast feed used by the Player e2e-llm test suite.</description>
    <language>en-us</language>
    <itunes:author>Player E2E Test Harness</itunes:author>
    <itunes:image href="${BASE_URL}/cover.jpg"/>
    <itunes:explicit>false</itunes:explicit>

    <item>
      <title>Episode 1 — Introduction</title>
      <link>${BASE_URL}/episode1.mp3</link>
      <guid isPermaLink="false">e2e-fixture-episode-1</guid>
      <description>First episode of the mock podcast.</description>
      <pubDate>Mon, 01 Jan 2024 10:00:00 +0000</pubDate>
      <enclosure url="${BASE_URL}/episode1.mp3" length="1024" type="audio/mpeg"/>
      <itunes:duration>00:01:30</itunes:duration>
      <itunes:explicit>false</itunes:explicit>
    </item>

    <item>
      <title>Episode 2 — The Follow-Up</title>
      <link>${BASE_URL}/episode2.mp3</link>
      <guid isPermaLink="false">e2e-fixture-episode-2</guid>
      <description>Second episode of the mock podcast.</description>
      <pubDate>Tue, 02 Jan 2024 10:00:00 +0000</pubDate>
      <enclosure url="${BASE_URL}/episode2.mp3" length="2048" type="audio/mpeg"/>
      <itunes:duration>00:03:00</itunes:duration>
      <itunes:explicit>false</itunes:explicit>
    </item>
  </channel>
</rss>`;
}

// A tiny non-empty blob returned for episode download requests.
// The Player server probes the file with ffprobe after download; since this is
// not real audio the probe will fail, but the download step itself will succeed
// (HTTP 200 with Content-Type audio/mpeg). The scenario only asserts the HTTP
// status of the download trigger, not the resulting ffprobe output.
const FAKE_AUDIO = Buffer.from(
  "ID3\x03\x00\x00\x00\x00\x00\x00" + "fake-mp3-content-for-e2e-testing",
  "ascii",
);

// Route the incoming request to the appropriate response.
function handleRequest(req, res) {
  const url = req.url.split("?")[0]; // strip query string

  if (url === "/feed.xml") {
    // Serve the RSS feed with appropriate headers.
    const body = buildFeed();
    res.writeHead(200, {
      "Content-Type": "application/rss+xml; charset=utf-8",
      "Content-Length": Buffer.byteLength(body, "utf8"),
      "Cache-Control": "no-cache",
    });
    res.end(body);
    return;
  }

  if (url === "/episode1.mp3" || url === "/episode2.mp3") {
    // Serve a fake audio blob so the Player download endpoint can retrieve it.
    res.writeHead(200, {
      "Content-Type": "audio/mpeg",
      "Content-Length": FAKE_AUDIO.length,
      "Accept-Ranges": "bytes",
    });
    res.end(FAKE_AUDIO);
    return;
  }

  if (url === "/cover.jpg") {
    // Serve a 1×1 white JPEG as a minimal cover image.
    const TINY_JPEG = Buffer.from(
      "/9j/4AAQSkZJRgABAQAAAQABAAD/2wBDAAgGBgcGBQgHBwcJCQgKDBQNDAsLDBkSEw8U" +
        "HRofHh0aHBwgJC4nICIsIxwcKDcpLDAxNDQ0Hyc5PTgyPC4zNDL/2wBDAQkJCQwLDBgN" +
        "DRgyIRwhMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIy" +
        "MjL/wAARCAABAAEDASIAAhEBAxEB/8QAFAABAAAAAAAAAAAAAAAAAAAACf/EABQQAQAAAAAA" +
        "AAAAAAAAAAAAAP/EABQBAQAAAAAAAAAAAAAAAAAAAAD/xAAUEQEAAAAAAAAAAAAAAAAAAAAA" +
        "/9oADAMBAAIRAxEAPwCwABmX/9k=",
      "base64",
    );
    res.writeHead(200, {
      "Content-Type": "image/jpeg",
      "Content-Length": TINY_JPEG.length,
    });
    res.end(TINY_JPEG);
    return;
  }

  // Any other path → 404.
  res.writeHead(404, { "Content-Type": "text/plain" });
  res.end("not found\n");
}

// Start the server and print a ready message so the harness can wait for it.
const server = http.createServer(handleRequest);

server.listen(PORT, "127.0.0.1", () => {
  console.log(`mock-rss-server: listening on ${BASE_URL}`);
  console.log(`mock-rss-server: feed available at ${BASE_URL}/feed.xml`);
});

// Graceful shutdown on SIGINT / SIGTERM so the harness can stop the server
// cleanly after the scenario completes.
function shutdown(signal) {
  console.log(`mock-rss-server: received ${signal}, shutting down`);
  server.close(() => {
    console.log("mock-rss-server: closed");
    process.exit(0);
  });
}

process.on("SIGINT", () => shutdown("SIGINT"));
process.on("SIGTERM", () => shutdown("SIGTERM"));
