import { execFileSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { gzipSync } from 'node:zlib';

// Homebrew only pours prebuilt bottles without a compiler; a formula without
// one is a source build, even when `install` just copies a binary. Build a
// bottle per platform from the release archives in dist/, so `task release`
// uploads them alongside the archives.
const dist = new URL('../dist/', import.meta.url).pathname;
const version = readFileSync(new URL('../version.txt', import.meta.url), 'utf8').trim();
if (!version) throw new Error('version.txt must not be empty');

const platforms = [['Darwin', 'arm64'], ['Darwin', 'x86_64'], ['Linux', 'arm64'], ['Linux', 'x86_64']];

// Homebrew's macOS bottle tags. Homebrew pours a bottle built for an older
// macOS on newer ones, so tag each bottle with the binary's minimum macOS.
const macosReleases = {
  27: 'golden_gate', 26: 'tahoe', 15: 'sequoia', 14: 'sonoma', 13: 'ventura', 12: 'monterey', 11: 'big_sur',
};

// Major version of the minimum macOS in a 64-bit Mach-O's LC_BUILD_VERSION.
function minimumMacos(binary) {
  if (binary.readUInt32LE(0) !== 0xfeedfacf) throw new Error('Expected a 64-bit Mach-O binary');
  let offset = 32;
  for (let i = 0; i < binary.readUInt32LE(16); i++) {
    if (binary.readUInt32LE(offset) === 0x32) return binary.readUInt32LE(offset + 12) >>> 16;
    offset += binary.readUInt32LE(offset + 4);
  }
  throw new Error('Mach-O binary has no LC_BUILD_VERSION load command');
}

function bottleTag(os, arch, binary) {
  if (os === 'Linux') return `${arch}_linux`;
  const macos = minimumMacos(binary);
  const release = macosReleases[macos];
  if (!release) throw new Error(`No Homebrew bottle tag for macOS ${macos}`);
  return arch === 'arm64' ? `arm64_${release}` : release;
}

// Minimal ustar archive, so bottles are reproducible on any OS: no extended
// attributes, fixed owner, and the release binary's own mtime.
function tarEntry(name, mode, mtime, data) {
  const header = Buffer.alloc(512);
  const field = (offset, length, value) => header.write(`${value.toString(8).padStart(length - 1, '0')}\0`, offset);
  header.write(name, 0, 100);
  field(100, 8, mode);
  field(108, 8, 0);
  field(116, 8, 0);
  field(124, 12, data ? data.length : 0);
  field(136, 12, mtime);
  header.fill(' ', 148, 156);
  header.write(data ? '0' : '5', 156);
  header.write('ustar\0', 257);
  header.write('00', 263);
  const checksum = header.reduce((sum, byte) => sum + byte, 0);
  header.write(`${checksum.toString(8).padStart(6, '0')}\0 `, 148);
  if (!data) return header;
  return Buffer.concat([header, data, Buffer.alloc((512 - (data.length % 512)) % 512)]);
}

function bottle(binary, mtime) {
  const keg = `clonetree/${version}`;
  const tar = Buffer.concat([
    tarEntry('clonetree/', 0o755, mtime),
    tarEntry(`${keg}/`, 0o755, mtime),
    tarEntry(`${keg}/bin/`, 0o755, mtime),
    tarEntry(`${keg}/bin/clonetree`, 0o755, mtime, binary),
    Buffer.alloc(1024),
  ]);
  return gzipSync(tar, { level: 9 });
}

const work = mkdtempSync(join(tmpdir(), 'clonetree-bottles-'));
try {
  for (const [os, arch] of platforms) {
    const extracted = join(work, `${os}-${arch}`);
    mkdirSync(extracted);
    execFileSync('tar', ['-xzf', join(dist, `clonetree_${os}_${arch}.tar.gz`), '-C', extracted, 'clonetree']);
    const binaryPath = join(extracted, 'clonetree');
    const binary = readFileSync(binaryPath);
    const name = `clonetree-${version}.${bottleTag(os, arch, binary)}.bottle.tar.gz`;
    writeFileSync(join(dist, name), bottle(binary, Math.floor(statSync(binaryPath).mtimeMs / 1000)));
    console.log(`Built dist/${name}`);
  }
} finally {
  rmSync(work, { recursive: true, force: true });
}
