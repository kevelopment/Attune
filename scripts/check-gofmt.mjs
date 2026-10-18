import { execFileSync } from 'node:child_process'
import { existsSync } from 'node:fs'

const files = execFileSync(
  'git',
  ['ls-files', '--cached', '--others', '--exclude-standard', '-z', '--', '*.go'],
  { encoding: 'utf8' },
)
  .split('\0')
  .filter((file) => file && existsSync(file))

if (files.length === 0) process.exit(0)

const unformatted = execFileSync('gofmt', ['-l', ...files], { encoding: 'utf8' }).trim()
if (unformatted) {
  console.error(`Run gofmt on:\n${unformatted}`)
  process.exit(1)
}
