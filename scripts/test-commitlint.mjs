import assert from 'node:assert/strict';
import { cpSync, mkdtempSync, mkdirSync, rmSync, symlinkSync, writeFileSync } from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import test from 'node:test';
import { fileURLToPath } from 'node:url';

const repoRoot = fileURLToPath(new URL('../', import.meta.url));

function command(cmd, args, cwd, options = {}) {
  return spawnSync(cmd, args, { cwd, encoding: 'utf8', ...options });
}

function makeFixture(t) {
  const root = mkdtempSync(path.join(os.tmpdir(), 'quick-review-hooks-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));

  const repo = path.join(root, 'repo');
  mkdirSync(repo);
  command('git', ['init', '-q'], repo);
  command('git', ['config', 'user.name', 'Hook Test'], repo);
  command('git', ['config', 'user.email', 'hooks@example.invalid'], repo);
  command('git', ['config', 'commit.gpgsign', 'false'], repo);
  mkdirSync(path.join(repo, '.husky'));
  for (const hook of ['commit-msg', 'pre-push']) {
    cpSync(path.join(repoRoot, '.husky', hook), path.join(repo, '.husky', hook));
  }
  const installed = command(process.execPath, [path.join(repoRoot, 'node_modules/husky/bin.js')], repo, { env: hookEnv() });
  assert.equal(installed.status, 0, installed.stderr);
  symlinkSync(path.join(repoRoot, 'node_modules'), path.join(repo, 'node_modules'), 'dir');
  cpSync(path.join(repoRoot, 'commitlint.config.cjs'), path.join(repo, 'commitlint.config.cjs'));
  writeFileSync(path.join(repo, 'tracked.txt'), 'fixture\n');
  command('git', ['add', 'tracked.txt'], repo);
  return { root, repo };
}

function hookEnv(extra = {}) {
  return { ...process.env, HUSKY: '1', ...extra };
}

function git(repo, args, options = {}) {
  return command('git', args, repo, { env: hookEnv(), ...options });
}

function lint(message) {
  return spawnSync(
    'pnpm',
    ['exec', 'commitlint'],
    { cwd: repoRoot, encoding: 'utf8', input: message },
  );
}

test('accepts Conventional Commit types and breaking changes', () => {
  for (const message of [
    'feat: add JSON output',
    'fix(parser): handle empty input',
    'chore: update Go toolchain',
    'feat!: remove the legacy output format',
    'feat: replace output format\n\nBREAKING CHANGE: consumers must select a format',
    'revert: feat: add JSON output',
  ]) {
    const result = lint(message);
    assert.equal(result.status, 0, `${message}\n${result.stdout}\n${result.stderr}`);
  }
});

test('rejects malformed or unsupported commit messages', () => {
  for (const message of [
    'Add JSON output',
    'feature: add JSON output',
    'feat add JSON output',
    ': missing type',
  ]) {
    const result = lint(message);
    assert.notEqual(result.status, 0, `unexpectedly accepted: ${message}`);
  }
});

test('Git commit hook rejects invalid messages and accepts Conventional Commits', (t) => {
  const { repo } = makeFixture(t);

  const rejected = git(repo, ['commit', '-m', 'Add fixture file']);
  assert.notEqual(rejected.status, 0, `${rejected.stdout}\n${rejected.stderr}`);
  assert.match(`${rejected.stdout}\n${rejected.stderr}`, /subject may not be empty|type may not be empty/i);

  const accepted = git(repo, ['commit', '-m', 'feat: add fixture file']);
  assert.equal(accepted.status, 0, `${accepted.stdout}\n${accepted.stderr}`);
});

test('pre-push hook blocks pushes when make verify fails', (t) => {
  const { root, repo } = makeFixture(t);
  const remote = path.join(root, 'remote.git');
  command('git', ['init', '--bare', '-q', remote], root);
  git(repo, ['commit', '-m', 'feat: add fixture file']);
  git(repo, ['remote', 'add', 'origin', remote]);

  const fakeBin = path.join(root, 'fake-bin');
  mkdirSync(fakeBin);
  const fakeMake = path.join(fakeBin, 'make');
  writeFileSync(fakeMake, '#!/bin/sh\nprintf "fake make received: %s\\n" "$*"\nexit 1\n', { mode: 0o755 });

  const result = git(repo, ['push', '-u', 'origin', 'HEAD'], {
    env: hookEnv({ PATH: `${fakeBin}:${process.env.PATH}` }),
  });
  assert.notEqual(result.status, 0, `${result.stdout}\n${result.stderr}`);
  assert.match(`${result.stdout}\n${result.stderr}`, /fake make received: verify/);
});

test('pre-push hook permits a push when make verify succeeds', (t) => {
  const { root, repo } = makeFixture(t);
  const remote = path.join(root, 'remote.git');
  command('git', ['init', '--bare', '-q', remote], root);
  git(repo, ['commit', '-m', 'feat: add fixture file']);
  git(repo, ['remote', 'add', 'origin', remote]);

  const fakeBin = path.join(root, 'fake-bin');
  mkdirSync(fakeBin);
  const fakeMake = path.join(fakeBin, 'make');
  writeFileSync(fakeMake, '#!/bin/sh\nprintf "fake make received: %s\\n" "$*"\nexit 0\n', { mode: 0o755 });

  const result = git(repo, ['push', '-u', 'origin', 'HEAD'], {
    env: hookEnv({ PATH: `${fakeBin}:${process.env.PATH}` }),
  });
  assert.equal(result.status, 0, `${result.stdout}\n${result.stderr}`);
  assert.match(`${result.stdout}\n${result.stderr}`, /fake make received: verify/);
});
