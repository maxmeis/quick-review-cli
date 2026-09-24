import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';

// Exercise the actual gate against real Go coverage, including rejection.
test('coverage gate accepts full coverage and rejects one uncovered branch', () => {
  const cwd = mkdtempSync(join(tmpdir(), 'review-coverage-'));
  try {
    writeFileSync(join(cwd, 'go.mod'), 'module coveragefixture\n\ngo 1.26.5\n');
    writeFileSync(join(cwd, 'fixture.go'), 'package fixture\nfunc Choose(b bool) int { if b { return 1 }; return 2 }\n');
    const run = () => spawnSync('make', ['-f', fileURLToPath(new URL('../Makefile', import.meta.url)), 'coverage'], { cwd, encoding: 'utf8' });
    writeFileSync(join(cwd, 'fixture_test.go'), 'package fixture\nimport "testing"\nfunc TestChoose(t *testing.T) { if Choose(true) != 1 { t.Fatal("bad") } }\n');
    const incomplete = run();
    assert.notEqual(incomplete.status, 0);
    assert.match(incomplete.stdout, /uncovered statement block/);
    writeFileSync(join(cwd, 'fixture_test.go'), 'package fixture\nimport "testing"\nfunc TestChoose(t *testing.T) { if Choose(true) != 1 || Choose(false) != 2 { t.Fatal("bad") } }\n');
    const complete = run();
    assert.equal(complete.status, 0, complete.stdout + complete.stderr);
    assert.match(complete.stdout, /100\.0%/);
  } finally {
    rmSync(cwd, { recursive: true, force: true });
  }
});
