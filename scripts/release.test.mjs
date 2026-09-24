import { test } from 'node:test';
import assert from 'node:assert/strict';
import { analyzeCommits } from '@semantic-release/commit-analyzer';
import { generateNotes } from '@semantic-release/release-notes-generator';
import config from '../release.config.cjs';

const logger = { log() {} };
for (const [message, expected] of [
  ['fix: preserve report during failed writes', 'patch'],
  ['feat(report): render diagrams', 'minor'],
  ['feat!: replace session format', 'major'],
  ['fix: update storage\n\nBREAKING CHANGE: old sessions require migration', 'major'],
  ['docs: clarify setup', null],
  ['chore(ci): enforce coverage', null],
  ['perf: cache diagram layout', 'patch'],
]) {
  test(`SemVer: ${message.split('\n')[0]}`, async () => {
    assert.equal(await analyzeCommits(config.plugins[0][1], { commits: [{ message }], logger }), expected);
  });
}
test('highest impact wins across a batch of commits', async () => {
  const commits = ['fix: repair scrolling', 'feat: add diagrams', 'refactor!: change session format'].map(message => ({ message }));
  assert.equal(await analyzeCommits(config.plugins[0][1], { commits, logger }), 'major');
});
test('release notes describe real changes', async () => {
  const notes = await generateNotes(config.plugins[1][1], {
    commits: [{ message: 'feat(report): render Mermaid', hash: 'a'.repeat(40) }], logger,
    options: { repositoryUrl: config.repositoryUrl },
    lastRelease: { gitTag: 'v1.0.0' }, nextRelease: { gitTag: 'v1.1.0', version: '1.1.0' },
  });
  assert.match(notes, /render Mermaid/);
  assert.match(notes, /1\.1\.0/);
});
test('only main publishes GitHub assets without npm or automated comments', () => {
  assert.deepEqual(config.branches, ['main']);
  assert.equal(config.tagFormat, 'v${version}');
  assert.equal(config.plugins.length, 3);
  const [plugin, options] = config.plugins[2];
  assert.equal(plugin, '@semantic-release/github');
  assert.deepEqual(options.assets, ['dist/*.tar.gz', 'dist/checksums.txt']);
  assert.equal(options.successComment, false);
  assert.equal(options.failComment, false);
  assert.equal(options.releasedLabels, false);
});
