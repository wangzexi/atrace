import { createHash, createPrivateKey, createPublicKey, sign } from 'node:crypto';
import { gzipSync } from 'node:zlib';
import { readFileSync, writeFileSync } from 'node:fs';
import { dirname, basename, join } from 'node:path';

export function signedManifest(archive, version, base, encodedKey, publicKey) {
  const raw = Buffer.from(encodedKey.trim(), 'base64');
  if (raw.length !== 64) throw new Error('Invalid update signing key');
  const key = createPrivateKey({ key: Buffer.concat([Buffer.from('302e020100300506032b657004220420', 'hex'), raw.subarray(0, 32)]), format: 'der', type: 'pkcs8' });
  const derived = createPublicKey(key).export({format:'der',type:'spki'}).subarray(-32).toString('base64');
  if (derived !== publicKey.trim()) throw new Error('Signing key does not match the App public key');
  if (!/^\d+\.\d+\.\d+$/.test(version)) throw new Error('Invalid App version');
  return { version, notes: 'Atrace 更新', date: new Date().toISOString(), url: `${base}/atrace-${version}-darwin-arm64-${createHash('sha256').update(archive).digest('hex').slice(0,12)}.tar.gz`, size: archive.length,
    signature: sign(null, createHash('sha256').update(archive).digest(), key).toString('base64') };
}

if (import.meta.main) {
  const app = process.argv[2];
  const config = JSON.parse(readFileSync('mygo.json', 'utf8'));
  const encodedKey = process.env.MYGO_UPDATER_PRIVATE_KEY;
  if (!encodedKey) { console.log('Local App built without publishing an update'); process.exit(0); }
  // Archive only after the final macOS bundle signature is sealed. Preserve modes
  // and symlinks so the updater installs the exact verified App, including Bun.
  const tar = Bun.spawnSync(['tar', '-cf', '-', basename(app)], { cwd: dirname(app), env: {...process.env, COPYFILE_DISABLE:'1'}, stdout:'pipe', stderr:'pipe' });
  if (tar.exitCode !== 0) throw new Error('App archive failed');
  const archive = gzipSync(tar.stdout, {level:1});
  const manifest = signedManifest(archive, config.version, config.updates.url, encodedKey, config.updates.publicKey);
  writeFileSync(join(dirname(app), new URL(manifest.url).pathname.split('/').pop()), archive);
  writeFileSync(join(dirname(app), 'update-darwin-arm64.json'), JSON.stringify(manifest, null, 2)+'\n');
  console.log(`Signed update ${config.version}`);
}
