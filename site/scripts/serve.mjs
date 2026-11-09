// Serves the built static site as GitHub Pages does, under /taxcalcs/:
// each route's prerendered index.html, and 404.html for anything missing.
//
//   node serve.mjs DIR
import { createReadStream, existsSync, statSync } from 'node:fs';
import { createServer } from 'node:http';
import { extname, join, normalize, resolve } from 'node:path';

const root = resolve(process.argv[2] ?? 'dist/site/browser');
const port = Number(process.env.PORT ?? 4300);
const prefix = '/taxcalcs';
const types = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript',
  '.mjs': 'text/javascript',
  '.css': 'text/css',
  '.json': 'application/json',
  '.pdf': 'application/pdf',
  '.ico': 'image/x-icon',
  '.svg': 'image/svg+xml',
};

createServer((req, res) => {
  const url = new URL(req.url, 'http://x');
  if (!url.pathname.startsWith(prefix + '/')) {
    res.writeHead(302, { location: prefix + '/' + (url.pathname === '/' || url.pathname === prefix ? '' : url.pathname.slice(1)) });
    res.end();
    return;
  }
  const path = normalize(decodeURIComponent(url.pathname.slice(prefix.length))).replace(/^(\.\.[/\\])+/, '');
  let file = join(root, path);
  if (existsSync(file) && statSync(file).isDirectory()) file = join(file, 'index.html');
  if (!existsSync(file)) {
    // GitHub Pages serves the site's own not-found page.
    const notFound = join(root, '404.html');
    res.writeHead(404, { 'content-type': existsSync(notFound) ? types['.html'] : 'text/plain' });
    if (existsSync(notFound)) createReadStream(notFound).pipe(res);
    else res.end('not found');
    return;
  }
  res.writeHead(200, { 'content-type': types[extname(file)] ?? 'application/octet-stream' });
  createReadStream(file).pipe(res);
}).listen(port, () => console.log(`serving ${root} at http://localhost:${port}${prefix}/`));
