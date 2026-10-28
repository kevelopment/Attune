import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawn, execFileSync } from 'node:child_process'
import net from 'node:net'
import { setTimeout as pause } from 'node:timers/promises'

const root = fs.realpathSync(path.join(path.dirname(fileURLToPath(import.meta.url)), '..'))
const state = path.join(root, '.cache/dev')
const pidFile = path.join(state, 'wails.pid')
const logFile = path.join(state, 'wails.log')
const [action, binary] = process.argv.slice(2)
fs.mkdirSync(state, { recursive: true })

function currentPID() {
  if (!fs.existsSync(pidFile)) return null
  const value = fs.readFileSync(pidFile, 'utf8').trim()
  if (!/^[1-9][0-9]*$/.test(value)) throw new Error('Invalid dev PID; refusing to signal it')
  const pid = Number(value)
  let command
  try { command = execFileSync('ps', ['-p', value, '-o', 'command='], { encoding: 'utf8' }).trim() }
  catch { fs.rmSync(pidFile, { force: true }); return null }
  if (command.includes('<defunct>')) { fs.rmSync(pidFile, { force: true }); return null }
  if (!command.startsWith(`${binary} dev `)) throw new Error('Recorded PID belongs to another command; refusing to signal it')
  const cwd = execFileSync('lsof', ['-a', '-p', value, '-d', 'cwd', '-Fn'], { encoding: 'utf8' })
  if (!cwd.split('\n').includes(`n${root}`)) throw new Error('Recorded PID belongs to another checkout')
  return pid
}

async function stop() {
  const pid = currentPID()
  if (!pid) { console.log('Attune development is stopped'); return }
  process.kill(pid, 'SIGTERM')
  for (let i = 0; i < 100; i++) {
    let running = false
    try { running = execFileSync('ps', ['-p', String(pid), '-o', 'command='], { encoding: 'utf8' }).trim().startsWith(`${binary} dev `) } catch {}
    if (!running) {
      fs.rmSync(pidFile, { force: true })
      console.log('Stopped Attune development')
      return
    }
    await pause(100)
  }
  throw new Error(`Development is still stopping; see ${logFile}`)
}

async function freePort(port) {
  await new Promise((resolve, reject) => {
    const probe = net.createServer()
    probe.once('error', () => reject(new Error(`Port ${port} is in use; stop its owner or choose ATTUNE_DEV_PORT for Vite`)))
    probe.listen(port, '127.0.0.1', () => probe.close(resolve))
  })
}

async function ready(url) {
  try { return (await fetch(url, { signal: AbortSignal.timeout(1000) })).ok }
  catch { return false }
}

async function start() {
  const existing = currentPID()
  if (existing) { console.log(`Attune development already running (PID ${existing}). Logs: ${logFile}`); return }
  const port = Number(process.env.ATTUNE_DEV_PORT)
  if (!Number.isInteger(port) || port < 1024 || port > 65535) throw new Error('ATTUNE_DEV_PORT must be between 1024 and 65535')
  await freePort(port)
  const log = fs.openSync(logFile, 'w', 0o600)
  const wailsArgs = ['dev', '-config', './build/config.yml', '-port', String(port)]
  const child = spawn(binary, wailsArgs, {
    cwd: root, detached: true, stdio: ['ignore', log, log], env: { ...process.env, EXTRA_TAGS: process.platform === 'linux' ? 'gtk3' : process.env.EXTRA_TAGS },
  })
  await new Promise((resolve, reject) => { child.once('spawn', resolve); child.once('error', reject) })
  child.unref()
  fs.closeSync(log)
  fs.writeFileSync(pidFile, `${child.pid}\n`, { mode: 0o600 })
  console.log(`Starting Attune; first compilation may take a moment. Logs: ${logFile}`)
  for (let i = 0; i < 240; i++) {
    if (!currentPID()) throw new Error(`Wails exited during startup; see ${logFile}`)
    if (await ready(`http://127.0.0.1:${port}/`)) {
      console.log(`Attune development frontend ready. React/CSS hot reload: ${port}`)
      console.log(`Development library: ${process.env.EBOOK_LIBRARY_HOME}`)
      return
    }
    await pause(500)
  }
  await stop()
  throw new Error(`Startup timed out; see ${logFile}`)
}

try {
  if (action === 'start') await start()
  else if (action === 'stop') await stop()
  else if (action === 'status') {
    const pid = currentPID()
    if (!pid) process.exitCode = 1
    else console.log(`Attune development already running (PID ${pid}). Logs: ${logFile}`)
  } else throw new Error('Expected start, stop, or status')
} catch (error) {
  console.error(error.message)
  process.exitCode = action === 'status' ? 2 : 1
}
