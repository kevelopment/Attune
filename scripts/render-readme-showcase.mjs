import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const imageDir = path.join(repo, 'docs/images')
const captures = [
  ['welcome-screen', 'Welcome screen'],
  ['library-calibre-import', 'Library after Calibre import'],
  ['recently-added', 'Recently added timeline'],
  ['connected-kindle-mock', 'Simulated Kindle reader'],
].map(([name, label]) => {
  const filename = path.join(imageDir, `${name}.png`)
  if (!fs.existsSync(filename)) throw new Error(`Missing screenshot: ${filename}`)
  const data = fs.readFileSync(filename)
  const mime = imageMime(data, filename)
  const { width, height } = imageSize(data, mime)
  return { name, label, filename, data, mime, width, height, uri: `data:${mime};base64,${data.toString('base64')}` }
})

for (const capture of captures) {
  const { name, label, width, height, uri } = capture
  const svg = `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}">
  <defs><clipPath id="clip"><rect width="${width}" height="${height}" rx="40"/></clipPath></defs>
  <title>${xml(label)}</title>
  <image width="${width}" height="${height}" clip-path="url(#clip)" xlink:href="${uri}"/>
</svg>
`
  fs.writeFileSync(path.join(imageDir, `${name}-rounded.svg`), svg)
}

const byName = Object.fromEntries(captures.map(capture => [capture.name, capture]))
const layers = [
  { image: byName['welcome-screen'], x: 660, y: 70, width: 1150, angle: 3 },
  { image: byName['library-calibre-import'], x: 110, y: 510, width: 1390, angle: -4 },
  { image: byName['recently-added'], x: 1280, y: 510, width: 850, angle: 5 },
  { image: byName['connected-kindle-mock'], x: 1240, y: 925, width: 900, angle: -2 },
]

const clips = layers.map(({ image, x, y, width }) => {
  const height = width * image.height / image.width
  return `    <clipPath id="clip-${image.name}" clipPathUnits="userSpaceOnUse"><rect x="${x}" y="${y}" width="${width}" height="${height.toFixed(1)}" rx="22"/></clipPath>`
}).join('\n')

const cards = layers.map(({ image, x, y, width, angle }) => {
  const height = width * image.height / image.width
  const cx = x + width / 2
  const cy = y + height / 2
  return `  <g transform="rotate(${angle} ${cx.toFixed(1)} ${cy.toFixed(1)})">
    <rect x="${x}" y="${y}" width="${width}" height="${height.toFixed(1)}" rx="22" fill="#242529" filter="url(#shadow)"/>
    <image x="${x}" y="${y}" width="${width}" height="${height.toFixed(1)}" xlink:href="${image.uri}" preserveAspectRatio="none" clip-path="url(#clip-${image.name})"/>
    <rect x="${x}" y="${y}" width="${width}" height="${height.toFixed(1)}" rx="22" fill="none" stroke="#ffffff" stroke-opacity=".42" stroke-width="2"/>
  </g>`
}).join('\n')

const showcase = `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="4400" height="3200" viewBox="0 0 2200 1600" role="img" aria-labelledby="title">
  <title id="title">Attune welcome, library, recent additions, and simulated Kindle screens</title>
  <defs>
    <filter id="shadow" x="-20%" y="-20%" width="140%" height="150%"><feDropShadow dx="0" dy="20" stdDeviation="24" flood-color="#111216" flood-opacity=".46"/></filter>
${clips}
  </defs>
${cards}
</svg>
`
fs.writeFileSync(path.join(imageDir, 'showcase.svg'), showcase)
console.log(`Rendered README showcase from ${captures.length} screenshots.`)

function imageMime(data, filename) {
  if (data.length >= 8 && data.subarray(0, 8).equals(Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]))) return 'image/png'
  if (data.length >= 3 && data[0] === 0xff && data[1] === 0xd8 && data[2] === 0xff) return 'image/jpeg'
  throw new Error(`Unsupported image format: ${filename}`)
}

function imageSize(data, mime) {
  if (mime === 'image/png') return { width: data.readUInt32BE(16), height: data.readUInt32BE(20) }
  let offset = 2
  while (offset + 9 < data.length) {
    if (data[offset] !== 0xff) { offset++; continue }
    const marker = data[offset + 1]
    const length = data.readUInt16BE(offset + 2)
    if ([0xc0, 0xc1, 0xc2, 0xc3, 0xc5, 0xc6, 0xc7, 0xc9, 0xca, 0xcb, 0xcd, 0xce, 0xcf].includes(marker)) {
      return { height: data.readUInt16BE(offset + 5), width: data.readUInt16BE(offset + 7) }
    }
    offset += 2 + length
  }
  throw new Error('Could not read JPEG dimensions')
}

function xml(value) {
  return value.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;')
}
