import { test, expect } from 'bun:test';
import { generateKeyPairSync, createHash, verify } from 'node:crypto';
import { signedManifest } from '../package-update.mjs';

test('updates bind the final archive to the embedded Ed25519 public key', () => {
  const {privateKey,publicKey}=generateKeyPairSync('ed25519');
  const pub=publicKey.export({format:'der',type:'spki'}).subarray(-32);
  const seed=privateKey.export({format:'der',type:'pkcs8'}).subarray(-32);
  const secret=Buffer.concat([seed,pub]).toString('base64');
  const bytes=Buffer.from('final signed App archive');
  const m=signedManifest(bytes,'0.3.1','https://example.com/releases',secret,pub.toString('base64'));
  expect(verify(null,createHash('sha256').update(bytes).digest(),publicKey,Buffer.from(m.signature,'base64'))).toBe(true);
  expect(verify(null,createHash('sha256').update(Buffer.from('tampered')).digest(),publicKey,Buffer.from(m.signature,'base64'))).toBe(false);
  expect(()=>signedManifest(bytes,'0.3.1','https://example.com',secret,Buffer.alloc(32).toString('base64'))).toThrow('does not match');
});
