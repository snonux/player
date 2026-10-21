/**
 * music-fixture.ts — access to the disposable music fixture and the shared
 * evidence log for the music suites (playwright.music.config.ts).
 *
 * The fixture is created by
 * player-android/integration_test/support/music_fixture.py (`up`), which
 * prints a fixture directory. Point PLAYER_MUSIC_FIXTURE at it. Everything a
 * test needs is read from that directory: state.json (origin, media ids,
 * versions, evidence directory), auth.json (random per-fixture passwords)
 * and manifest.json (expected titles, tones and levels of every track).
 *
 * Evidence records use the schema of the Python runners (evidence.py), so
 * one `evidence.py summary <dir>` covers Chrome, Firefox and Android.
 */
import { existsSync, mkdirSync, readFileSync, appendFileSync, writeFileSync } from 'node:fs';
import { join, relative, resolve, isAbsolute } from 'node:path';

export interface Account { username: string; password: string }

export interface ManifestTrack {
  path: string;
  set: string;
  file_name: string;
  title: string;
  tone_hz: number[];
  level_dbfs: number;
  duration_s: number;
  codec: string;
  gapless: { group: string; index: number; samples: number } | null;
}

export interface MusicFixture {
  dir: string;
  origin: string;
  runId: string;
  serverVersion: string;
  buildRevision: string;
  checkout: string;
  evidenceDir: string;
  admin: Account;
  user: Account;
  userSets: string[];
  setIds: Record<string, number>;
  mediaIds: Record<string, number>;
  tracks: ManifestTrack[];
  doublesOrigin: string | null;
}

export type Result = 'pass' | 'fail' | 'gap';
export type Proof = 'real' | 'service-double' | 'none';

/** Repository path of the Python support scripts shared with Android. */
export const SUPPORT_DIR = resolve(__dirname, '../../../../../player-android/integration_test/support');

/** Load the fixture named by PLAYER_MUSIC_FIXTURE; throws a clear error otherwise. */
export function loadFixture(dir = process.env.PLAYER_MUSIC_FIXTURE): MusicFixture {
  if (!dir || !existsSync(join(dir, 'state.json'))) {
    throw new Error('Set PLAYER_MUSIC_FIXTURE to the directory printed by music_fixture.py up');
  }
  const state = JSON.parse(readFileSync(join(dir, 'state.json'), 'utf8'));
  const auth = JSON.parse(readFileSync(join(dir, 'auth.json'), 'utf8'));
  const manifest = JSON.parse(readFileSync(state.manifest_file, 'utf8'));
  return {
    dir,
    origin: state.origin,
    runId: state.run_id,
    serverVersion: state.server.version,
    buildRevision: state.server.build_revision,
    checkout: state.server.checkout,
    evidenceDir: state.evidence_dir,
    admin: auth.admin,
    user: auth.user,
    userSets: state.accounts.user_sets,
    setIds: state.set_ids,
    mediaIds: state.media_ids,
    tracks: manifest.tracks,
    doublesOrigin: state.doubles?.origin ?? null,
  };
}

/** Manifest entry for a library path such as "Music/Gain Check/Loud.flac". */
export function track(fixture: MusicFixture, path: string): ManifestTrack & { id: number } {
  const entry = fixture.tracks.find((t) => t.path === path);
  const id = fixture.mediaIds[path];
  if (!entry || !id) throw new Error(`Fixture has no track ${path}`);
  return { ...entry, id };
}

/** Append-only evidence log of one platform (chrome or firefox) of one run. */
export class Evidence {
  readonly dir: string;

  constructor(readonly root: string, readonly platform: string, versions: Record<string, unknown>) {
    this.dir = join(root, platform);
    mkdirSync(this.dir, { recursive: true });
    writeFileSync(join(this.dir, 'versions.json'), JSON.stringify(versions, null, 1));
  }

  /** Absolute path for a new artifact (screenshot, WAV, JSON dump). */
  path(name: string): string {
    return join(this.dir, name);
  }

  record(entry: {
    feature: string; scenario: string; result: Result; detail?: string;
    data?: Record<string, unknown>; artifacts?: string[]; proof?: Proof;
  }): void {
    const line = {
      schema: 1,
      time: new Date().toISOString().replace(/\.\d+Z$/, '+00:00'),
      platform: this.platform,
      feature: entry.feature,
      scenario: entry.scenario,
      result: entry.result,
      proof: entry.proof ?? 'real',
      detail: entry.detail ?? '',
      data: entry.data ?? {},
      artifacts: (entry.artifacts ?? []).map((a) => (isAbsolute(a) ? relative(this.root, a) : a)),
    };
    appendFileSync(join(this.dir, 'evidence.jsonl'), JSON.stringify(line) + '\n');
  }
}
