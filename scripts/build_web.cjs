const esbuild = require('esbuild')
const path = require('path')
const fs = require('fs')
// 用法：node scripts/build_web.cjs [输出目录]
// 默认写 build/web-dist；Go embed 通过复制进 internal/webdist 打进二进制。
const webDir = path.resolve(__dirname, '..', 'web')
const distOut = process.argv[2] || process.env.QUICSMB_WEB_DIST_OUT ||
  path.join(__dirname, '..', 'build', 'web-dist')
const embedDir = path.join(__dirname, '..', 'internal', 'webdist')
fs.mkdirSync(distOut, { recursive: true })
fs.mkdirSync(embedDir, { recursive: true })
esbuild.build({
  entryPoints: [path.join(webDir, 'src/app.ts')],
  bundle: true,
  format: 'iife',
  globalName: 'QuicsmbWeb',
  outfile: path.join(distOut, 'quicsmb-web.js'),
  minify: false
}).then(() => {
  fs.copyFileSync(path.join(distOut, 'quicsmb-web.js'), path.join(embedDir, 'quicsmb-web.js'))
  fs.copyFileSync(path.join(webDir, 'index.html'), path.join(embedDir, 'index.html'))
  fs.copyFileSync(path.join(webDir, 'style.css'), path.join(embedDir, 'style.css'))
  console.log('built', distOut)
}).catch(e => { console.error(e); process.exit(1) })
