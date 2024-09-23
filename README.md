> [!NOTE]
> Status: beta

# nixpkgs-update-notifier

A Matrix bot that allows you to subscribe to build failures for [nixpkgs-update](https://nix-community.github.io/nixpkgs-update/) (aka [`r-ryantm`](https://github.com/r-ryantm)).

Message the bot and type `help` to see a list of commands.

## Limitations

The bot has no access to the actual exit code of the nixpkgs-update runners, so it uses a simple heuristic to detect failures - it looks for "errory" words inside the build log.
This is obviously fallible, and can lead to false positives and false negatives -- feel free to report them!

## TODO

- Subscribe to packages under multiple Python/Ruby/... versions
