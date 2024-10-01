> [!Warning]
> The bot does not handle encryption, so it currently does **not** work with Element X. Follow [this issue](https://github.com/asymmetric/nixpkgs-update-notifier/issues/1) for progress.

# nixpkgs-update-notifier

A Matrix bot that allows you to subscribe to build failures for [nixpkgs-update](https://nix-community.github.io/nixpkgs-update/) (aka [`r-ryantm`](https://github.com/r-ryantm)).

Message the bot at `@nixpkgs-update-notify-bot:matrix.org` ([link](https://matrix.to/#/@nixpkgs-update-notify-bot:matrix.org)) and type `help` to see a list of commands.

## Limitations

The bot has no access to the actual exit code of the nixpkgs-update runners, so it uses a simple heuristic to detect failures - it looks for "errory" words inside the build log.
This is obviously fallible, and can lead to false positives and false negatives -- feel free to report them!

## TODO

- Subscribe to packages under multiple Python/Ruby/... versions
- Subscribe to multiple packages at once
- Subscribe to packages by maintainer
- Subscribe to packages by team
