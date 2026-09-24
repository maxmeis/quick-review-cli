module.exports = {
  branches: ['main'],
  repositoryUrl: 'https://github.com/maxmeis/quick-review-cli.git',
  tagFormat: 'v${version}',
  plugins: [
    ['@semantic-release/commit-analyzer', { preset: 'conventionalcommits' }],
    ['@semantic-release/release-notes-generator', { preset: 'conventionalcommits' }],
    ['@semantic-release/github', {
      assets: ['dist/*.tar.gz', 'dist/checksums.txt'],
      successComment: false,
      failComment: false,
      releasedLabels: false,
    }],
  ],
};
