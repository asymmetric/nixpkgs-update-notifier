> [!Warning]
> The bot does not handle encryption, so it currently **might not work** with Element X. Follow [this issue](https://github.com/asymmetric/nixpkgs-update-notifier/issues/1) for progress.

# nixpkgs-update-notifier

A Matrix bot that allows you to subscribe to build failures for [nixpkgs-update](https://nix-community.github.io/nixpkgs-update/) (aka [`r-ryantm`](https://github.com/r-ryantm)).

Message the bot at `@nixpkgs-update-notify-bot:matrix.org` ([link](https://matrix.to/#/@nixpkgs-update-notify-bot:matrix.org)) and type `help` to see a list of commands.

## Moving parts

### nixpkgs-update log page

URL: `https://nixpkgs-update-logs.nix-community.org` (configurable via `-url` flag)

The main page is an HTML index of `<a>` links, one per attr path (e.g. `python3Packages.diceware`). Each link points to a per-package page, which in turn lists individual log files named by date (`2024-12-10.log`). Attr path names on this page are **normalized** by the nixpkgs-update bot (e.g. `python3Packages` rather than `python312Packages`).

The bot scrapes the main page on a timer and stores the normalized attr paths in the `packages` table — this is the canonical set of packages the bot knows about. It then fetches individual log files to check for build failures.

### packages.json.br

URL: `https://channels.nixos.org/nixos-unstable/packages.json.br`

A brotli-compressed JSON file published with each nixos-unstable channel update. It contains metadata for every package in nixpkgs, keyed by attr path:

```json
{
  "packages": {
    "python312Packages.diceware": {
      "meta": {
        "maintainers": [{ "github": "asymmetric" }, ...]
      }
    }
  }
}
```

This is used exclusively by the `follow` command to look up all packages maintained by a given GitHub handle. Unlike the log page, attr paths here are **denormalized** (e.g. `python312Packages`). The bot normalizes them before storing subscriptions so they match the log page's naming.

## Limitations

The bot has no access to the actual exit code of the nixpkgs-update runners, so it uses a simple heuristic to detect failures - it looks for "errory" words inside the build log.
This is obviously fallible, and can lead to false positives and false negatives -- feel free to report them!

The normalization rules mirror [`filter.sed`](https://github.com/nix-community/infra/blob/master/hosts/build02/filter.sed) from `nix-community/infra`. Some rules normalize to a specific pinned version (e.g. `beam26Packages`, `lua51Packages`) — if upstream changes that canonical version, the rules here must be updated too, or `follow` subscriptions for those package sets will silently stop matching.

## TODO

- Subscribe to multiple packages at once
- Subscribe to packages by team
