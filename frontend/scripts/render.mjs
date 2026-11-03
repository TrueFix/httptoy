import { readFileSync, writeFileSync } from 'node:fs';
import { render } from '../.ssr/entry-server.js';

const { html, faq } = render();
let template = readFileSync(new URL('../../templates/web/index.html', import.meta.url), 'utf8');
if (!template.includes('<!--app-html-->')) throw new Error('Missing pre-render placeholder');
template = template.replace('<!--app-html-->', html).replace('<!--faq-schema-->', `<script type="application/ld+json">${JSON.stringify(faq).replaceAll('<', '\\u003c')}</script>`);
writeFileSync(new URL('../../templates/index.html', import.meta.url), template);
console.log('Pre-rendered React content and API catalog into templates/index.html');
