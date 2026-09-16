module.exports = {
  username: "renovate[bot]",
  gitAuthor: "Renovate Bot <bot@renovateapp.com>",
  onboarding: false,
  platform: "github",
  forkProcessing: "disabled",
  dryRun: null,
  repositories: ["loafoe/mcp-notifier"],
  enabledManagers: ["gomod", "github-actions"],
  packageRules: [
    {
      matchDatasources: ["go"],
      matchFileNames: ["go.mod"],
      description: "Go dependencies in root",
    },
  ],
};
