import {defineConfig} from 'vite';
import {fileURLToPath} from 'node:url';
import {dirname,resolve} from 'node:path';
const root=dirname(fileURLToPath(import.meta.url));
// Document parsers load only when a user chooses a plan file.
export default defineConfig({build:{emptyOutDir:false,outDir:resolve(root,'../static'),lib:{entry:resolve(root,'src/release-plan-import.mjs'),formats:['es'],fileName:()=> 'release_plan_import.js'},rolldownOptions:{output:{codeSplitting:false}}}});
