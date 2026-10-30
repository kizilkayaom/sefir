import { createServer } from 'node:http'
import { readFile } from 'node:fs/promises'
import { resolve, extname, sep } from 'node:path'

const root = resolve('dist')
const contentTypes = {
  '.html': 'text/html',
  '.js': 'text/javascript',
  '.css': 'text/css',
  '.woff2': 'font/woff2',
  '.svg': 'image/svg+xml',
}

createServer(async (request, response) => {
  const pathname = new URL(request.url, 'http://127.0.0.1:4173').pathname
  const path = resolve(root, pathname === '/' ? 'index.html' : pathname.slice(1))
  if (!path.startsWith(root + sep)) {
    response.writeHead(403).end()
    return
  }
  try {
    const content = await readFile(path)
    response.setHeader('Content-Type', contentTypes[extname(path)] ?? 'application/octet-stream')
    response.setHeader('Content-Security-Policy', "default-src 'self'; script-src 'self'; style-src 'self'; font-src 'self'; img-src 'self' data:; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
    response.end(content)
  } catch {
    response.writeHead(404).end()
  }
}).listen(4173, '127.0.0.1')
