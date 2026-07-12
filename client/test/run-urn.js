// Conformance gate: the client's urn:mlet construction must equal
// the Go core's for the TV-001 media object.
import { urnMlet, urnMletOfBlob } from '../lib/mlet-urn.js';

const data = new TextEncoder().encode('MLP test vector 001: media object A\n');
const want = 'urn:mlet:bdyqbcgucl4thqvg5tqzv66l4q6myvy3j647jojvokzpw2c4ohcctu6y';
const got = urnMlet(data);
if (got !== want) {
  console.error(`urnMlet mismatch:\n got ${got}\nwant ${want}`);
  process.exit(1);
}
const blobGot = await urnMletOfBlob(new Blob([data]));
if (blobGot !== want) {
  console.error(`urnMletOfBlob mismatch: ${blobGot}`);
  process.exit(1);
}
console.log('mlet-urn: TV-001 media address reproduced (direct + streamed)');
