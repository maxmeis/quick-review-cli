import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { parse } from 'yaml';

const load = path => readFileSync(new URL(`../${path}`, import.meta.url), 'utf8');
const ci = parse(load('.github/workflows/ci.yml'));
const settings = JSON.parse(load('.github/repository/settings.json'));
const protection = JSON.parse(load('.github/repository/main-protection.json'));

test('all PR updates and main pushes run the full required checks', () => {
  assert.deepEqual(ci.on.push.branches, ['main']);
  assert.deepEqual(ci.on.pull_request.types, ['opened', 'synchronize', 'reopened', 'edited', 'ready_for_review']);
  assert.deepEqual(ci.jobs.verify.strategy.matrix.os, ['ubuntu-latest', 'macos-latest']);
  assert(ci.jobs.verify.steps.some(step => step.run === 'make verify'));
  assert(ci.jobs.standards.steps.some(step => step.run === 'pnpm test'));
  assert(ci.jobs.standards.steps.some(step => step.name === 'Validate squash commit title and body'));
  const squashCheck = ci.jobs.standards.steps.find(step => step.name === 'Validate squash commit title and body');
  assert.equal(squashCheck.env.PR_BODY, '${{ github.event.pull_request.body }}');
  assert.match(squashCheck.run, /\"\$PR_BODY\"/);
  assert.deepEqual(ci.permissions, { contents: 'read' });
  assert.equal(ci.concurrency['cancel-in-progress'], "${{ github.event_name == 'pull_request' }}");
  assert.match(ci.concurrency.group, /pull_request.number/);
  for (const job of Object.values(ci.jobs)) assert(job['timeout-minutes'] > 0);
});

test('main releases require the complete passing matrix and matching build artifacts', () => {
  const job = ci.jobs.release;
  assert.deepEqual(job.needs, ['verify', 'standards']);
  assert.equal(job.if, "(github.event_name == 'push' || github.event_name == 'workflow_dispatch') && github.ref == 'refs/heads/main'");
  assert.equal(job.concurrency['cancel-in-progress'], false);
  assert.deepEqual(job.permissions, { contents: 'write' });
  assert(job.steps.some(step => step.uses?.startsWith('actions/download-artifact@') && step.with.name === 'release-assets'));
  assert(job.steps.some(step => step.run?.includes('shasum -a 256 -c checksums.txt')));
  assert.equal(job.steps.at(-1).run, 'pnpm release');
});

test('repository permits only squash merges with conventional PR titles', () => {
  assert.equal(settings.allow_squash_merge, true);
  assert.equal(settings.allow_merge_commit, false);
  assert.equal(settings.allow_rebase_merge, false);
  assert.equal(settings.squash_merge_commit_title, 'PR_TITLE');
  assert.equal(settings.squash_merge_commit_message, 'PR_BODY');
  assert.equal(settings.delete_branch_on_merge, true);
  assert.equal(settings.allow_auto_merge, true);
});

test('main requires PRs, current checks, linear history, and resolved conversations', () => {
  assert.equal(protection.enforce_admins, true);
  assert.equal(protection.required_status_checks.strict, true);
  assert.deepEqual(protection.required_status_checks.checks.map(check => check.context).sort(), [
    'Commit and release checks', 'Verify (macos-latest)', 'Verify (ubuntu-latest)',
  ]);
  assert(protection.required_status_checks.checks.every(check => check.app_id === 15368));
  assert.equal(protection.required_pull_request_reviews.required_approving_review_count, 0);
  assert.equal(protection.required_linear_history, true);
  assert.equal(protection.required_conversation_resolution, true);
  assert.equal(protection.allow_force_pushes, false);
  assert.equal(protection.allow_deletions, false);
});
