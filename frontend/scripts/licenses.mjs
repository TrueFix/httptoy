import { readFileSync, writeFileSync } from 'node:fs';

// Runtime packages incorporated in the browser bundle, including transitive code.
const packages = ['react', 'react-dom', 'scheduler', 'framer-motion', 'motion-dom', 'motion-utils', 'tslib', 'lucide-react', '@fontsource-variable/big-shoulders', '@fontsource-variable/public-sans'];
const notices = packages.map(name => {
  const directory = new URL(`../node_modules/${name}/`, import.meta.url);
  const pkg = JSON.parse(readFileSync(new URL('package.json', directory), 'utf8'));
  let license;
  for (const file of ['LICENSE', 'LICENSE.md', 'LICENSE.txt']) {
    try { license = readFileSync(new URL(file, directory), 'utf8'); break; } catch (error) { if (error.code !== 'ENOENT') throw error; }
  }
  if (!license) throw new Error(`Missing license for ${name}`);
  return `## ${name} ${pkg.version} — ${pkg.license}\n\n${license.trim()}\n`;
}).join('\n');
const text = `# Frontend third-party notices\n\nPreserve these notices when redistributing embedded frontend assets.\n\n${notices}`;
writeFileSync(new URL('../../FRONTEND_NOTICES.md', import.meta.url), text);
writeFileSync(new URL('../public/assets/licenses.txt', import.meta.url), text);
