import { readFileSync, writeFileSync } from 'node:fs';

const version = readFileSync(new URL('../version.txt', import.meta.url), 'utf8').trim();
if (!version) throw new Error('version.txt must not be empty');

const path = new URL('../npmreleaser.json', import.meta.url);
const config = JSON.parse(readFileSync(path, 'utf8'));
if (config['package.json'].version !== version) {
  config['package.json'].version = version;
  writeFileSync(path, `${JSON.stringify(config, null, 2)}\n`);
}
